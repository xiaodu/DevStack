package service

import (
	"fmt"
	"path/filepath"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

var handlers = map[string]ServiceHandler{
	"nginx": &NginxHandler{},
	"mysql": &MySQLHandler{},
	"redis": &RedisHandler{},
	"php":   &PHPHandler{},
}

type ServiceHandler interface {
	Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error
	Stop(s *ServiceInstance, pm *process.Manager) error
}

type NginxHandler struct{}

func (h *NginxHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	nginxDir := filepath.Join(installDir, "nginx", s.Info.Version)
	nginxExe := filepath.Join(nginxDir, "nginx.exe")

	if !fileExists(nginxExe) {
		return fmt.Errorf("Nginx 可执行文件未找到: %s", nginxExe)
	}

	s.Info.Status = core.StatusStarting

	pid, err := pm.Start("nginx", nginxExe, nil, nil, nginxDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{
		pid:     pid,
		cmd:     nginxExe,
		started: now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	return nil
}

func (h *NginxHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
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
