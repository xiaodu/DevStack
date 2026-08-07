package reactive

import (
	"context"
	"sync"
	"sync/atomic"
)

type EventHandler func(event *Event)

type Engine struct {
	mu           sync.RWMutex
	subscribers  map[EventType][]EventHandler
	ruleSet      *RuleSet
	eventBuffer  chan *Event
	running      atomic.Bool
	dropped      atomic.Uint64
	onDropped    func(event *Event)
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewEngine(bufferSize int) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		subscribers: make(map[EventType][]EventHandler),
		ruleSet:     NewRuleSet(),
		eventBuffer: make(chan *Event, bufferSize),
		ctx:         ctx,
		cancel:      cancel,
	}
	e.running.Store(true)
	return e
}

// SetOnDropped 注册缓冲满时被丢弃事件的回调，用于可观测化（日志/指标）。
func (e *Engine) SetOnDropped(fn func(event *Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onDropped = fn
}

// DroppedCount 返回因缓冲满而丢弃的事件数。
func (e *Engine) DroppedCount() uint64 {
	return e.dropped.Load()
}

func (e *Engine) Start() {
	go e.processEvents()
}

func (e *Engine) Stop() {
	e.running.Store(false)
	e.cancel()
}

func (e *Engine) processEvents() {
	for {
		select {
		case event := <-e.eventBuffer:
			e.dispatch(event)
			e.executeRules(event)
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *Engine) dispatch(event *Event) {
	e.mu.RLock()
	handlers, ok := e.subscribers[event.Type]
	e.mu.RUnlock()

	if !ok {
		return
	}

	for _, handler := range handlers {
		go func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					// 记录 panic 但不崩溃
				}
			}()
			h(event)
		}(handler)
	}
}

func (e *Engine) executeRules(event *Event) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 记录 panic 但不崩溃
			}
		}()
		_ = e.ruleSet.ExecuteMatching(e.ctx, event)
	}()
}

func (e *Engine) Publish(event *Event) {
	if !e.running.Load() {
		return
	}

	select {
	case e.eventBuffer <- event:
	default:
		e.dropped.Add(1)
		e.mu.RLock()
		cb := e.onDropped
		e.mu.RUnlock()
		if cb != nil {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						// 记录 panic 但不崩溃
					}
				}()
				cb(event)
			}()
		}
	}
}

func (e *Engine) Subscribe(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.subscribers[eventType] = append(e.subscribers[eventType], handler)
}

func (e *Engine) Unsubscribe(eventType EventType, _ EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.subscribers, eventType)
}

func (e *Engine) AddRule(rule Rule) {
	e.ruleSet.AddRule(rule)
}

func (e *Engine) RemoveRule(name string) {
	e.ruleSet.RemoveRule(name)
}

func (e *Engine) GetRules(eventType EventType) []Rule {
	return e.ruleSet.GetRules(eventType)
}