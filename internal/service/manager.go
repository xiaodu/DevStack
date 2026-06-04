package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type Manager struct {
	mu        sync.RWMutex
	cfg       *config.Config
	procMgr   *process.Manager
	services  map[string]*ServiceInstance
}

type ServiceInstance struct {
	Info    core.ServiceInfo
	process *execWrapper
}

type execWrapper struct {
	pid int
	cmd string
	env []string
	started time.Time
}

func NewManager(cfg *config.Config, procMgr *process.Manager) *Manager {
	m := &Manager{
		cfg:      cfg,
		procMgr:  procMgr,
		services: make(map[string]*ServiceInstance),
	}
	m.registerDefaults()
	return m
}

func (m *Manager) registerDefaults() {
	installDir := m.cfg.Get().InstallDir

	m.services["nginx"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "nginx", Name: "Nginx", Type: core.TypeNginx,
			Version: "1.26.0", Port: 80, Status: core.StatusStopped,
		},
	}
	m.services["mysql"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "mysql", Name: "MySQL", Type: core.TypeMySQL,
			Version: "8.4.0", Port: 3306, Status: core.StatusStopped,
		},
	}
	m.services["redis"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "redis", Name: "Redis", Type: core.TypeRedis,
			Version: "7.2.0", Port: 6379, Status: core.StatusStopped,
		},
	}
	m.services["php"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "php", Name: "PHP", Type: core.TypePHP,
			Version: "8.3.8", Port: 9000, Status: core.StatusStopped,
		},
	}
	m.services["node"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "node", Name: "Node.js", Type: core.TypeNode,
			Version: "20.11.0", Port: 0, Status: core.StatusStopped,
		},
	}
	m.services["python"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "python", Name: "Python", Type: core.TypePython,
			Version: "3.12.0", Port: 0, Status: core.StatusStopped,
		},
	}

	for _, s := range m.services {
		binPath := filepath.Join(installDir, string(s.Info.Type), s.Info.Version)
		s.Info.Installed = dirExists(binPath)
		if s.Info.Installed {
			pid := process.FindPIDByPort(s.Info.Port)
			if pid > 0 && m.procMgr.IsRunning(pid) {
				s.Info.Status = core.StatusRunning
				s.Info.PID = pid
			}
		}
	}
}

func (m *Manager) List() []core.ServiceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]core.ServiceInfo, 0, len(m.services))
	for _, s := range m.services {
		info := s.Info
		if s.process != nil && s.process.pid > 0 {
			if m.procMgr.IsRunning(s.process.pid) {
				info.Status = core.StatusRunning
				info.PID = s.process.pid
				info.Uptime = time.Since(s.process.started).Round(time.Second).String()
			} else {
				info.Status = core.StatusStopped
				info.PID = 0
			}
		}
		result = append(result, info)
	}
	return result
}

func (m *Manager) Get(id string) (core.ServiceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.services[id]
	if !ok {
		return core.ServiceInfo{}, core.ErrServiceNotFound
	}
	return s.Info, nil
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}
	if s.Info.Status == core.StatusRunning {
		return nil
	}

	handler, ok := handlers[id]
	if !ok {
		return fmt.Errorf("不支持的的服务类型: %s", id)
	}

	return handler.Start(s, m.cfg, m.procMgr)
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}

	handler, ok := handlers[id]
	if !ok {
		return fmt.Errorf("不支持的的服务类型: %s", id)
	}

	return handler.Stop(s, m.procMgr)
}

func (m *Manager) Restart(id string) error {
	if err := m.Stop(id); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start(id)
}

func (m *Manager) SetVersion(id, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}

	installDir := m.cfg.Get().InstallDir
	binPath := filepath.Join(installDir, id, version)
	if !dirExists(binPath) {
		return fmt.Errorf("版本 %s 未安装", version)
	}

	s.Info.Version = version
	s.Info.Installed = true
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
