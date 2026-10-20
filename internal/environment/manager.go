package environment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/atomicfile"
	"github.com/xiaodu/DevStack/internal/core"
)

type Manager struct {
	mu              sync.RWMutex
	filePath        string
	envs            map[string]Environment
	builtins        []Environment
	deletedBuiltins []string
}

// persistedFile 是 environments.json 的持久化结构。
// 预设与自定义环境同样落盘；被删除的预设通过 deletedBuiltinIDs 记录，重启后不再补全。
type persistedFile struct {
	DeletedBuiltinIDs []string      `json:"deletedBuiltinIDs,omitempty"`
	Environments      []Environment `json:"environments,omitempty"`
}

func NewManager(installDir string) *Manager {
	m := &Manager{
		filePath: filepath.Join(installDir, "config", "environments.json"),
		envs:     make(map[string]Environment),
	}
	m.builtins = m.defaultBuiltins()
	if installDir != "" {
		_ = os.MkdirAll(filepath.Dir(m.filePath), 0755)
	}
	m.load()
	m.mergeBuiltins()
	return m
}

func (m *Manager) defaultBuiltins() []Environment {
	now := time.Now().Format("2006-01-02 15:04:05")
	return []Environment{
		{
			ID:          "builtin-lnmp-php74",
			Name:        "LNMP (PHP 7.4 + MySQL 5.7 + Nginx 1.20)",
			Description: "经典 LNMP 组合，兼容绝大多数国内 PHP 开源系统（WordPress、DedeCMS、TP5 等）",
			IsBuiltin:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        []string{"LAMP", "PHP", "推荐", "建站"},
			Units: []UnitSpec{
				{Kind: "nginx", Version: "1.20.2", Name: "Nginx"},
				{Kind: "mysql", Version: "5.7.44", Name: "MySQL"},
				{Kind: "php", Version: "7.4.33", Name: "PHP"},
				{Kind: "redis", Version: "7.2.5", Name: "Redis"},
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
			Units: []UnitSpec{
				{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
				{Kind: "mysql", Version: "8.0.39", Name: "MySQL"},
				{Kind: "php", Version: "8.3.12", Name: "PHP"},
				{Kind: "redis", Version: "7.2.5", Name: "Redis"},
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
			Units: []UnitSpec{
				{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
				{Kind: "redis", Version: "7.2.5", Name: "Redis"},
				{Kind: "node", Version: "20.18.0", Name: "Node.js"},
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
			Units: []UnitSpec{
				{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
				{Kind: "redis", Version: "7.2.5", Name: "Redis"},
				{Kind: "python", Version: "3.11.8", Name: "Python"},
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
			Units: []UnitSpec{
				{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
			},
		},
	}
}

// mergeBuiltins 补全默认内置预设，但跳过已被用户删除的预设 ID。
func (m *Manager) mergeBuiltins() {
	deleted := make(map[string]bool, len(m.deletedBuiltins))
	for _, id := range m.deletedBuiltins {
		deleted[id] = true
	}
	for _, b := range m.builtins {
		if deleted[b.ID] {
			continue
		}
		if _, exists := m.envs[b.ID]; !exists {
			m.envs[b.ID] = b
		}
	}
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var pf persistedFile
	if err := json.Unmarshal(data, &pf); err != nil {
		// 配置损坏时不能静默吞掉——那会让用户的环境列表凭空消失且无从恢复。
		// 保留损坏文件副本供人工修复，再继续用空列表启动。
		backup := m.filePath + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.WriteFile(backup, data, 0644)
		fmt.Fprintf(os.Stderr, "[environment] environments.json 解析失败: %v，已备份到 %s\n", err, backup)
		return
	}
	m.deletedBuiltins = pf.DeletedBuiltinIDs
	for _, e := range pf.Environments {
		if e.ID == "" {
			continue
		}
		m.envs[e.ID] = e
	}
}

func (m *Manager) save() error {
	pf := persistedFile{
		DeletedBuiltinIDs: m.deletedBuiltins,
		Environments:      make([]Environment, 0, len(m.envs)),
	}
	for _, e := range m.envs {
		pf.Environments = append(pf.Environments, e)
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(m.filePath, data, 0644)
}

func (m *Manager) List() []Environment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Environment, 0, len(m.envs))
	for _, e := range m.envs {
		result = append(result, e)
	}
	return result
}

func (m *Manager) Get(id string) (Environment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.envs[id]
	if !ok {
		return Environment{}, fmt.Errorf("environment %q not found: %w", id, core.ErrNotFound)
	}
	return e, nil
}

func (m *Manager) Create(e Environment) (Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := validateUnits(e.Units); err != nil {
		return Environment{}, err
	}
	if e.ID == "" {
		e.ID = NewID()
	}
	if _, exists := m.envs[e.ID]; exists {
		return Environment{}, fmt.Errorf("environment %q already exists", e.ID)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if e.CreatedAt == "" {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	e.IsBuiltin = false
	if e.Units == nil {
		e.Units = []UnitSpec{}
	}
	m.envs[e.ID] = e
	if err := m.save(); err != nil {
		delete(m.envs, e.ID)
		return Environment{}, err
	}
	return e, nil
}

func (m *Manager) Update(e Environment) (Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.envs[e.ID]
	if !ok {
		return Environment{}, fmt.Errorf("environment %q not found: %w", e.ID, core.ErrNotFound)
	}
	if err := validateUnits(e.Units); err != nil {
		return Environment{}, err
	}
	e.IsBuiltin = old.IsBuiltin
	e.CreatedAt = old.CreatedAt
	e.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if e.Units == nil {
		e.Units = []UnitSpec{}
	}
	m.envs[e.ID] = e
	if err := m.save(); err != nil {
		m.envs[e.ID] = old
		return Environment{}, err
	}
	return e, nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.envs[id]
	if !ok {
		return nil
	}
	if e.IsBuiltin {
		m.deletedBuiltins = append(m.deletedBuiltins, id)
	}
	delete(m.envs, id)
	if err := m.save(); err != nil {
		m.envs[id] = e
		if e.IsBuiltin {
			m.deletedBuiltins = m.deletedBuiltins[:len(m.deletedBuiltins)-1]
		}
		return err
	}
	return nil
}

// supportedUnitKinds 是环境模板允许的组件类型，必须与环境编辑器 catalog
// 以及实际能启动的 provider/runtime 保持一致（memcached 已具备安装与服务能力）。
var supportedUnitKinds = []string{"nginx", "mysql", "redis", "memcached", "php", "node", "python"}

var validKinds = func() map[string]bool {
	m := make(map[string]bool, len(supportedUnitKinds))
	for _, k := range supportedUnitKinds {
		m[k] = true
	}
	return m
}()

func validateUnits(units []UnitSpec) error {
	for i, u := range units {
		if strings.TrimSpace(u.Kind) == "" {
			return fmt.Errorf("第 %d 个组件的 kind 不能为空", i+1)
		}
		if !validKinds[u.Kind] {
			return fmt.Errorf("第 %d 个组件的 kind %q 不合法，支持: %s", i+1, u.Kind, strings.Join(supportedUnitKinds, ", "))
		}
		if strings.TrimSpace(u.Version) == "" {
			return fmt.Errorf("第 %d 个组件 %q 的 version 不能为空", i+1, u.Kind)
		}
	}
	return nil
}
