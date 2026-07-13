package service

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	stopped chan struct{}
}

type execWrapper struct {
	pid       int
	mpid      int   // master PID (for services like nginx with master/worker)
	extraPids []int // additional PIDs (e.g. multiple php-cgi workers)
	cmd       string
	env       []string
	started   time.Time
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
			Version: "8.3.32", Port: 9000, Status: core.StatusStopped,
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

var serviceOrder = []string{"nginx", "php", "mysql", "redis", "node", "python"}

func isPortAlive(port, pid int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return process.IsProcessRunning(pid)
}

func (m *Manager) List() []core.ServiceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	portPIDMap := process.FindAllPIDsByPort()

	result := make([]core.ServiceInfo, 0, len(m.services))
	for _, id := range serviceOrder {
		s, ok := m.services[id]
		if !ok {
			continue
		}
		info := s.Info

		switch info.Status {
		case core.StatusStarting, core.StatusStopping:
			// Keep transitional states as-is

		case core.StatusRunning:
			stillRunning := false
			if s.process != nil && s.process.pid > 0 && m.procMgr.IsRunning(s.process.pid) {
				stillRunning = true
			}
			if !stillRunning && info.Port > 0 {
				if pid, ok := portPIDMap[info.Port]; ok {
					if isPortAlive(info.Port, pid) {
						stillRunning = true
						info.PID = pid
						if s.process == nil || s.process.pid != pid {
							s.process = &execWrapper{pid: pid, cmd: "", started: time.Now()}
						}
					}
				}
			}
			if !stillRunning {
				info.Status = core.StatusStopped
				info.PID = 0
				s.process = nil
			}

		case core.StatusStopped:
			if info.Port > 0 {
				if pid, ok := portPIDMap[info.Port]; ok {
					if isPortAlive(info.Port, pid) {
						info.Status = core.StatusRunning
						info.PID = pid
						s.process = &execWrapper{pid: pid, cmd: "", started: time.Now()}
					}
				}
			}
		}

		if info.Status == core.StatusRunning && s.process != nil {
			info.Uptime = time.Since(s.process.started).Round(time.Second).String()
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

	info := s.Info

	switch info.Status {
	case core.StatusStarting, core.StatusStopping:
		// Keep transitional states as-is

	case core.StatusRunning:
		stillRunning := false
		if s.process != nil && s.process.pid > 0 && m.procMgr.IsRunning(s.process.pid) {
			stillRunning = true
		}
		if !stillRunning && info.Port > 0 {
			if pid := process.FindPIDByPort(info.Port); pid > 0 {
				if isPortAlive(info.Port, pid) {
					stillRunning = true
					info.PID = pid
				}
			}
		}
		if !stillRunning {
			info.Status = core.StatusStopped
			info.PID = 0
		}

	case core.StatusStopped:
		if info.Port > 0 {
			if pid := process.FindPIDByPort(info.Port); pid > 0 {
				if isPortAlive(info.Port, pid) {
					info.Status = core.StatusRunning
					info.PID = pid
				}
			}
		}
	}

	return info, nil
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()

	s, ok := m.services[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}
	if s.Info.Status == core.StatusRunning {
		m.mu.Unlock()
		return nil
	}

	handler, ok := handlers[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("不支持的的服务类型: %s", id)
	}

	err := handler.Start(s, m.cfg, m.procMgr)
	if err != nil {
		m.mu.Unlock()
		return err
	}

	s.stopped = make(chan struct{})
	m.mu.Unlock()

	go m.watchProcess(id)

	return nil
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}

	// Signal watcher to stop (no-op if already stopped/not watching)
	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}

	if s.process != nil && s.process.pid > 0 {
		handler, ok := handlers[id]
		if !ok {
			return fmt.Errorf("不支持的的服务类型: %s", id)
		}
		return handler.Stop(s, m.procMgr)
	}

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

func (m *Manager) watchProcess(id string) {
	m.mu.RLock()
	s, ok := m.services[id]
	if !ok || s.process == nil || s.stopped == nil {
		m.mu.RUnlock()
		return
	}
	pid := s.process.pid
	stopped := s.stopped
	m.mu.RUnlock()

	select {
	case <-m.procMgr.WaitChan(pid):
	case <-stopped:
		return
	}

	m.mu.Lock()
	s2 := m.services[id]
	if s2 == nil || s2.stopped == nil || s2.Info.Status == core.StatusStopped || s2.Info.Status == core.StatusStopping {
		m.mu.Unlock()
		return
	}
	// Clean up stale process ref and restart
	s2.process = nil
	s2.Info.PID = 0
	m.mu.Unlock()

	time.Sleep(2 * time.Second)
	m.Start(id)
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

	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}

	if s.Info.PID > 0 {
		m.procMgr.Stop(s.Info.PID)
		s.Info.PID = 0
		s.process = nil
	}

	installDir := m.cfg.Get().InstallDir
	binPath := filepath.Join(installDir, id, s.Info.Version)
	if dirExists(binPath) {
		if err := removeAll(id, binPath); err != nil {
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

func (m *Manager) GetPHPExtensions(version string) ([]PHPExtensionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "php", Type: core.TypePHP, Version: version,
		},
	}
	handler := &PHPHandler{}
	return handler.GetExtensions(s, m.cfg)
}

func (m *Manager) SavePHPExtensions(version string, extensionNames []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &ServiceInstance{
		Info: core.ServiceInfo{
			ID: "php", Type: core.TypePHP, Version: version,
		},
	}
	handler := &PHPHandler{}
	return handler.SaveExtensions(s, m.cfg, extensionNames)
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

func (m *Manager) CheckMySQLRootAuth() (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.services["mysql"]
	if !ok {
		return false, fmt.Errorf("MySQL 服务未注册")
	}

	if !s.Info.Installed {
		return false, fmt.Errorf("MySQL 未安装")
	}

	installDir := m.cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "mysql", s.Info.Version)
	mysqlBin := filepath.Join(mysqlDir, "bin", "mysql.exe")

	if _, err := os.Stat(mysqlBin); err != nil {
		return false, fmt.Errorf("mysql 客户端未找到")
	}

	port := s.Info.Port
	if port == 0 {
		port = 3306
	}

	pid := process.FindPIDByPort(port)
	if pid <= 0 {
		return false, fmt.Errorf("MySQL 未运行，请先启动")
	}

	args := []string{
		fmt.Sprintf("--port=%d", port),
		"-u", "root",
		"-e", "SELECT 1;",
	}

	cmd := exec.Command(mysqlBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "Access denied") {
			return true, nil
		}
		return false, fmt.Errorf("检查失败: %s", output)
	}
	return false, nil
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
			if s.stopped != nil {
				close(s.stopped)
				s.stopped = nil
			}
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
		return removeAll(serviceType, binPath)
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m *Manager) GetInstalledVersions(serviceType string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return nil
	}
	svcDir := filepath.Join(installDir, serviceType)
	entries, err := os.ReadDir(svcDir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && dirExists(filepath.Join(svcDir, e.Name())) {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var serviceExecutables = map[string][]string{
	"nginx":  {"nginx.exe"},
	"mysql":  {"mysqld.exe"},
	"redis":  {"redis-server.exe"},
	"php":    {"php-cgi.exe", "php.exe"},
	"node":   {"node.exe"},
	"python": {"python.exe"},
}

func removeAll(serviceType, path string) error {
	if exes, ok := serviceExecutables[serviceType]; ok {
		for _, exe := range exes {
			cmd := exec.Command("taskkill", "/f", "/im", exe)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Run()
		}
	}

	c := exec.Command("cmd", "/c", "attrib", "-r", "-s", "-h", filepath.Join(path, "*"), "/s")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	c.Run()

	rd := exec.Command("cmd", "/c", "rd", "/s", "/q", path)
	rd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return rd.Run()
}
