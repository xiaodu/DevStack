package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
)

type Manager struct {
	mu       sync.RWMutex
	sites    map[string]*core.Site
	filePath string
}

func NewManager(dataDir string) *Manager {
	m := &Manager{
		sites:    make(map[string]*core.Site),
		filePath: filepath.Join(dataDir, "sites.json"),
	}
	m.load()
	return m
}

func (m *Manager) List() []core.Site {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]core.Site, 0, len(m.sites))
	for _, s := range m.sites {
		result = append(result, *s)
	}
	return result
}

func (m *Manager) Get(id string) (core.Site, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sites[id]
	if !ok {
		return core.Site{}, fmt.Errorf("站点 %s 未找到", id)
	}
	return *s, nil
}

func (m *Manager) Create(s core.Site) (*core.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.ID == "" {
		s.ID = fmt.Sprintf("site_%d", time.Now().UnixMilli())
	}
	s.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	s.Enabled = true

	m.sites[s.ID] = &s
	if err := m.save(); err != nil {
		delete(m.sites, s.ID)
		return nil, err
	}
	return &s, nil
}

func (m *Manager) Update(s core.Site) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sites[s.ID]; !ok {
		return fmt.Errorf("站点 %s 未找到", s.ID)
	}
	m.sites[s.ID] = &s
	return m.save()
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sites[id]; !ok {
		return fmt.Errorf("站点 %s 未找到", id)
	}
	delete(m.sites, id)
	return m.save()
}

func (m *Manager) Toggle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sites[id]
	if !ok {
		return fmt.Errorf("站点 %s 未找到", id)
	}
	s.Enabled = !s.Enabled
	return m.save()
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var sites []core.Site
	if json.Unmarshal(data, &sites) != nil {
		return
	}
	for i := range sites {
		m.sites[sites[i].ID] = &sites[i]
	}
}

func (m *Manager) save() error {
	sites := make([]core.Site, 0, len(m.sites))
	for _, s := range m.sites {
		sites = append(sites, *s)
	}
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(m.filePath), 0755)
	return os.WriteFile(m.filePath, data, 0644)
}
