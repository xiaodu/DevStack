package provider

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// ownedPIDByPort 返回监听在 port 上、且可执行文件路径位于 ownDir 目录内的进程 PID；
// 端口被其它程序或其它版本占用时返回 0，避免把外部进程误认/误杀为本实例。
func ownedPIDByPort(port int, ownDir string) int {
	if ownDir == "" {
		return 0
	}
	pid := process.FindPIDByPort(port)
	if pid <= 0 {
		return 0
	}
	exe := strings.ToLower(process.GetExecutablePath(pid))
	if exe == "" || !strings.Contains(exe, strings.ToLower(ownDir)) {
		return 0
	}
	return pid
}

// stopOwnedProcessByPort 停止监听在 port 上、且可执行文件路径位于 ownDir 目录内的进程。
// 端口被其它程序或其它版本占用时不误杀，返回 0。
func stopOwnedProcessByPort(port int, ownDir string) int {
	pid := ownedPIDByPort(port, ownDir)
	if pid <= 0 {
		return 0
	}
	process.NewManager().Stop(pid)
	return pid
}

// waitProcessesExited 等待 pids 全部退出，全部退出返回 nil，超时返回仍未退出的 PID 集合。
func waitProcessesExited(pids map[int]struct{}, timeout time.Duration) map[int]struct{} {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if alive := alivePIDs(pids); len(alive) == 0 {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return alivePIDs(pids)
}

func alivePIDs(pids map[int]struct{}) map[int]struct{} {
	alive := make(map[int]struct{})
	for pid := range pids {
		if process.IsProcessRunning(pid) {
			alive[pid] = struct{}{}
		}
	}
	if len(alive) == 0 {
		return nil
	}
	return alive
}

func pidList(pids map[int]struct{}) string {
	parts := make([]string, 0, len(pids))
	for pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
