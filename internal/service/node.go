package service

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type NodeHandler struct{}

func (h *NodeHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	nodeDir := filepath.Join(installDir, "node", s.Info.Version)
	nodeExe := filepath.Join(nodeDir, "node.exe")

	if !fileExists(nodeExe) {
		return fmt.Errorf("Node.js 可执行文件未找到: %s", nodeExe)
	}

	s.Info.Status = core.StatusStarting
	pid, err := pm.Start("node", nodeExe, []string{"--version"}, nil, nodeDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	// Node.js runs as a version manager - we just verify it works
	s.process = &execWrapper{pid: pid, cmd: nodeExe, started: time.Now()}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	return nil
}

func (h *NodeHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
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

func init() {
	handlers["node"] = &NodeHandler{}
}
