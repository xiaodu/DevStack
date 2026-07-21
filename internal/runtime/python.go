package runtime

import "github.com/xiaodu/DevStack/internal/core"

func (m *Manager) GetPythonInfo() []core.RuntimeInfo {
	return m.ListByType(core.RuntimePython)
}

func (m *Manager) StartPython(version string) error {
	return nil
}

func (m *Manager) StopPython(version string) error {
	return nil
}
