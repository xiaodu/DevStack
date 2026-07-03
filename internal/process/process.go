package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ProcessInfo struct {
	PID     int
	Name    string
	Running bool
	Port    int
	Uptime  time.Duration
	Started time.Time
}

type Manager struct {
	mu        sync.Mutex
	processes map[int]*exec.Cmd
}

func NewManager() *Manager {
	return &Manager{
		processes: make(map[int]*exec.Cmd),
	}
}

func (m *Manager) Start(name, executable string, args []string, env []string, workdir string) (int, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workdir
	cmd.Stdout = nil
	cmd.Stderr = nil
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动 %s 失败: %w", name, err)
	}

	pid := cmd.Process.Pid
	m.mu.Lock()
	m.processes[pid] = cmd
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		m.mu.Lock()
		delete(m.processes, pid)
		m.mu.Unlock()
	}()

	return pid, nil
}

func (m *Manager) Stop(pid int) error {
	m.mu.Lock()
	cmd, ok := m.processes[pid]
	m.mu.Unlock()
	if !ok {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("进程 %d 未找到", pid)
		}
		return proc.Kill()
	}
	return cmd.Process.Kill()
}

func (m *Manager) IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	m.mu.Lock()
	_, ok := m.processes[pid]
	m.mu.Unlock()
	if ok {
		return true
	}
	return IsProcessRunning(pid)
}

func IsProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func FindPIDByPort(port int) int {
	cmd := exec.Command("netstat", "-ano")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	portStr := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, portStr) && strings.Contains(line, "LISTEN") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				pid, _ := strconv.Atoi(fields[len(fields)-1])
				return pid
			}
		}
	}
	return 0
}

func FindExecutable(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exts := []string{".exe", ".bat", ".cmd", ""}
	paths := strings.Split(os.Getenv("PATH"), ";")
	for _, path := range paths {
		for _, ext := range exts {
			full := filepath.Join(path, name+ext)
			if _, err := os.Stat(full); err == nil {
				return full
			}
		}
	}
	return ""
}
