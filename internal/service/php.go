package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/runtime"
)

type PHPHandler struct {
	BaseHandler
}

func (h *PHPHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)
	phpIni := filepath.Join(phpDir, "php.ini")
	if !fileExists(phpIni) {
		ini := runtime.GeneratePHPIniContent(cfg, phpDir, s.Info.Version)
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	// Generate PHP-FPM config for Unix systems
	// Check if we're on a Unix system (php-fpm binary exists)
	phpFpm := filepath.Join(phpDir, "sbin", "php-fpm")
	if fileExists(phpFpm) {
		fpmDir := filepath.Join(phpDir, "etc", "php-fpm.d")
		os.MkdirAll(fpmDir, 0755)
		port := runtime.GetPHPVersionPort(cfg, s.Info.Version)
		if port == 0 {
			port = 9000
		}
		fpmConf := filepath.Join(fpmDir, "www.conf")
		if !fileExists(fpmConf) {
			conf := runtime.GeneratePHPFPMConfig(port)
			os.WriteFile(fpmConf, []byte(conf), 0644)
		}
	}

	return nil
}

func (h *PHPHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	version := s.Info.Version
	phpDir := filepath.Join(installDir, "php", version)
	phpExe := filepath.Join(phpDir, "php-cgi.exe")
	if !fileExists(phpExe) {
		return fmt.Errorf("php-cgi.exe 未找到: %s", phpExe)
	}

	phpIni := filepath.Join(phpDir, "php.ini")
	if !fileExists(phpIni) {
		ini := runtime.GeneratePHPIniContent(cfg, phpDir, version)
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	s.Info.Status = core.StatusStarting

	runtime.AutoAssignPorts(cfg, installDir)
	port := runtime.GetPHPVersionPort(cfg, version)
	s.Info.Port = port

	// Performance environment variables (reference: phpStudy)
	phpEnv := []string{
		"PHP_FCGI_MAX_REQUESTS=300", // 宝塔推荐值,防内存泄漏
		"PHP_FCGI_CHILDREN=1",       // Windows 下固定为 1
	}
	var pids []int

	for i := 0; i < phpWorkerCount; i++ {
		p := port + i
		args := []string{
			"-b", fmt.Sprintf("127.0.0.1:%d", p),
			"-c", phpIni,
		}
		pid, err := pm.Start("php", phpExe, args, phpEnv, phpDir)
		if err != nil {
			for _, pid2 := range pids {
				pm.Stop(pid2)
			}
			s.Info.Status = core.StatusError
			return err
		}
		pids = append(pids, pid)
	}

	for i := 0; i < 6; i++ {
		allRunning := true
		for _, pid := range pids {
			if !pm.IsRunning(pid) {
				allRunning = false
				break
			}
		}
		if allRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	for _, pid := range pids {
		if !pm.IsRunning(pid) {
			for _, p := range pids {
				pm.Stop(p)
			}
			s.Info.Status = core.StatusError
			return fmt.Errorf("PHP 进程启动后立即退出")
		}
	}

	s.process = &execWrapper{
		pid:       pids[0],
		extraPids: pids[1:],
		cmd:       phpExe,
		started:   time.Now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pids[0]

	s.stopped = make(chan struct{})
	return nil
}

func (h *PHPHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping

	pids := append([]int{s.process.pid}, s.process.extraPids...)
	for _, pid := range pids {
		pm.Stop(pid)
	}

	for i := 0; i < 6; i++ {
		allDead := true
		for _, pid := range pids {
			if pm.IsRunning(pid) {
				allDead = false
				break
			}
		}
		if allDead {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
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
		var t int
		fmt.Sscanf(v, "%d", &t)
		pc.MaxExecutionTime = t
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
		pc.DisplayErrors = v == "true"
		changed = true
	}
	if v, ok := values["opcacheEnabled"]; ok {
		pc.OPcacheEnabled = v == "true"
		changed = true
	}
	if v, ok := values["realpathCacheSize"]; ok {
		pc.RealpathCacheSize = v
		changed = true
	}

	if changed {
		settings.PHPConfig = pc
		if err := cfg.Update(settings); err != nil {
			return err
		}
	}

	// Regenerate php.ini with new settings
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)
	phpIni := filepath.Join(phpDir, "php.ini")
	ini := runtime.GeneratePHPIniContent(cfg, phpDir, s.Info.Version)
	return os.WriteFile(phpIni, []byte(ini), 0644)
}
