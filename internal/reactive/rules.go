package reactive

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Rule interface {
	Name() string
	When(eventType EventType) bool
	Match(ctx context.Context, event *Event) bool
	Execute(ctx context.Context, event *Event) error
}

type ActionFunc func(ctx context.Context, event *Event) error

type SimpleRule struct {
	name        string
	eventType   EventType
	matchFunc   func(ctx context.Context, event *Event) bool
	actionFunc  ActionFunc
}

func NewRule(name string, eventType EventType, match func(ctx context.Context, event *Event) bool, action ActionFunc) *SimpleRule {
	return &SimpleRule{
		name:       name,
		eventType:  eventType,
		matchFunc:  match,
		actionFunc: action,
	}
}

func (r *SimpleRule) Name() string {
	return r.name
}

func (r *SimpleRule) When(eventType EventType) bool {
	return r.eventType == eventType
}

func (r *SimpleRule) Match(ctx context.Context, event *Event) bool {
	if r.matchFunc == nil {
		return true
	}
	return r.matchFunc(ctx, event)
}

func (r *SimpleRule) Execute(ctx context.Context, event *Event) error {
	if r.actionFunc == nil {
		return nil
	}
	return r.actionFunc(ctx, event)
}

type RuleSet struct {
	rules sync.Map
}

func NewRuleSet() *RuleSet {
	return &RuleSet{}
}

func (rs *RuleSet) AddRule(rule Rule) {
	rs.rules.Store(rule.Name(), rule)
}

func (rs *RuleSet) RemoveRule(name string) {
	rs.rules.Delete(name)
}

func (rs *RuleSet) GetRules(eventType EventType) []Rule {
	var rules []Rule
	rs.rules.Range(func(key, value interface{}) bool {
		rule := value.(Rule)
		if rule.When(eventType) {
			rules = append(rules, rule)
		}
		return true
	})
	return rules
}

func (rs *RuleSet) ExecuteMatching(ctx context.Context, event *Event) []error {
	rules := rs.GetRules(event.Type)
	var errors []error
	for _, rule := range rules {
		if rule.Match(ctx, event) {
			if err := rule.Execute(ctx, event); err != nil {
				errors = append(errors, fmt.Errorf("rule %s failed: %w", rule.Name(), err))
			}
		}
	}
	return errors
}

type ServiceRestarter func(serviceID string) error
type PortReallocator func(owner string, oldPort int) (int, error)
type AlertCallback func(eventType EventType, source string, data map[string]interface{})

func RegisterBuiltinRules(engine *Engine, restarter ServiceRestarter, reallocator PortReallocator, alert AlertCallback) {
	engine.AddRule(NewRule(
		"service_crash_restart",
		EventServiceCrashed,
		func(ctx context.Context, event *Event) bool {
			if count, ok := event.Data["restartCount"].(int); ok && count >= 3 {
				return false
			}
			return true
		},
		func(ctx context.Context, event *Event) error {
			if restarter == nil {
				return nil
			}
			if id, ok := event.Data["serviceID"].(string); ok {
				return restarter(id)
			}
			return nil
		},
	))

	engine.AddRule(NewRule(
		"port_conflict_reassign",
		EventPortConflict,
		func(ctx context.Context, event *Event) bool {
			_, hasOwner := event.Data["owner"].(string)
			_, hasOldPort := event.Data["port"].(int)
			return hasOwner && hasOldPort
		},
		func(ctx context.Context, event *Event) error {
			if reallocator == nil {
				return nil
			}
			owner, _ := event.Data["owner"].(string)
			oldPort, _ := event.Data["port"].(int)
			newPort, err := reallocator(owner, oldPort)
			if err != nil {
				return err
			}
			event.Data["newPort"] = newPort
			return nil
		},
	))

	engine.AddRule(NewRule(
		"health_check_failure_alert",
		EventServiceHealthFailed,
		func(ctx context.Context, event *Event) bool {
			return true
		},
		func(ctx context.Context, event *Event) error {
			if alert != nil {
				alert(EventServiceHealthFailed, event.Source, event.Data)
			}
			return nil
		},
	))

	engine.AddRule(NewRule(
		"service_start_failure_retry",
		EventServiceCrashed,
		func(ctx context.Context, event *Event) bool {
			if count, ok := event.Data["restartCount"].(int); ok && count >= 2 {
				return false
			}
			return true
		},
		func(ctx context.Context, event *Event) error {
			time.Sleep(2 * time.Second)
			if restarter == nil {
				return nil
			}
			if id, ok := event.Data["serviceID"].(string); ok {
				return restarter(id)
			}
			return nil
		},
	))
}