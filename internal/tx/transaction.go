package tx

import (
	"context"
	"fmt"
	"sync"
)

type Transaction struct {
	id          string
	steps       []*Step
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	status      TransactionStatus
	err         error
	rollbackErr error
}

type Step struct {
	name       string
	forward    StepFunc
	compensate StepFunc
	result     interface{}
	err        error
	executed   bool
}

type StepFunc func(ctx context.Context) error

type TransactionStatus string

const (
	StatusPending    TransactionStatus = "pending"
	StatusRunning    TransactionStatus = "running"
	StatusCommitted  TransactionStatus = "committed"
	StatusRolledBack TransactionStatus = "rolledback"
	StatusFailed     TransactionStatus = "failed"
)

func NewTransaction(id string) *Transaction {
	ctx, cancel := context.WithCancel(context.Background())
	return &Transaction{
		id:     id,
		steps:  make([]*Step, 0),
		ctx:    ctx,
		cancel: cancel,
		status: StatusPending,
	}
}

func NewTransactionWithContext(ctx context.Context, id string) *Transaction {
	ctx, cancel := context.WithCancel(ctx)
	return &Transaction{
		id:     id,
		steps:  make([]*Step, 0),
		ctx:    ctx,
		cancel: cancel,
		status: StatusPending,
	}
}

func (t *Transaction) Step(name string, forward StepFunc, compensate StepFunc) *Transaction {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status != StatusPending {
		panic(fmt.Sprintf("transaction %s is not in pending state (current: %s)", t.id, t.status))
	}

	t.steps = append(t.steps, &Step{
		name:       name,
		forward:    forward,
		compensate: compensate,
	})
	return t
}

func (t *Transaction) StepOnly(name string, forward StepFunc) *Transaction {
	return t.Step(name, forward, nil)
}

func (t *Transaction) Commit() error {
	t.mu.Lock()
	if t.status != StatusPending {
		t.mu.Unlock()
		return fmt.Errorf("transaction %s is not in pending state (current: %s)", t.id, t.status)
	}
	t.status = StatusRunning
	t.mu.Unlock()

	for i, step := range t.steps {
		select {
		case <-t.ctx.Done():
			t.Rollback()
			return fmt.Errorf("transaction %s cancelled: %w", t.id, t.ctx.Err())
		default:
		}

		t.mu.Lock()
		currentStep := i
		t.mu.Unlock()

		if err := step.forward(t.ctx); err != nil {
			t.mu.Lock()
			step.err = err
			step.executed = true
			t.err = fmt.Errorf("step %d (%s) failed: %w", currentStep, step.name, err)
			t.mu.Unlock()

			rbErr := t.Rollback()
			if rbErr != nil {
				t.mu.Lock()
				t.rollbackErr = rbErr
				t.mu.Unlock()
				return fmt.Errorf("step %d (%s) failed: %w; rollback also failed: %w", currentStep, step.name, err, rbErr)
			}
			return t.err
		}

		t.mu.Lock()
		step.executed = true
		t.mu.Unlock()
	}

	t.mu.Lock()
	t.status = StatusCommitted
	t.mu.Unlock()
	t.cancel()

	return nil
}

func (t *Transaction) Rollback() error {
	t.mu.Lock()
	if t.status == StatusRolledBack || t.status == StatusCommitted {
		t.mu.Unlock()
		return nil
	}
	t.status = StatusRolledBack
	t.mu.Unlock()

	rbCtx := context.Background()
	var errs []error
	for i := len(t.steps) - 1; i >= 0; i-- {
		step := t.steps[i]
		t.mu.RLock()
		executed := step.executed
		t.mu.RUnlock()
		if !executed || step.compensate == nil {
			continue
		}

		if err := step.compensate(rbCtx); err != nil {
			errs = append(errs, fmt.Errorf("compensate step %d (%s) failed: %w", i, step.name, err))
		}
	}

	t.cancel()

	if len(errs) > 0 {
		return fmt.Errorf("rollback completed with %d errors: %v", len(errs), errs)
	}
	return nil
}

func (t *Transaction) Cancel() {
	t.cancel()
}

func (t *Transaction) Status() TransactionStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *Transaction) ID() string {
	return t.id
}

func (t *Transaction) Error() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err
}

func (t *Transaction) RollbackError() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rollbackErr
}