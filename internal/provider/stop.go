package provider

import (
	"strings"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// stopProcessByPort 停止监听在指定端口上的进程。
// 仅当进程可执行文件名包含 expect（小写）时才执行，避免误杀占用端口的其它程序。
// 未找到匹配进程时返回 0，否则返回被终止的 PID。
func stopProcessByPort(port int, expect string) int {
	pid := process.FindPIDByPort(port)
	if pid <= 0 {
		return 0
	}
	if expect != "" {
		exe := strings.ToLower(process.GetExecutablePath(pid))
		if !strings.Contains(exe, strings.ToLower(expect)) {
			return 0
		}
	}
	process.NewManager().Stop(pid)
	return pid
}

// waitPortReleased 等待端口不再被监听，超时未释放返回 false。
func waitPortReleased(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if process.FindPIDByPort(port) == 0 {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return process.FindPIDByPort(port) == 0
}
