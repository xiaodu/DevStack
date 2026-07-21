package runtime

import "github.com/xiaodu/DevStack/internal/core"

func (m *Manager) GetNodeInfo() []core.RuntimeInfo {
	return m.ListByType(core.RuntimeNode)
}

func (m *Manager) StartNode(version string) error {
	return nil
}

func (m *Manager) StopNode(version string) error {
	return nil
}

func (m *Manager) ListByType(rt core.RuntimeType) []core.RuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []core.RuntimeInfo
	for _, id := range m.idByType[rt] {
		if inst, ok := m.instances[id]; ok {
			result = append(result, inst.Info)
		}
	}
	return result
}
