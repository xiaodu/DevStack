package environment

import "time"

type Environment struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	IsBuiltin   bool       `json:"isBuiltin"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
	Units       []UnitSpec `json:"units"`
	Tags        []string   `json:"tags,omitempty"`
}

type UnitSpec struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Name    string `json:"name"`
}

func NewID() string {
	return "env_" + time.Now().Format("20060102150405") + "_" + randSuffix()
}

func randSuffix() string {
	s := make([]byte, 6)
	for i := range s {
		s[i] = "0123456789abcdefghijklmnopqrstuvwxyz"[time.Now().UnixNano()%36]
		time.Sleep(time.Nanosecond)
	}
	return string(s)
}
