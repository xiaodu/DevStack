package project

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
)

type Manager struct {
	mu         sync.RWMutex
	projects   map[string]*core.Project
	filePath   string
	installDir string
	nginxVer   string
	runtime    RuntimePortGetter
}

func NewManager(installDir, nginxVer string, runtime RuntimePortGetter) *Manager {
	m := &Manager{
		projects:   make(map[string]*core.Project),
		filePath:   filepath.Join(installDir, "data", "projects.json"),
		installDir: installDir,
		nginxVer:   nginxVer,
		runtime:    runtime,
	}
	m.load()
	return m
}

func (m *Manager) List() []core.Project {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]core.Project, 0, len(m.projects))
	for _, p := range m.projects {
		result = append(result, *p)
	}
	return result
}

func (m *Manager) Get(id string) (core.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.projects[id]
	if !ok {
		return core.Project{}, core.ErrProjectNotFound
	}
	return *p, nil
}

func (m *Manager) Create(p core.Project) (*core.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p.ID == "" {
		p.ID = fmt.Sprintf("proj_%d", time.Now().UnixMilli())
	}
	p.CreatedAt = time.Now().Format("2006-01-02 15:04:05")

	m.projects[p.ID] = &p
	if err := m.save(); err != nil {
		delete(m.projects, p.ID)
		return nil, err
	}
	m.syncNginx()
	if p.Enabled {
		if err := ensureHostsEntry(p.Domain, true); err != nil {
			return &p, err
		}
	}
	return &p, nil
}

func (m *Manager) Update(p core.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old, ok := m.projects[p.ID]
	if !ok {
		return core.ErrProjectNotFound
	}
	if old.Domain != p.Domain {
		ensureHostsEntry(old.Domain, false)
		if p.Enabled {
			ensureHostsEntry(p.Domain, true)
		}
	}
	m.projects[p.ID] = &p
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.projects[id]
	if !ok {
		return core.ErrProjectNotFound
	}
	ensureHostsEntry(p.Domain, false)
	delete(m.projects, id)
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) Toggle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.projects[id]
	if !ok {
		return core.ErrProjectNotFound
	}
	p.Enabled = !p.Enabled
	if p.Enabled {
		ensureHostsEntry(p.Domain, true)
	} else {
		ensureHostsEntry(p.Domain, false)
	}
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) nginxDir() string {
	if m.installDir == "" || m.nginxVer == "" {
		return ""
	}
	return filepath.Join(m.installDir, "nginx", m.nginxVer)
}

func (m *Manager) syncNginx() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	sitesDir := filepath.Join(nd, "conf", "sites-enabled")
	os.MkdirAll(sitesDir, 0755)
	os.MkdirAll(filepath.Join(m.installDir, "www"), 0755)

	entries, _ := os.ReadDir(sitesDir)
	for _, e := range entries {
		os.Remove(filepath.Join(sitesDir, e.Name()))
	}

	for _, p := range m.projects {
		if !p.Enabled {
			continue
		}
		conf := m.buildConfig(*p)
		confPath := filepath.Join(sitesDir, p.ID+".conf")
		os.WriteFile(confPath, []byte(conf), 0644)
	}

	m.ensureNginxInclude()
	m.reloadNginx()
}

func (m *Manager) ensureNginxInclude() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	nginxConf := filepath.Join(nd, "conf", "nginx.conf")
	data, err := os.ReadFile(nginxConf)
	if err != nil {
		return
	}
	includeLine := "include sites-enabled/*.conf;"
	if !containsString(string(data), includeLine) {
		patch := "\nhttp {\n    " + includeLine + "\n}\n"
		f, _ := os.OpenFile(nginxConf, os.O_APPEND|os.O_WRONLY, 0644)
		if f != nil {
			f.WriteString(patch)
			f.Close()
		}
	}
}

func (m *Manager) reloadNginx() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	nginxExe := filepath.Join(nd, "nginx.exe")
	if _, err := os.Stat(nginxExe); err != nil {
		return
	}
	cmd := exec.Command(nginxExe, "-s", "reload")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var projects []core.Project
	if json.Unmarshal(data, &projects) != nil {
		return
	}
	for i := range projects {
		m.projects[projects[i].ID] = &projects[i]
	}
}

func (m *Manager) save() error {
	projects := make([]core.Project, 0, len(m.projects))
	for _, p := range m.projects {
		projects = append(projects, *p)
	}
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(m.filePath), 0755)
	return os.WriteFile(m.filePath, data, 0644)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && containsFunc(s, substr)
}

func containsFunc(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
