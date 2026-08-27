package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// kindDirOf 返回服务安装根目录（如 <install>\programs\nginx），用于识别"本工具管理的任意版本残留进程"。
func kindDirOf(instanceDir string) string {
	if instanceDir == "" {
		return ""
	}
	return filepath.Dir(instanceDir)
}

// portDebug 环境变量 DEVSTACK_PORT_DEBUG=1 时输出端口接管过程的诊断日志。
var portDebug = os.Getenv("DEVSTACK_PORT_DEBUG") == "1"

func dlogf(format string, args ...interface{}) {
	if portDebug {
		fmt.Fprintf(os.Stderr, "[port-debug] "+format+"\n", args...)
	}
}

// ensurePortFree 确保端口可被本实例使用，返回 nil 时保证端口当前未被任何进程监听。
//
//   - 端口未被占用 → 直接返回；
//   - 占用者位于 kindDir（如 <install>\programs\nginx）内 → 属于本工具管理的其它版本残留
//     实例（典型场景：环境切换版本后旧版本孤儿仍占着端口），按版本目录整组清除
//     （nginx 的 master 会重生 worker，单杀占端口的 worker 无效），与 Laragon 切换版本时
//     "自动接管旧实例"的行为一致；
//   - 占用者是外部程序 → 返回含 PID 与可执行路径的错误，绝不误杀。
func ensurePortFree(port int, kindDir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		pid := process.FindPIDByPort(port)
		if pid <= 0 {
			return nil
		}
		exe := process.GetExecutablePath(pid)
		dlogf("端口 %d 被 PID %d (%s) 占用", port, pid, exe)
		if kindDir == "" || exe == "" ||
			!strings.Contains(strings.ToLower(exe), strings.ToLower(kindDir)) {
			return fmt.Errorf("端口 %d 被外部程序占用: %s (PID %d)，请手动处理后再启动", port, exe, pid)
		}
		t0 := time.Now()
		if err := killOwnedInstance(pid); err != nil {
			return err
		}
		dlogf("整组清除耗时 %s", time.Since(t0).Round(time.Millisecond))
		if !time.Now().Before(deadline) {
			return fmt.Errorf("等待端口 %d 释放超时", port)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// killOwnedInstance 清除端口占用者所属实例的整组进程。
//
// nginx 等服务是 master/worker 结构：占端口的 worker 被 /T 杀掉后，
// master 会立即重生新 worker 抢回端口。因此必须沿父链向上找到
// 同名根进程（master），从根整树击杀。全程只做一次进程快照，
// 不逐 PID 查询路径（对受保护进程开销极大）。
func killOwnedInstance(pid int) error {
	procs, err := process.ListProcesses()
	if err != nil {
		return err
	}
	byPID := make(map[int]process.ProcInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}

	self, ok := byPID[pid]
	if !ok {
		return fmt.Errorf("进程 %d 已退出", pid)
	}

	// 沿父链上爬到最顶层同名祖先（master）
	root := pid
	seen := map[int]struct{}{pid: {}}
	for {
		parent, ok := byPID[self.PPID]
		if !ok || parent.Name != self.Name {
			break
		}
		if _, dup := seen[parent.PID]; dup {
			break
		}
		seen[parent.PID] = struct{}{}
		root, self = parent.PID, parent
	}

	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(root))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()

	set := map[int]struct{}{root: {}}
	if alive := waitProcessesExited(set, 5*time.Second); len(alive) > 0 {
		return fmt.Errorf("残留进程 (PID %d) 未能终止", root)
	}
	return nil
}

// ownedPIDByPort 返回监听在 port 上、且可执行文件路径位于 ownDir 目录内的进程 PID；
// 端口被其它程序或其它版本占用时返回 0，避免把外部进程误认/误杀为本实例。
func ownedPIDByPort(port int, ownDir string) int {	if ownDir == "" {
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
