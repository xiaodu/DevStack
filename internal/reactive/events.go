package reactive

import (
	"fmt"
	"time"
)

type EventType string

const (
	EventServiceStarted     EventType = "service.started"
	EventServiceStopped     EventType = "service.stopped"
	EventServiceCrashed     EventType = "service.crashed"
	EventServiceHealthFailed EventType = "service.health_failed"
	EventServiceRestarted   EventType = "service.restarted"
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
	return &Event{
		ID:        fmt.Sprintf("%s-%d", eventType, time.Now().UnixNano()),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}
}