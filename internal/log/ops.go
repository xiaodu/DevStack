package log

import (
	"sync"
	"time"
)

type OpEntry struct {
	Time    string `json:"time"`
	Action  string `json:"action"`
	Service string `json:"service"`
	Detail  string `json:"detail"`
	Status  string `json:"status"`
}

type OpStore struct {
	mu  sync.RWMutex
	buf []OpEntry
	max int
}

func NewOpStore(max int) *OpStore {
	return &OpStore{max: max}
}

func (s *OpStore) Add(action, service, detail, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, OpEntry{
		Time:    time.Now().Format("15:04:05"),
		Action:  action,
		Service: service,
		Detail:  detail,
		Status:  status,
	})
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
}

func (s *OpStore) Recent(n int) []OpEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.buf) {
		n = len(s.buf)
	}
	return append([]OpEntry{}, s.buf[len(s.buf)-n:]...)
}
