package service

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type PythonHandler struct{}

func (h *PythonHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	return nil
}

func (h *PythonHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	pyDir := filepath.Join(installDir, "python", s.Info.Version)
	pyExe := filepath.Join(pyDir, "python.exe")

	if !fileExists(pyExe) {
		return fmt.Errorf("Python 可执行文件未找到: %s", pyExe)
	}

	port := s.Info.Port
	if port == 0 {
		port = 3001
	}

	s.Info.Status = core.StatusStarting
	pid, err := pm.Start("python", pyExe, []string{"-m", "http.server", fmt.Sprintf("%d", port), "--bind", "127.0.0.1"}, nil, pyDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{pid: pid, cmd: pyExe, started: time.Now()}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	if s.Info.Port == 0 {
		s.Info.Port = port
	}
	return nil
}

func (h *PythonHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	return nil
}

func (h *PythonHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
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


