package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
	"github.com/xiaodu/DevStack/internal/atomicfile"
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
	return ""
}

func writeBootstrap(installDir string) error {
	os.MkdirAll(filepath.Dir(bootstrapPath()), 0755)
	return os.WriteFile(bootstrapPath(), []byte(installDir), 0644)
}

type Config struct {
	mu         sync.RWMutex
	viper      *viper.Viper
	installDir string
	filePath   string
	Settings   core.AppSettings
}

func NewConfig() *Config {
	return NewConfigForDir(readBootstrap())
}

// NewConfigForDir 以指定安装目录创建配置，不读写用户 bootstrap 文件（用于测试/嵌入式场景）。
func NewConfigForDir(installDir string) *Config {
	v := viper.New()
	v.SetConfigName("settings")
	v.SetConfigType("toml")
	cfgDir := filepath.Join(installDir, "config")
	v.AddConfigPath(cfgDir)

	setViperDefaults(v)

	settings := defaultSettings(installDir)

	if err := v.ReadInConfig(); err == nil {
		v.Unmarshal(&settings)
	}

	settings.InstallDir = installDir

	filePath := filepath.Join(cfgDir, "settings.toml")

	return &Config{
		viper:      v,
		installDir: installDir,
		filePath:   filePath,
		Settings:   settings,
	}
}

func setViperDefaults(v *viper.Viper) {
	def := defaultSettings("")
	v.SetDefault("installDir", def.InstallDir)
	v.SetDefault("startOnBoot", def.StartOnBoot)
	v.SetDefault("minimizeToTray", def.MinimizeToTray)
	v.SetDefault("theme", def.Theme)
	v.SetDefault("language", def.Language)
	v.SetDefault("nginxConfig.workerConnections", def.NginxConfig.WorkerConnections)
	v.SetDefault("nginxConfig.clientMaxBodySize", def.NginxConfig.ClientMaxBodySize)
	v.SetDefault("nginxConfig.gzipEnabled", def.NginxConfig.GzipEnabled)
	v.SetDefault("nginxConfig.staticCacheDays", def.NginxConfig.StaticCacheDays)
	v.SetDefault("nginxConfig.keepaliveTimeout", def.NginxConfig.KeepaliveTimeout)
	v.SetDefault("nginxConfig.serverTokens", def.NginxConfig.ServerTokens)
	v.SetDefault("phpConfig.memoryLimit", def.PHPConfig.MemoryLimit)
	v.SetDefault("phpConfig.maxExecutionTime", def.PHPConfig.MaxExecutionTime)
	v.SetDefault("phpConfig.uploadMaxFilesize", def.PHPConfig.UploadMaxFilesize)
	v.SetDefault("phpConfig.postMaxSize", def.PHPConfig.PostMaxSize)
	v.SetDefault("phpConfig.displayErrors", def.PHPConfig.DisplayErrors)
	v.SetDefault("phpConfig.opcacheEnabled", def.PHPConfig.OPcacheEnabled)
	v.SetDefault("phpConfig.realpathCacheSize", def.PHPConfig.RealpathCacheSize)
	v.SetDefault("phpConfig.maxInputTime", def.PHPConfig.MaxInputTime)
	v.SetDefault("phpConfig.maxInputVars", def.PHPConfig.MaxInputVars)
	v.SetDefault("phpConfig.maxFileUploads", def.PHPConfig.MaxFileUploads)
	v.SetDefault("phpConfig.timezone", def.PHPConfig.Timezone)
	v.SetDefault("phpConfig.defaultCharset", def.PHPConfig.DefaultCharset)
	v.SetDefault("phpConfig.fileUploads", def.PHPConfig.FileUploads)
	v.SetDefault("phpConfig.sessionGCMaxLifetime", def.PHPConfig.SessionGCMaxLifetime)
	v.SetDefault("phpConfig.logErrors", def.PHPConfig.LogErrors)
	v.SetDefault("phpConfig.errorLog", def.PHPConfig.ErrorLog)
	v.SetDefault("phpConfig.fixPathinfo", def.PHPConfig.FixPathinfo)
	v.SetDefault("mysqlConfig.innodbBufferPoolSize", def.MySQLConfig.InnoDBBufferPoolSize)
	v.SetDefault("mysqlConfig.innodbLogFileSize", def.MySQLConfig.InnoDBLogFileSize)
	v.SetDefault("mysqlConfig.innodbFlushLogAtTrxCommit", def.MySQLConfig.InnoDBFlushLogAtTrxCommit)
	v.SetDefault("mysqlConfig.maxConnections", def.MySQLConfig.MaxConnections)
	v.SetDefault("mysqlConfig.tmpTableSize", def.MySQLConfig.TmpTableSize)
	v.SetDefault("mysqlConfig.maxHeapTableSize", def.MySQLConfig.MaxHeapTableSize)
	v.SetDefault("mysqlConfig.threadCacheSize", def.MySQLConfig.ThreadCacheSize)
	v.SetDefault("mysqlConfig.tableOpenCache", def.MySQLConfig.TableOpenCache)
	v.SetDefault("mysqlConfig.bindAddress", def.MySQLConfig.BindAddress)
	v.SetDefault("mysqlConfig.port", def.MySQLConfig.Port)
	v.SetDefault("mysqlConfig.characterSet", def.MySQLConfig.CharacterSet)
	v.SetDefault("mysqlConfig.collation", def.MySQLConfig.Collation)
	v.SetDefault("mysqlConfig.sqlMode", def.MySQLConfig.SQLMode)
	v.SetDefault("mysqlConfig.skipNameResolve", def.MySQLConfig.SkipNameResolve)
	v.SetDefault("mysqlConfig.generalLog", def.MySQLConfig.GeneralLog)
	v.SetDefault("mysqlConfig.slowQueryLog", def.MySQLConfig.SlowQueryLog)
	v.SetDefault("mysqlConfig.longQueryTime", def.MySQLConfig.LongQueryTime)
	v.SetDefault("mysqlConfig.maxAllowedPacket", def.MySQLConfig.MaxAllowedPacket)
	v.SetDefault("mysqlConfig.keyBufferSize", def.MySQLConfig.KeyBufferSize)
	v.SetDefault("mysqlConfig.waitTimeout", def.MySQLConfig.WaitTimeout)
	v.SetDefault("mysqlConfig.sortBufferSize", def.MySQLConfig.SortBufferSize)
	v.SetDefault("mysqlConfig.joinBufferSize", def.MySQLConfig.JoinBufferSize)
}

func defaultSettings(installDir string) core.AppSettings {
	return core.AppSettings{
		InstallDir:     installDir,
		StartOnBoot:    false,
		MinimizeToTray: true,
		Theme:          "light",
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
			MaxInputTime:        60,
			MaxInputVars:        3000,
			MaxFileUploads:      20,
			Timezone:            "Asia/Shanghai",
			DefaultCharset:      "UTF-8",
			FileUploads:         true,
			SessionGCMaxLifetime: 7200,
			LogErrors:           true,
			ErrorLog:            "php_errors.log",
			FixPathinfo:         true,
		},
		MySQLConfig: core.MySQLSettings{
			InnoDBBufferPoolSize:      "256M",
			InnoDBLogFileSize:         "64M",
			InnoDBFlushLogAtTrxCommit: 2,
			MaxConnections:            50,
			TmpTableSize:              "64M",
			MaxHeapTableSize:          "64M",
			ThreadCacheSize:           8,
			TableOpenCache:            400,
			BindAddress:               "127.0.0.1",
			Port:                      3306,
			CharacterSet:              "utf8mb4",
			Collation:                 "utf8mb4_unicode_ci",
			SQLMode:                   "STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION",
			SkipNameResolve:           false,
			GeneralLog:                false,
			SlowQueryLog:              false,
			LongQueryTime:             2,
			MaxAllowedPacket:          "16M",
			KeyBufferSize:             "64M",
			WaitTimeout:               28800,
			SortBufferSize:            "2M",
			JoinBufferSize:            "2M",
		},
	}
}

func (c *Config) save() error {
	data, err := toml.Marshal(c.Settings)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
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
	if err := atomicfile.WriteFile(c.filePath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
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
		c.filePath = filepath.Join(s.InstallDir, "config", "settings.toml")
	}

	c.Settings = s
	return c.save()
}

func EnsureDirs(installDir string) {
	dirs := []string{
		installDir,
		filepath.Join(installDir, "config"),
		filepath.Join(installDir, "logs"),
		filepath.Join(installDir, "data"),
		filepath.Join(installDir, "data", "certs"),
		filepath.Join(installDir, "data", "mysql"),
		filepath.Join(installDir, "www"),
	}
	for _, d := range dirs {
		if d != "" {
			os.MkdirAll(d, 0755)
		}
	}
}