package resource

import "time"

type ProcessResource struct {
	PID         int       `json:"pid"`
	Owner       string    `json:"owner"`
	ProcessType string    `json:"processType"`
	StartedAt   time.Time `json:"startedAt"`
	Port        int       `json:"port,omitempty"`
}

func NewProcessResource(pid int, owner string, processType string) *ProcessResource {
	return &ProcessResource{
		PID:         pid,
		Owner:       owner,
		ProcessType: processType,
		StartedAt:   time.Now(),
	}
}
