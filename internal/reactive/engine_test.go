package reactive

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ==================== RuleSet ====================

func TestRuleSetExecuteMatchingRunsOnlyMatching(t *testing.T) {
	rs := NewRuleSet()
	var ran1, ran2 int32
	rs.AddRule(NewRule("r1", EventServiceCrashed, nil, func(ctx context.Context, e *Event) error {
		atomic.AddInt32(&ran1, 1)
		return nil
	}))
	rs.AddRule(NewRule("r2", EventServiceStarted, nil, func(ctx context.Context, e *Event) error {
		atomic.AddInt32(&ran2, 1)
		return nil
	}))

	rs.ExecuteMatching(context.Background(), NewEvent(EventServiceCrashed, "test", nil))
	if atomic.LoadInt32(&ran1) != 1 {
		t.Errorf("r1 应执行 1 次, got %d", ran1)
	}
	if atomic.LoadInt32(&ran2) != 0 {
		t.Errorf("r2 不应执行, got %d", ran2)
	}
}

func TestRuleSetMatchPredicateBlocks(t *testing.T) {
	rs := NewRuleSet()
	var ran int32
	rs.AddRule(NewRule("blocked", EventServiceCrashed,
		func(ctx context.Context, e *Event) bool { return false },
		func(ctx context.Context, e *Event) error {
			atomic.AddInt32(&ran, 1)
			return nil
		}))
	rs.ExecuteMatching(context.Background(), NewEvent(EventServiceCrashed, "test", nil))
	if atomic.LoadInt32(&ran) != 0 {
		t.Errorf("match=false 时不应执行, got %d", ran)
	}
}

func TestRuleSetCollectsErrors(t *testing.T) {
	rs := NewRuleSet()
	rs.AddRule(NewRule("err1", EventServiceCrashed, nil, func(ctx context.Context, e *Event) error {
		return context.DeadlineExceeded
	}))
	errs := rs.ExecuteMatching(context.Background(), NewEvent(EventServiceCrashed, "test", nil))
	if len(errs) != 1 {
		t.Fatalf("应收集 1 个错误, got %d", len(errs))
	}
}

// probe: 相同事件类型的多个规则执行顺序应当稳定（依赖 sync.Map.Range 的随机顺序是隐患）
func TestRuleSetExecutionOrderStable(t *testing.T) {
	rs := NewRuleSet()
	order := []string{}
	rs.AddRule(NewRule("first", EventServiceCrashed, nil, func(ctx context.Context, e *Event) error {
		order = append(order, "first")
		return nil
	}))
	rs.AddRule(NewRule("second", EventServiceCrashed, nil, func(ctx context.Context, e *Event) error {
		order = append(order, "second")
		return nil
	}))

	for i := 0; i < 100; i++ {
		order = nil
		rs.ExecuteMatching(context.Background(), NewEvent(EventServiceCrashed, "test", nil))
		if len(order) != 2 {
			t.Fatalf("规则未全部执行: %v", order)
		}
		if order[0] != "first" || order[1] != "second" {
			t.Fatalf("执行顺序不稳定: %v (期望 first,second)", order)
		}
	}
}

// ==================== Engine ====================

func TestEngineDeliversBufferedEventsAfterStart(t *testing.T) {
	e := NewEngine(10)
	var got atomic.Int32
	e.Subscribe(EventServiceStarted, func(event *Event) {
		got.Add(1)
	})
	// 未 Start 时先 Publish（进 buffer）
	e.Publish(NewEvent(EventServiceStarted, "test", nil))
	e.Publish(NewEvent(EventServiceStarted, "test", nil))
	e.Start()
	deadline := time.Now().Add(2 * time.Second)
	for got.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 2 {
		t.Errorf("buffer 内事件未全部投递, got %d want 2", got.Load())
	}
}

func TestEngineIgnoresEventWhenStopped(t *testing.T) {
	e := NewEngine(10)
	var got atomic.Int32
	e.Subscribe(EventServiceStarted, func(event *Event) { got.Add(1) })
	e.Start()
	e.Stop()
	time.Sleep(20 * time.Millisecond)
	e.Publish(NewEvent(EventServiceStarted, "test", nil))
	time.Sleep(100 * time.Millisecond)
	if got.Load() != 0 {
		t.Errorf("Stop 后 Publish 不应被投递, got %d", got.Load())
	}
}

// probe: 缓冲满时事件不允许静默丢失 — 必须可观测（DroppedCount 计数 + 回调）
func TestEngineBufferOverflowObservable(t *testing.T) {
	e := NewEngine(100)
	var got atomic.Int32
	e.Subscribe(EventServiceStarted, func(event *Event) { got.Add(1) })
	var droppedCalls atomic.Int32
	e.SetOnDropped(func(event *Event) { droppedCalls.Add(1) })
	e.Start()

	wg := sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				e.Publish(NewEvent(EventServiceStarted, "burst", nil))
			}
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for got.Load()+droppedCalls.Load() < 500 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	total := got.Load() + droppedCalls.Load()
	if total != 500 {
		t.Errorf("事件总账不平: 投递 %d + 丢弃 %d = %d, want 500", got.Load(), droppedCalls.Load(), total)
	}
	if got.Load() == 0 {
		t.Errorf("没有事件被投递, 全部丢弃")
	}
}

// probe: Stop() 之后 processEvents 不应崩溃（关闭的 channel 会一直返回零值）
func TestEngineStopDoesNotCrash(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop 后触发 panic: %v", r)
		}
	}()

	e := NewEngine(10)
	e.Subscribe(EventServiceStarted, func(event *Event) {})
	e.Publish(NewEvent(EventServiceStarted, "test", nil))
	e.Publish(NewEvent(EventServiceStarted, "test", nil))
	e.Start()
	time.Sleep(50 * time.Millisecond)
	e.Stop()
	// 给 processEvents 从已关闭 channel 读 nil 的机会
	time.Sleep(100 * time.Millisecond)
}

// probe: Publish 与 Stop 并发不应产生 data race / panic
// （Publish 无锁读 e.running，Stop 写 e.running 并 close 事件通道）
func TestEngineConcurrentPublishStopNoRace(t *testing.T) {
	for round := 0; round < 5; round++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("并发 Publish/Stop 触发 panic: %v", r)
				}
			}()
			e := NewEngine(10)
			e.Subscribe(EventServiceStarted, func(event *Event) {})
			e.Start()

			stopCh := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					select {
					case <-stopCh:
						return
					default:
						e.Publish(NewEvent(EventServiceStarted, "race", nil))
					}
				}
			}()
			time.Sleep(time.Millisecond)
			e.Stop()
			close(stopCh)
			<-done
		}()
	}
}
