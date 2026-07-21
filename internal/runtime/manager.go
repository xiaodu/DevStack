package runtime

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type Manager struct {
	mu        sync.RWMutex
	cfg       *config.Config
	procMgr   *process.Manager
	instances map[string]*Instance
	idByType  map[core.RuntimeType][]string
}

func NewManager(cfg *config.Config, procMgr *process.Manager) *Manager {
	m := &Manager{
		cfg:       cfg,
		procMgr:   procMgr,
		instances: make(map[string]*Instance),
		idByType:  make(map[core.RuntimeType][]string),
	}
	m.scan()
	return m
}

func (m *Manager) List() []core.RuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]core.RuntimeInfo, 0, len(m.instances))
	typeOrder := []core.RuntimeType{core.RuntimePHP, core.RuntimeNode, core.RuntimePython}

	for _, rt := range typeOrder {
		for _, id := range m.idByType[rt] {
			inst := m.instances[id]
			result = append(result, inst.Info)
		}
	}
	return result
}

func (m *Manager) Get(id string) (core.RuntimeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inst, ok := m.instances[id]
	if !ok {
		return core.RuntimeInfo{}, core.ErrRuntimeNotFound
	}

	return inst.Info, nil
}

func (m *Manager) GetPHPVersionPort(version string) int {
	return GetPHPVersionPort(m.cfg, version)
}

func (m *Manager) scan() {
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return
	}

	typeDirs := map[core.RuntimeType]string{
		core.RuntimePHP:    filepath.Join(installDir, "php"),
		core.RuntimeNode:   filepath.Join(installDir, "node"),
		core.RuntimePython: filepath.Join(installDir, "python"),
	}

	// 加载 PHP 端口注册表
	phpPorts := make(map[string]int)
	for _, v := range LoadPHPRegistry(m.cfg).Versions {
		phpPorts[v.Version] = v.Port
	}

	// 类型显示名称
	typeNames := map[core.RuntimeType]string{
		core.RuntimePHP:    "PHP",
		core.RuntimeNode:   "Node.js",
		core.RuntimePython: "Python",
	}

	for rt, dir := range typeDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			version := entry.Name()
			rPath := filepath.Join(dir, version)

			id := string(rt) + "-" + version

			displayName := typeNames[rt]
			if displayName == "" {
				displayName = string(rt)
			}

			info := core.RuntimeInfo{
				ID:        id,
				Type:      rt,
				Version:   version,
				Path:      rPath,
				Active:    false,
				Running:   false,
				Installed: true,
				Status:    core.StatusStopped,
				Name:      displayName + " " + version,
			}

			// PHP 从注册表获取 FastCGI 端口
			if rt == core.RuntimePHP {
				if port, ok := phpPorts[version]; ok {
					info.Port = port
				}
			}

			inst := &Instance{Info: info}
			m.instances[id] = inst
			m.idByType[rt] = append(m.idByType[rt], id)
		}
	}
}
