package resource

import (
	"fmt"
	"sync"
)

type Manager struct {
	mu       sync.RWMutex
	ports    *PortPool
	paths    *PathManager
	processes map[int]*ProcessResource
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		ports:     NewPortPool(baseDir),
		paths:     NewPathManager(baseDir),
		processes: make(map[int]*ProcessResource),
	}
}

func (m *Manager) Ports() *PortPool {
	return m.ports
}

func (m *Manager) Paths() *PathManager {
	return m.paths
}

func (m *Manager) RegisterProcess(pid int, owner string, processType string) *ProcessResource {
	m.mu.Lock()
	defer m.mu.Unlock()

	pr := &ProcessResource{
		PID:         pid,
		Owner:       owner,
		ProcessType: processType,
	}
	m.processes[pid] = pr
	return pr
}

func (m *Manager) GetProcess(pid int) (*ProcessResource, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pr, ok := m.processes[pid]
	return pr, ok
}

func (m *Manager) UnregisterProcess(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processes, pid)
}

func (m *Manager) GetProcessesByOwner(owner string) []*ProcessResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*ProcessResource
	for _, pr := range m.processes {
		if pr.Owner == owner {
			result = append(result, pr)
		}
	}
	return result
}

func (m *Manager) ReleaseAll(owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	for pid, pr := range m.processes {
		if pr.Owner == owner {
			delete(m.processes, pid)
		}
	}

	m.ports.ReleaseByOwner(owner)

	if len(errs) > 0 {
		return fmt.Errorf("释放资源时出现 %d 个错误", len(errs))
	}
	return nil
}
