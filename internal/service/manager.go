package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
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
			Category: core.CategoryWebServer,
			Version: "1.26.0", Port: 80, Status: core.StatusStopped,
		},
	}
	m.services["mysql"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "mysql", Name: "MySQL", Type: core.TypeMySQL,
			Category: core.CategoryDatabase,
			Version: "8.4.0", Port: 3306, Status: core.StatusStopped,
		},
	}
	m.services["redis"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "redis", Name: "Redis", Type: core.TypeRedis,
			Category: core.CategoryDatabase,
			Version: "7.2.0", Port: 6379, Status: core.StatusStopped,
		},
	}
	m.services["php"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "php", Name: "PHP", Type: core.TypePHP,
			Category: core.CategoryLanguage,
			Version: "8.3.8", Port: 9000, Status: core.StatusStopped,
		},
	}
	m.services["node"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "node", Name: "Node.js", Type: core.TypeNode,
			Category: core.CategoryLanguage,
			Version: "20.11.0", Port: 3000, Status: core.StatusStopped,
		},
	}
	m.services["python"] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "python", Name: "Python", Type: core.TypePython,
			Category: core.CategoryLanguage,
			Version: "3.12.0", Port: 3001, Status: core.StatusStopped,
		},
	}

	for _, s := range m.services {
		svcDir := filepath.Join(installDir, string(s.Info.Type))
		entries, err := os.ReadDir(svcDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					verDir := filepath.Join(svcDir, entry.Name())
					if dirExists(verDir) {
						s.Info.Version = entry.Name()
						s.Info.Installed = true
						break
					}
				}
			}
		}
		if s.Info.Installed {
			pid := process.FindPIDByPort(s.Info.Port)
			if pid > 0 {
				s.Info.Status = core.StatusRunning
				s.Info.PID = pid
			}
		}
	}
}

var serviceOrder = []string{"nginx", "mysql", "redis", "php", "node", "python"}

func (m *Manager) List() []core.ServiceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]core.ServiceInfo, 0, len(m.services))
	for _, id := range serviceOrder {
		s, ok := m.services[id]
		if !ok {
			continue
		}
		info := s.Info

		running := false
		if s.process != nil && s.process.pid > 0 {
			if m.procMgr.IsRunning(s.process.pid) {
				running = true
				info.PID = s.process.pid
				info.Uptime = time.Since(s.process.started).Round(time.Second).String()
			}
		}
		if !running {
			pid := process.FindPIDByPort(info.Port)
			if pid > 0 {
				running = true
				info.PID = pid
			}
		}

		if running {
			info.Status = core.StatusRunning
		} else {
			info.Status = core.StatusStopped
			info.PID = 0
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
	if s.process != nil && s.process.pid > 0 && m.procMgr.IsRunning(s.process.pid) {
		s.Info.Status = core.StatusRunning
		s.Info.PID = s.process.pid
	} else {
		pid := process.FindPIDByPort(s.Info.Port)
		if pid > 0 {
			s.Info.Status = core.StatusRunning
			s.Info.PID = pid
		} else {
			s.Info.Status = core.StatusStopped
			s.Info.PID = 0
		}
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

	// If we have an execWrapper, delegate to the handler
	if s.process != nil && s.process.pid > 0 {
		handler, ok := handlers[id]
		if !ok {
			return fmt.Errorf("不支持的的服务类型: %s", id)
		}
		return handler.Stop(s, m.procMgr)
	}

	// No execWrapper — try to stop by port (e.g. started externally or before app restart)
	pid := process.FindPIDByPort(s.Info.Port)
	if pid > 0 {
		s.Info.Status = core.StatusStopping
		if err := m.procMgr.Stop(pid); err != nil {
			s.Info.Status = core.StatusError
			return err
		}
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}

func (m *Manager) Restart(id string) error {
	if err := m.Stop(id); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start(id)
}

func (m *Manager) Uninstall(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}

	if s.Info.PID > 0 {
		m.procMgr.Stop(s.Info.PID)
		s.Info.PID = 0
		s.process = nil
	}

	installDir := m.cfg.Get().InstallDir
	binPath := filepath.Join(installDir, id, s.Info.Version)
	if dirExists(binPath) {
		if err := removeAll(binPath); err != nil {
			return fmt.Errorf("删除安装目录失败: %w", err)
		}
	}

	s.Info.Installed = false
	s.Info.Status = core.StatusStopped
	return nil
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

func (m *Manager) MarkInstalled(id, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.services[id]; ok {
		s.Info.Version = version
		s.Info.Installed = true
	}
}

func (m *Manager) SaveConfig(id string, values map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}
	handler, ok := handlers[id]
	if !ok {
		return nil
	}
	return handler.SaveConfig(s, m.cfg, values)
}

func (m *Manager) Configure(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}
	handler, ok := handlers[id]
	if !ok {
		return nil
	}
	return handler.Configure(s, m.cfg)
}

func (m *Manager) CheckVersionInstalled(serviceType, version string) bool {
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return false
	}
	return dirExists(filepath.Join(installDir, serviceType, version))
}

func (m *Manager) UninstallVersion(serviceType, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return nil
	}

	if s, ok := m.services[serviceType]; ok {
		if s.Info.Version == version && s.Info.PID > 0 {
			m.procMgr.Stop(s.Info.PID)
		}
		if s.Info.Version == version {
			s.Info.Installed = false
			s.Info.Status = core.StatusStopped
			s.Info.PID = 0
			s.process = nil
		}
	}

	binPath := filepath.Join(installDir, serviceType, version)
	if dirExists(binPath) {
		return removeAll(binPath)
	}
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

func removeAll(path string) error {
	cmd := exec.Command("taskkill", "/f", "/im", "nginx.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()

	c := exec.Command("cmd", "/c", "attrib", "-r", "-s", "-h", filepath.Join(path, "*"), "/s")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	c.Run()

	rd := exec.Command("cmd", "/c", "rd", "/s", "/q", path)
	rd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return rd.Run()
}
