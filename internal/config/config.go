package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaodu/DevStack/internal/core"
)

const bootstrapDirName = ".devstack"
const bootstrapFileName = "basedir"

func bootstrapPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, bootstrapDirName, bootstrapFileName)
}

func readBootstrap() string {
	data, err := os.ReadFile(bootstrapPath())
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	// Migration: read old settings.json for installDir
	home, _ := os.UserHomeDir()
	oldPath := filepath.Join(home, bootstrapDirName, "settings.json")
	data, err = os.ReadFile(oldPath)
	if err == nil {
		var old struct {
			InstallDir string `json:"installDir"`
		}
		if json.Unmarshal(data, &old) == nil && old.InstallDir != "" {
			os.MkdirAll(filepath.Dir(bootstrapPath()), 0755)
			os.WriteFile(bootstrapPath(), []byte(old.InstallDir), 0644)
			return old.InstallDir
		}
	}
	return ""
}

func writeBootstrap(installDir string) error {
	os.MkdirAll(filepath.Dir(bootstrapPath()), 0755)
	return os.WriteFile(bootstrapPath(), []byte(installDir), 0644)
}

type Config struct {
	mu       sync.RWMutex
	filePath string
	Settings core.AppSettings
}

func NewConfig() *Config {
	installDir := readBootstrap()
	cfg := &Config{
		filePath: filepath.Join(installDir, "data", "settings.json"),
	}
	cfg.Settings = core.AppSettings{
		InstallDir:     installDir,
		StartOnBoot:    false,
		MinimizeToTray: true,
		Theme:          "dark",
		Language:       "zh-CN",
		NginxConfig: core.NginxSettings{
			WorkerConnections:   2048,
			ClientMaxBodySize:   "64m",
			GzipEnabled:         true,
			StaticCacheDays:     7,
			KeepaliveTimeout:    65,
			ServerTokens:        false,
		},
		PHPConfig: core.PHPSettings{
			MemoryLimit:         "256M",
			MaxExecutionTime:    300,
			UploadMaxFilesize:   "64M",
			PostMaxSize:         "64M",
			DisplayErrors:       false,
			OPcacheEnabled:      true,
			RealpathCacheSize:   "4096K",
		},
		MySQLConfig: core.MySQLSettings{
			InnoDBBufferPoolSize:     "256M",
			InnoDBLogFileSize:        "64M",
			InnoDBFlushLogAtTrxCommit: 2,
			MaxConnections:           50,
			TmpTableSize:             "64M",
			MaxHeapTableSize:         "64M",
			ThreadCacheSize:          8,
			TableOpenCache:           400,
			BindAddress:              "127.0.0.1",
			Port:                     3306,
		},
	}
	cfg.load()
	return cfg
}

func (c *Config) load() {
	if c.Settings.InstallDir == "" {
		return
	}
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		// 文件不存在时创建默认配置
		c.save()
		return
	}
	var s core.AppSettings
	if json.Unmarshal(data, &s) == nil {
		s.InstallDir = c.Settings.InstallDir // always use bootstrap as source of truth
		c.Settings = s
	}
}

func (c *Config) save() error {
	data, err := json.MarshalIndent(c.Settings, "", "  ")
	if err != nil {
		return err
	}
	if c.Settings.InstallDir != "" {
		EnsureDirs(c.Settings.InstallDir)
	} else {
		dir := filepath.Dir(c.filePath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(c.filePath, data, 0644); err != nil {
		return err
	}
	// 写入后读回验证
	verify, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("配置文件写入验证失败: %w", err)
	}
	var v core.AppSettings
	if err := json.Unmarshal(verify, &v); err != nil {
		return fmt.Errorf("配置文件解析验证失败: %w", err)
	}
	if v.Theme != c.Settings.Theme {
		return fmt.Errorf("配置文件验证失败: 期望 theme=%s, 实际 theme=%s", c.Settings.Theme, v.Theme)
	}
	return nil
}

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.save()
}

func (c *Config) Get() core.AppSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Settings
}

// FilePath 返回当前配置文件的绝对路径
func (c *Config) FilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	abs, err := filepath.Abs(c.filePath)
	if err != nil {
		return c.filePath
	}
	return abs
}

func (c *Config) Update(s core.AppSettings) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s.InstallDir != c.Settings.InstallDir {
		if err := writeBootstrap(s.InstallDir); err != nil {
			return err
		}
		c.filePath = filepath.Join(s.InstallDir, "data", "settings.json")
	}

	c.Settings = s
	return c.save()
}

func EnsureDirs(installDir string) {
	dirs := []string{
		installDir,
		filepath.Join(installDir, "data"),
		filepath.Join(installDir, "data", "logs"),
		filepath.Join(installDir, "data", "certs"),
		filepath.Join(installDir, "db"),
		filepath.Join(installDir, "db", "mysql"),
		filepath.Join(installDir, "www"),
		filepath.Join(installDir, "nginx"),
		filepath.Join(installDir, "mysql"),
		filepath.Join(installDir, "redis"),
		filepath.Join(installDir, "php"),
		filepath.Join(installDir, "node"),
		filepath.Join(installDir, "python"),
	}
	for _, d := range dirs {
		if d != "" {
			os.MkdirAll(d, 0755)
		}
	}
}
