package reactive

import (
	"context"
	"sync"
)

type EventHandler func(event *Event)

type Engine struct {
	mu           sync.RWMutex
	subscribers  map[EventType][]EventHandler
	ruleSet      *RuleSet
	eventBuffer  chan *Event
	running      bool
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewEngine(bufferSize int) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		subscribers: make(map[EventType][]EventHandler),
		ruleSet:     NewRuleSet(),
		eventBuffer: make(chan *Event, bufferSize),
		running:     true,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (e *Engine) Start() {
	go e.processEvents()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
	e.cancel()
	close(e.eventBuffer)
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
		go handler(event)
	}
}

func (e *Engine) executeRules(event *Event) {
	go func() {
		_ = e.ruleSet.ExecuteMatching(e.ctx, event)
	}()
}

func (e *Engine) Publish(event *Event) {
	if !e.running {
		return
	}

	select {
	case e.eventBuffer <- event:
	default:
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