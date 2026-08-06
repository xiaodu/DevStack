package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

func (m *Manager) GetNodeInfo() []core.RuntimeInfo {
	return m.ListByType(core.RuntimeNode)
}

func (m *Manager) StartNode(version string) error {
	return m.Start("node@" + version)
}

func (m *Manager) StopNode(version string) error {
	return m.Stop("node@" + version)
}

func (m *Manager) startNode(inst *Instance) error {
	nodeDir := inst.Info.Path
	nodeExe := filepath.Join(nodeDir, "node.exe")
	if !fileExists(nodeExe) {
		return fmt.Errorf("node.exe 未找到: %s", nodeExe)
	}

	port := m.nodePort(inst.Info.Version)
	if port <= 0 {
		return fmt.Errorf("无法为 Node 分配可用端口")
	}
	inst.Info.Port = port

	serverFile := filepath.Join(nodeDir, "devstack-server.js")
	if !fileExists(serverFile) {
		if err := os.WriteFile(serverFile, []byte(nodeServerScript(port)), 0644); err != nil {
			return err
		}
	}

	inst.Info.Status = core.StatusStarting

	pid, err := m.procMgr.Start("node", nodeExe, []string{serverFile}, nil, nodeDir)
	if err != nil {
		inst.Info.Status = core.StatusError
		return err
	}

	for i := 0; i < 12; i++ {
		if p := process.FindPIDByPort(port); p > 0 {
			pid = p
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if process.FindPIDByPort(port) == 0 && !m.procMgr.IsRunning(pid) {
		m.procMgr.Stop(pid)
		inst.Info.Status = core.StatusError
		return fmt.Errorf("Node 进程启动后立即退出")
	}

	inst.process = &process.ProcessHandle{PID: pid, Started: time.Now()}
	inst.Info.Status = core.StatusRunning
	inst.Info.Running = true
	inst.Info.PID = pid
	return nil
}

func (m *Manager) stopNode(inst *Instance) error {
	if inst.process == nil || inst.process.PID <= 0 {
		return nil
	}
	inst.Info.Status = core.StatusStopping

	pid := inst.process.PID
	m.procMgr.Stop(pid)

	for i := 0; i < 6; i++ {
		if !m.procMgr.IsRunning(pid) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	inst.process = nil
	inst.Info.Status = core.StatusStopped
	inst.Info.Running = false
	inst.Info.PID = 0
	return nil
}

func nodeServerScript(port int) string {
	return fmt.Sprintf(`const http = require('http');
const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
  res.end('<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>DevStack Node.js</title></head><body><h1>DevStack Node.js runtime</h1><p>Node.js 运行环境已启动。</p></body></html>');
});
server.listen(%d, '127.0.0.1', () => {
  console.log('DevStack Node.js runtime listening on 127.0.0.1:%d');
});
`, port, port)
}

func (m *Manager) ListByType(rt core.RuntimeType) []core.RuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []core.RuntimeInfo
	for _, id := range m.idByType[rt] {
		if inst, ok := m.instances[id]; ok {
			result = append(result, inst.Info)
		}
	}
	return result
}
