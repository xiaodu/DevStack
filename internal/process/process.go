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
	exitChans map[int]chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		processes: make(map[int]*exec.Cmd),
		exitChans: make(map[int]chan struct{}),
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
	exitCh := make(chan struct{})
	m.mu.Lock()
	m.processes[pid] = cmd
	m.exitChans[pid] = exitCh
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		m.mu.Lock()
		delete(m.processes, pid)
		close(exitCh)
		delete(m.exitChans, pid)
		m.mu.Unlock()
	}()

	return pid, nil
}

func (m *Manager) WaitChan(pid int) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.exitChans[pid]; ok {
		return ch
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *Manager) Stop(pid int) error {
	m.mu.Lock()
	cmd, ok := m.processes[pid]
	m.mu.Unlock()
	if !ok {
		if !IsProcessRunning(pid) {
			return fmt.Errorf("进程 %d 未找到或已退出", pid)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("进程 %d 未找到: %w", pid, err)
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
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	err = syscall.GetExitCodeProcess(h, &code)
	if err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
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

func FindAllPIDsByPort() map[int]int {
	cmd := exec.Command("netstat", "-ano")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[int]int)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		addr := fields[1]
		if idx := strings.LastIndex(addr, ":"); idx > 0 {
			portStr := addr[idx+1:]
			if port, err := strconv.Atoi(portStr); err == nil {
				pid, _ := strconv.Atoi(fields[len(fields)-1])
				if pid > 0 {
					result[port] = pid
				}
			}
		}
	}
	return result
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
