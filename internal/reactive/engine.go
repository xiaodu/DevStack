package reactive

import (
	"context"
	"reflect"
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
	dispatchSem  chan struct{}
	ruleSem      chan struct{}
}

func NewEngine(bufferSize int) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		subscribers: make(map[EventType][]EventHandler),
		ruleSet:     NewRuleSet(),
		eventBuffer: make(chan *Event, bufferSize),
		ctx:         ctx,
		cancel:      cancel,
		dispatchSem: make(chan struct{}, 64),
		ruleSem:     make(chan struct{}, 16),
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

	// 记账语义：一个事件对订阅者"完全丢失"只记一次丢弃（got + dropped 与发布总数守恒）
	delivered := false
	for _, handler := range handlers {
		select {
		case e.dispatchSem <- struct{}{}:
			delivered = true
			go func(h EventHandler) {
				defer func() { <-e.dispatchSem }()
				defer func() {
					if r := recover(); r != nil {
						// handler panic 不允许击垮事件循环
					}
				}()
				h(event)
			}(handler)
		default:
			// 超过并发限制，本 handler 分发被丢弃
		}
	}
	if !delivered {
		e.recordDropped(event)
	}
}

func (e *Engine) executeRules(event *Event) {
	select {
	case e.ruleSem <- struct{}{}:
		go func() {
			defer func() { <-e.ruleSem }()
			defer func() {
				if r := recover(); r != nil {
					// 规则 panic 不允许击垮事件循环
				}
			}()
			_ = e.ruleSet.ExecuteMatching(e.ctx, event)
		}()
	default:
		// 规则执行器满，跳过本次规则匹配。不记入事件丢弃账：
		// 事件本身已投递给订阅者，这里只是规则链路降级（避免与事件账重复计数）
	}
}

// recordDropped 记录一次事件丢弃：计数 + 触发 onDropped 回调。
func (e *Engine) recordDropped(event *Event) {
	e.dropped.Add(1)
	e.mu.RLock()
	cb := e.onDropped
	e.mu.RUnlock()
	if cb != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// 回调 panic 不允许击垮事件循环
				}
			}()
			cb(event)
		}()
	}
}

func (e *Engine) Publish(event *Event) {
	if !e.running.Load() {
		return
	}

	select {
	case e.eventBuffer <- event:
	default:
		e.recordDropped(event)
	}
}

func (e *Engine) Subscribe(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.subscribers[eventType] = append(e.subscribers[eventType], handler)
}

func (e *Engine) Unsubscribe(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	handlers := e.subscribers[eventType]
	for i, h := range handlers {
		if reflect.ValueOf(h).Pointer() == reflect.ValueOf(handler).Pointer() {
			e.subscribers[eventType] = append(handlers[:i], handlers[i+1:]...)
			return
		}
	}
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