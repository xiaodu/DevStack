package service

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type NodeHandler struct {
	BaseHandler
}

func (h *NodeHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	return nil
}

func (h *NodeHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	nodeDir := filepath.Join(installDir, "node", s.Info.Version)
	nodeExe := filepath.Join(nodeDir, "node.exe")

	if !fileExists(nodeExe) {
		return fmt.Errorf("Node.js 可执行文件未找到: %s", nodeExe)
	}

	port := s.Info.Port
	if port == 0 {
		port = 3000
	}

	script := fmt.Sprintf(
		`const h=require('http');h.createServer((q,r)=>{r.writeHead(200,{'Content-Type':'text/plain'});r.end('DevStack Node.js %s')}).listen(%d)`,
		s.Info.Version, port)

	s.Info.Status = core.StatusStarting
	pid, err := pm.Start("node", nodeExe, []string{"-e", script}, nil, nodeDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{pid: pid, cmd: nodeExe, started: time.Now()}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	if s.Info.Port == 0 {
		s.Info.Port = port
	}
	return nil
}

func (h *NodeHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	return nil
}


