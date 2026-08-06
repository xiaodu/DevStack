package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/runtime"
)

// PHPHandler 负责 PHP 全局配置（php.ini）的读写。PHP 属于运行时，SaveConfig 不依赖 ServiceInstance。
type PHPHandler struct{}

func (h *PHPHandler) Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error {
	return nil
}

func (h *PHPHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	settings := cfg.Get()
	pc := settings.PHPConfig
	changed := false

	if v, ok := values["memoryLimit"]; ok {
		pc.MemoryLimit = v
		changed = true
	}
	if v, ok := values["maxExecutionTime"]; ok {
		pc.MaxExecutionTime = parseInt(v)
		changed = true
	}
	if v, ok := values["uploadMaxFilesize"]; ok {
		pc.UploadMaxFilesize = v
		changed = true
	}
	if v, ok := values["postMaxSize"]; ok {
		pc.PostMaxSize = v
		changed = true
	}
	if v, ok := values["displayErrors"]; ok {
		pc.DisplayErrors = toBool(v)
		changed = true
	}
	if v, ok := values["opcacheEnabled"]; ok {
		pc.OPcacheEnabled = toBool(v)
		changed = true
	}
	if v, ok := values["realpathCacheSize"]; ok {
		pc.RealpathCacheSize = v
		changed = true
	}
	if v, ok := values["maxInputTime"]; ok {
		pc.MaxInputTime = parseInt(v)
		changed = true
	}
	if v, ok := values["maxInputVars"]; ok {
		pc.MaxInputVars = parseInt(v)
		changed = true
	}
	if v, ok := values["maxFileUploads"]; ok {
		pc.MaxFileUploads = parseInt(v)
		changed = true
	}
	if v, ok := values["timezone"]; ok {
		pc.Timezone = v
		changed = true
	}
	if v, ok := values["defaultCharset"]; ok {
		pc.DefaultCharset = v
		changed = true
	}
	if v, ok := values["fileUploads"]; ok {
		pc.FileUploads = toBool(v)
		changed = true
	}
	if v, ok := values["sessionGCMaxLifetime"]; ok {
		pc.SessionGCMaxLifetime = parseInt(v)
		changed = true
	}
	if v, ok := values["logErrors"]; ok {
		pc.LogErrors = toBool(v)
		changed = true
	}
	if v, ok := values["errorLog"]; ok {
		pc.ErrorLog = v
		changed = true
	}
	if v, ok := values["fixPathinfo"]; ok {
		pc.FixPathinfo = toBool(v)
		changed = true
	}

	if changed {
		settings.PHPConfig = pc
		if err := cfg.Update(settings); err != nil {
			return err
		}
	}

	// 为所有已安装的 PHP 版本重新生成 php.ini
	phpRoot := filepath.Join(settings.InstallDir, "programs", "php")
	entries, err := os.ReadDir(phpRoot)
	if err != nil {
		return nil // 尚无 PHP 安装，配置已保存，稍后启动时生成
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		version := e.Name()
		phpDir := filepath.Join(phpRoot, version)
		if err := runtime.RegeneratePHPIni(cfg, phpDir, version); err != nil {
			return fmt.Errorf("写入 %s 的 php.ini 失败: %w", version, err)
		}
	}
	return nil
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func toBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}