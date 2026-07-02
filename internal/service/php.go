package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type PHPHandler struct{}

func (h *PHPHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)

	phpIni := filepath.Join(phpDir, "php.ini")
	if fileExists(phpIni) {
		return nil
	}

	prod := filepath.Join(phpDir, "php.ini-production")
	dev := filepath.Join(phpDir, "php.ini-development")
	if fileExists(prod) {
		data, err := os.ReadFile(prod)
		if err != nil {
			return err
		}
		return os.WriteFile(phpIni, data, 0644)
	}
	if fileExists(dev) {
		data, err := os.ReadFile(dev)
		if err != nil {
			return err
		}
		return os.WriteFile(phpIni, data, 0644)
	}

	ini := `[PHP]
engine = On
error_reporting = E_ALL
display_errors = On
log_errors = On
error_log = "php_errors.log"
extension_dir = "ext"
cgi.fix_pathinfo = 1
cgi.force_redirect = 0
fastcgi.impersonate = 1
date.timezone = Asia/Shanghai
`
	return os.WriteFile(phpIni, []byte(ini), 0644)
}

func (h *PHPHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)
	phpExe := filepath.Join(phpDir, "php-cgi.exe")

	if !fileExists(phpExe) {
		return fmt.Errorf("PHP 可执行文件未找到: %s", phpExe)
	}

	phpIni := filepath.Join(phpDir, "php.ini")
	if !fileExists(phpIni) {
		ini := `[PHP]
engine = On
error_reporting = E_ALL
display_errors = On
display_startup_errors = On
log_errors = On
error_log = "php_errors.log"
extension_dir = "ext"
cgi.fix_pathinfo = 1
cgi.force_redirect = 0
cgi.fix_pathinfo = 1
fastcgi.impersonate = 1
fastcgi.logging = 1
date.timezone = Asia/Shanghai
`
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	s.Info.Status = core.StatusStarting
	args := []string{
		"-b", fmt.Sprintf("127.0.0.1:%d", s.Info.Port),
		"-c", phpIni,
	}

	pid, err := pm.Start("php", phpExe, args, nil, phpDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{
		pid:     pid,
		cmd:     phpExe,
		started: time.Now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	return nil
}

func (h *PHPHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	phpIni := filepath.Join(installDir, "php", s.Info.Version, "php.ini")
	if !fileExists(phpIni) {
		return h.Configure(s, cfg)
	}
	data, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	content := string(data)
	for k, v := range values {
		content = updateIniValue(content, k, v)
	}
	return os.WriteFile(phpIni, []byte(content), 0644)
}

func (h *PHPHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping
	if err := pm.Stop(s.process.pid); err != nil {
		s.Info.Status = core.StatusError
		return err
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}
