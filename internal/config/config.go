package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaodu/DevStack/internal/core"
)

type Config struct {
	mu       sync.RWMutex
	filePath string
	Settings core.AppSettings
}

func NewConfig(dataDir string) *Config {
	cfg := &Config{
		filePath: filepath.Join(dataDir, "settings.json"),
	}
	cfg.Settings = core.AppSettings{
		InstallDir:     filepath.Join(dataDir, "apps"),
		DataDir:        dataDir,
		StartOnBoot:    false,
		MinimizeToTray: true,
		Theme:          "dark",
		Language:       "zh-CN",
	}
	cfg.Load()
	return cfg
}

func (c *Config) Load() {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return
	}
	var s core.AppSettings
	if json.Unmarshal(data, &s) == nil {
		c.Settings = s
	}
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, err := json.MarshalIndent(c.Settings, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(c.filePath), 0755)
	return os.WriteFile(c.filePath, data, 0644)
}

func (c *Config) Get() core.AppSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Settings
}

func (c *Config) Update(s core.AppSettings) error {
	c.mu.Lock()
	c.Settings = s
	c.mu.Unlock()
	return c.Save()
}
