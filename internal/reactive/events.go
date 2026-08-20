package reactive

import (
	"fmt"
	"sync/atomic"
	"time"
)

type EventType string

var eventCounter atomic.Uint64

const (
	EventServiceStarted     EventType = "service.started"
	EventServiceStopped     EventType = "service.stopped"
	EventServiceStarting    EventType = "service.starting"
	EventServiceStopping    EventType = "service.stopping"
	EventServiceCrashed     EventType = "service.crashed"
	EventServiceHealthFailed EventType = "service.health_failed"
	EventServiceRestarted   EventType = "service.restarted"
	EventServiceError       EventType = "service.error"
	EventPortConflict       EventType = "port.conflict"
	EventConfigChanged      EventType = "config.changed"
	EventConfigReloaded     EventType = "config.reloaded"
	EventResourceAllocated  EventType = "resource.allocated"
	EventResourceReleased   EventType = "resource.released"
	EventProjectCreated     EventType = "project.created"
	EventProjectDeleted     EventType = "project.deleted"
	EventProjectEnabled     EventType = "project.enabled"
	EventProjectDisabled    EventType = "project.disabled"
)

type Event struct {
	ID        string
	Type      EventType
	Source    string
	Timestamp time.Time
	Data      map[string]interface{}
}

func NewEvent(eventType EventType, source string, data map[string]interface{}) *Event {
	counter := eventCounter.Add(1)
	return &Event{
		ID:        fmt.Sprintf("%s-%d-%d", eventType, time.Now().UnixNano(), counter),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}
}