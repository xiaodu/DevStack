package provider

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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

// runStopCommand 执行服务自带的优雅停止命令（如 nginx -s stop / mysqladmin shutdown / redis-cli shutdown）。
// 工作目录设为 workdir（为空则不设置），隐藏窗口，捕获 stderr/stdout 合并输出。
// 失败时返回含输出的错误，便于上层决定是否走兜底强杀。调用方通常忽略此错误，
// 因为优雅停止失败属正常情况（PID 文件丢失、端口被占、密码不符等），由后续 taskkill /F /T 兜底。
func runStopCommand(exe string, args []string, workdir string) error {
	if exe == "" {
		return fmt.Errorf("可执行文件路径为空")
	}
	cmd := exec.Command(exe, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s 优雅停止失败: %w, 输出: %s", filepath.Base(exe), err, strings.TrimSpace(string(out)))
	}
	return nil
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
