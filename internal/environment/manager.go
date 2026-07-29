package environment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
)

// Manager 管理 EnvironmentProfile（环境模板）。持久化到 data/environments.json，
// 并内置一套预设环境供用户创建项目时直接选用。
type Manager struct {
	mu        sync.RWMutex
	filePath  string
	profiles  map[string]EnvironmentProfile
	builtins  []EnvironmentProfile
}

func NewManager(installDir string) *Manager {
	m := &Manager{
		filePath: filepath.Join(installDir, "config", "environments.json"),
		profiles: make(map[string]EnvironmentProfile),
	}
	m.builtins = m.defaultBuiltins()
	_ = os.MkdirAll(filepath.Dir(m.filePath), 0755)
	m.load()
	m.ensureBuiltinsPresent()
	return m
}

// defaultBuiltins 内置常见环境模板，用于创建项目时快速选择
func (m *Manager) defaultBuiltins() []EnvironmentProfile {
	now := time.Now().Format("2006-01-02 15:04:05")
	return []EnvironmentProfile{
		{
			ID:          "builtin-lnmp-php74",
			Name:        "LNMP (PHP 7.4 + MySQL 5.7 + Nginx 1.20)",
			Description: "经典 LNMP 组合，兼容绝大多数国内 PHP 开源系统（WordPress、DedeCMS、TP5 等）",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"LAMP", "PHP", "推荐", "建站"},
			Services: map[string]EnvironmentServiceSpec{
				"mysql": {ID: "mysql", Version: "5.7.44"},
				"nginx": {ID: "nginx", Version: "1.20.2"},
				"redis": {ID: "redis", Version: "7.2.5"},
			},
			Runtimes: map[string]EnvironmentRuntimeSpec{
				"php": {RuntimeID: "php", Version: "7.4.33", Default: true},
			},
			Dependencies: map[string]string{
				"service:redis": "redis",
			},
		},
		{
			ID:          "builtin-modern-php8",
			Name:        "现代 PHP 全栈 (PHP 8.3 + MySQL 8.0 + Redis 7 + Nginx 1.24)",
			Description: "面向 Laravel / Symfony / ThinkPHP 8 等现代框架的高性能组合",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"PHP8", "Laravel", "高性能"},
			Services: map[string]EnvironmentServiceSpec{
				"mysql": {ID: "mysql", Version: "8.0.39"},
				"nginx": {ID: "nginx", Version: "1.24.0"},
				"redis": {ID: "redis", Version: "7.2.5"},
			},
			Runtimes: map[string]EnvironmentRuntimeSpec{
				"php": {RuntimeID: "php", Version: "8.3.12", Default: true},
			},
			Dependencies: map[string]string{
				"service:redis": "redis",
			},
		},
		{
			ID:          "builtin-node-web",
			Name:        "Node 全栈 (Node 20 + Nginx 1.24 + Redis 7)",
			Description: "适用于 Vue/Next/Nuxt 前端 SSR、Koa/Nest/Egg 服务端项目",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"Node", "SSR", "Vue", "React"},
			Services: map[string]EnvironmentServiceSpec{
				"nginx": {ID: "nginx", Version: "1.24.0"},
				"redis": {ID: "redis", Version: "7.2.5"},
			},
			Runtimes: map[string]EnvironmentRuntimeSpec{
				"node": {RuntimeID: "node", Version: "20.18.0", Default: true},
			},
		},
		{
			ID:          "builtin-python-web",
			Name:        "Python Web (Python 3.11 + Redis 7 + Nginx 1.24)",
			Description: "适用于 Django / Flask / FastAPI / Tornado 等 Python Web 框架",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"Python", "Django", "FastAPI"},
			Services: map[string]EnvironmentServiceSpec{
				"nginx": {ID: "nginx", Version: "1.24.0"},
				"redis": {ID: "redis", Version: "7.2.5"},
			},
			Runtimes: map[string]EnvironmentRuntimeSpec{
				"python": {RuntimeID: "python", Version: "3.11.8", Default: true},
			},
		},
		{
			ID:          "builtin-static-nginx",
			Name:        "纯静态站点 (Nginx 1.24)",
			Description: "无后端依赖，仅用 Nginx 托管纯静态 HTML、打包后的前端产物",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"静态", "纯前端"},
			Services: map[string]EnvironmentServiceSpec{
				"nginx": {ID: "nginx", Version: "1.24.0"},
			},
		},
	}
}

// ensureBuiltinsPresent 保证每次启动时把内置 profile 都注入 profiles map，
// 以便用户通过 List/APIs 统一可见（但不能被删除）
func (m *Manager) ensureBuiltinsPresent() {
	for _, b := range m.builtins {
		if _, exists := m.profiles[b.ID]; !exists {
			m.profiles[b.ID] = b
		}
	}
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var list []EnvironmentProfile
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	for _, p := range list {
		if p.ID == "" {
			continue
		}
		m.profiles[p.ID] = p
	}
}

func (m *Manager) save() error {
	list := make([]EnvironmentProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		if p.IsBuiltin {
			// 预设不写入 JSON，始终在启动时按 defaultBuiltins 注入，方便后续升级代码时平滑更新
			continue
		}
		list = append(list, p)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, data, 0644)
}

// List 返回环境模板列表（含内置和自定义）。usage 回调可选，用于注入 ProjectCount。
func (m *Manager) List(usage func(id string) int) []EnvironmentProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]EnvironmentProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		result = append(result, p)
	}
	return result
}

// Get 按 ID 返回单个环境模板
func (m *Manager) Get(id string) (EnvironmentProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.profiles[id]
	if !ok {
		return EnvironmentProfile{}, fmt.Errorf("environment profile %q not found: %w", id, core.ErrNotFound)
	}
	return p, nil
}

// Create 创建一个自定义环境模板
func (m *Manager) Create(p EnvironmentProfile) (EnvironmentProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == "" {
		p.ID = NewID()
	}
	if _, exists := m.profiles[p.ID]; exists {
		return EnvironmentProfile{}, fmt.Errorf("environment profile %q already exists", p.ID)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	p.IsCustom = true
	p.IsBuiltin = false
	if p.Services == nil {
		p.Services = make(map[string]EnvironmentServiceSpec)
	}
	if p.Runtimes == nil {
		p.Runtimes = make(map[string]EnvironmentRuntimeSpec)
	}
	m.profiles[p.ID] = p
	if err := m.save(); err != nil {
		delete(m.profiles, p.ID)
		return EnvironmentProfile{}, err
	}
	return p, nil
}

// Update 更新用户自定义环境模板
func (m *Manager) Update(p EnvironmentProfile) (EnvironmentProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.profiles[p.ID]
	if !ok {
		return EnvironmentProfile{}, fmt.Errorf("environment profile %q not found: %w", p.ID, core.ErrNotFound)
	}
	if old.IsBuiltin {
		return EnvironmentProfile{}, fmt.Errorf("built-in environment profile %q cannot be modified", p.ID)
	}
	p.IsBuiltin = false
	p.IsCustom = true
	p.CreatedAt = old.CreatedAt
	p.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if p.Services == nil {
		p.Services = make(map[string]EnvironmentServiceSpec)
	}
	if p.Runtimes == nil {
		p.Runtimes = make(map[string]EnvironmentRuntimeSpec)
	}
	m.profiles[p.ID] = p
	if err := m.save(); err != nil {
		m.profiles[p.ID] = old
		return EnvironmentProfile{}, err
	}
	return p, nil
}

// Delete 删除自定义环境模板，内置模板禁止删除
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[id]
	if !ok {
		return nil
	}
	if p.IsBuiltin {
		return fmt.Errorf("built-in environment profile %q cannot be deleted", id)
	}
	delete(m.profiles, id)
	if err := m.save(); err != nil {
		m.profiles[id] = p
		return err
	}
	return nil
}

// SaveProjectAsEnvironment 以现有项目的版本信息为基础，克隆出一个自定义环境模板
func (m *Manager) SaveProjectAsEnvironment(projectID string, name string, description string,
	serviceVersions map[string]string, runtimeVersions map[string]string) (EnvironmentProfile, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	svcs := make(map[string]EnvironmentServiceSpec)
	for svcID, ver := range serviceVersions {
		if ver == "" {
			continue
		}
		svcs[svcID] = EnvironmentServiceSpec{ID: svcID, Version: ver}
	}

	rts := make(map[string]EnvironmentRuntimeSpec)
	first := true
	for rtID, ver := range runtimeVersions {
		if ver == "" {
			continue
		}
		spec := EnvironmentRuntimeSpec{RuntimeID: rtID, Version: ver}
		if first {
			spec.Default = true
			first = false
		}
		rts[rtID] = spec
	}

	profile := EnvironmentProfile{
		ID:          NewID(),
		Name:        name,
		Description: description,
		IsBuiltin:   false,
		IsCustom:    true,
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		UpdatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		Services:    svcs,
		Runtimes:    rts,
		Tags:        []string{"自定义", "from:" + projectID},
	}
	m.profiles[profile.ID] = profile
	if err := m.save(); err != nil {
		delete(m.profiles, profile.ID)
		return EnvironmentProfile{}, err
	}
	return profile, nil
}

// ResolveProfileVersions 给定一个 profile，把其中声明的版本散列返回给调用方（用于填充 Project）
func ResolveProfileVersions(profile EnvironmentProfile) (
	serviceVersions map[string]string,
	runtimeVersions map[string]string,
	dependencies map[string]string,
) {
	serviceVersions = make(map[string]string)
	for _, svc := range profile.Services {
		serviceVersions[svc.ID] = svc.Version
	}
	runtimeVersions = make(map[string]string)
	for _, rt := range profile.Runtimes {
		runtimeVersions[rt.RuntimeID] = rt.Version
	}
	dependencies = make(map[string]string)
	for k, v := range profile.Dependencies {
		dependencies[k] = v
	}
	return
}
