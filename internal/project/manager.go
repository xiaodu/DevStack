package project

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/atomicfile"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
)

type Manager struct {
	mu         sync.RWMutex
	projects   map[string]*core.Project
	filePath   string
	installDir string
	runtime    RuntimePortGetter
	resMgr     *resource.Manager
	svcMgr     *service.Manager
	rtMgr      *runtime.Manager
}

func NewManager(installDir string, runtime RuntimePortGetter, resMgr *resource.Manager) *Manager {
	m := &Manager{
		projects:   make(map[string]*core.Project),
		filePath:   filepath.Join(installDir, "config", "projects.json"),
		installDir: installDir,
		runtime:    runtime,
		resMgr:     resMgr,
	}
	m.load()
	return m
}

func (m *Manager) SetServiceManager(svcMgr *service.Manager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.svcMgr = svcMgr
}

func (m *Manager) SetRuntimeManager(rtMgr *runtime.Manager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rtMgr = rtMgr
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

	if p.Port <= 0 {
		port, err := m.resMgr.Ports().Allocate("project:"+p.ID, 0)
		if err != nil {
			return nil, fmt.Errorf("为项目分配端口失败: %w", err)
		}
		p.Port = port
	}

	m.projects[p.ID] = &p
	if err := m.save(); err != nil {
		delete(m.projects, p.ID)
		m.resMgr.Ports().Release(p.Port, "project:"+p.ID)
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

func (m *Manager) Enable(id string) error {
	m.mu.Lock()

	p, ok := m.projects[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrProjectNotFound
	}
	if p.Enabled {
		m.mu.Unlock()
		return nil
	}

	deps := make(map[string]string, len(p.Dependencies))
	for k, v := range p.Dependencies {
		deps[k] = v
	}
	domain := p.Domain
	p.Enabled = true
	ensureHostsEntry(domain, true)
	if err := m.save(); err != nil {
		p.Enabled = false
		m.mu.Unlock()
		return err
	}
	m.syncNginx()
	m.mu.Unlock()

	for capability, providerID := range deps {
		if m.svcMgr != nil {
			m.svcMgr.RegisterDependent(providerID, id)
		}
		if strings.HasPrefix(providerID, "php@") {
			if m.rtMgr != nil {
				if err := m.rtMgr.Start(providerID); err != nil {
					return fmt.Errorf("启动运行时依赖 %s (%s) 失败: %w", capability, providerID, err)
				}
			}
		} else {
			if m.svcMgr != nil {
				if err := m.svcMgr.Start(providerID); err != nil {
					return fmt.Errorf("启动服务依赖 %s (%s) 失败: %w", capability, providerID, err)
				}
			}
		}
	}
	return nil
}

func (m *Manager) Disable(id string) error {
	m.mu.Lock()

	p, ok := m.projects[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrProjectNotFound
	}
	if !p.Enabled {
		m.mu.Unlock()
		return nil
	}

	deps := make(map[string]string, len(p.Dependencies))
	for k, v := range p.Dependencies {
		deps[k] = v
	}
	domain := p.Domain
	p.Enabled = false
	ensureHostsEntry(domain, false)
	if err := m.save(); err != nil {
		p.Enabled = true
		m.mu.Unlock()
		return err
	}
	m.syncNginx()
	m.mu.Unlock()

	for _, providerID := range deps {
		if m.svcMgr != nil {
			m.svcMgr.UnregisterDependent(providerID, id)
		}
	}
	return nil
}

// nginxDirs 返回安装目录下所有已安装（含 nginx.exe）的 nginx 版本目录。
// 站点配置必须写进实际运行的那个版本；历史实现写死 1.26.0，而环境常用 1.24.0，
// 导致域名请求落入默认站点。现在对所有已安装版本同步，环境切换版本后也天然生效。
func (m *Manager) nginxDirs() []string {
	if m.installDir == "" {
		return nil
	}
	base := filepath.Join(m.installDir, "programs", "nginx")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "nginx.exe")); err != nil {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// SyncNginx 对外暴露一次全量站点配置同步（启动时迁移历史配置）。
func (m *Manager) SyncNginx() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncNginx()
}

func (m *Manager) syncNginx() {
	dirs := m.nginxDirs()
	if len(dirs) == 0 {
		return
	}
	os.MkdirAll(filepath.Join(m.installDir, "www"), 0755)

	for _, nd := range dirs {
		sitesDir := filepath.Join(nd, "conf", "sites-enabled")
		if err := os.MkdirAll(sitesDir, 0755); err != nil {
			continue
		}

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

		m.ensureNginxInclude(nd)
		m.reloadNginx(nd)
	}
}

func (m *Manager) ensureNginxInclude(nd string) {
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

func (m *Manager) reloadNginx(nd string) {
	nginxExe := filepath.Join(nd, "nginx.exe")
	if _, err := os.Stat(nginxExe); err != nil {
		return
	}
	cmd := exec.Command(nginxExe, "-s", "reload")
	// -s reload 依赖 conf/nginx.conf 里相对 pid 路径 logs/nginx.pid，工作目录必须是版本根目录
	cmd.Dir = nd
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
	return atomicfile.WriteFile(m.filePath, data, 0644)
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
