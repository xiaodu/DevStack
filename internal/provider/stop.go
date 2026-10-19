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

// scanPortPids 走一次真实扫描取端口上的全部监听 PID。
// 必须先作废缓存：netstat 结果有 2s TTL，停止动作后的轮询若读到停止前的快照，
// 会把已经停成功的操作误判成"仍在监听"，产生假阴/假阳双向误差。
func scanPortPids(port int) []int {
	process.InvalidatePortCache()
	return process.FindPIDsByPort(port)
}

// ensurePortFree 确保端口可被本实例使用，返回 nil 时保证端口当前未被任何进程监听。
//
//   - 端口未被占用 → 直接返回；
//   - 占用者位于 kindDir（如 <install>\programs\nginx）内 → 属于本工具管理的其它版本残留
//     实例（环境切换版本后旧版本孤儿仍占着端口、或历史停止失败累积出的多个 master）：
//     先尝试优雅关闭，再按版本目录把端口上属于本工具的全部进程树一次性清干净。
//     与 Laragon 切换版本时"自动接管旧实例"的行为一致；
//   - 端口上只剩外部程序 → 返回含 PID 与可执行路径的错误，绝不误杀。
//
// 关键约束：同一端口上 master 与每个 worker 都会出现在 netstat 里，
// 逐个杀"随机一个 worker"永远清不干净（master 会重生 worker 抢回端口），
// 因此每一轮都必须把本工具名下的全部根进程一起击杀。
func ensurePortFree(port int, kindDir string, timeout time.Duration, graceful ...func(port int) error) error {
	deadline := time.Now().Add(timeout)
	gracefulTried := false
	for {
		pids := scanPortPids(port)
		if len(pids) == 0 {
			return nil
		}
		owned, foreign := splitPidsByDir(pids, kindDir)
		dlogf("端口 %d 监听者: 本工具 %v / 外部 %v", port, owned, foreign)
		if len(owned) == 0 {
			pid := foreign[0]
			exe := process.GetExecutablePath(pid)
			return fmt.Errorf("端口 %d 被外部程序占用: %s (PID %d)，请手动处理后再启动", port, exe, pid)
		}

		// 先走优雅关闭（仅对残留的自家实例；对 mysqld 直接强杀有 InnoDB 数据损坏风险）
		if !gracefulTried && len(graceful) > 0 && graceful[0] != nil {
			gracefulTried = true
			dlogf("尝试优雅关闭端口 %d 的残留实例", port)
			if err := graceful[0](port); err != nil {
				dlogf("优雅关闭失败: %v，回退强制清除", err)
			} else if alive := waitProcessesExited(pidSet(owned), 8*time.Second); len(alive) == 0 {
				dlogf("残留实例已优雅退出")
				continue
			}
		}

		t0 := time.Now()
		// 整组清除：本轮一次性干掉端口上属于本工具的全部根进程，
		// 避免"杀一个 worker → master 重生 → 下一轮又看到新 worker"的死循环。
		var killErr error
		for _, root := range topLevelAncestors(owned) {
			if err := killRootTree(root); err != nil {
				killErr = err
			}
		}
		if killErr != nil {
			return killErr
		}
		dlogf("整组清除耗时 %s", time.Since(t0).Round(time.Millisecond))
		if !time.Now().Before(deadline) {
			return fmt.Errorf("等待端口 %d 释放超时", port)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// splitPidsByDir 按可执行文件路径是否落在 dir 内，把监听 PID 分成自家与外部两组。
func splitPidsByDir(pids []int, dir string) (owned, foreign []int) {
	lower := strings.ToLower(dir)
	for _, pid := range pids {
		exe := strings.ToLower(process.GetExecutablePath(pid))
		if dir != "" && exe != "" && strings.Contains(exe, lower) {
			owned = append(owned, pid)
			continue
		}
		foreign = append(foreign, pid)
	}
	return owned, foreign
}

func pidSet(pids []int) map[int]struct{} {
	set := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return set
}

// pidSlice 把 PID 集合摊平成切片（用于按集合批量归并根进程）。
func pidSlice(set map[int]struct{}) []int {
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for pid := range set {
		out = append(out, pid)
	}
	return out
}

// topLevelAncestors 把一组 PID 归并成它们各自最顶层的同名祖先（master）集合。
//
// 端口上往往同时挂着多个 master 的 worker（历史停止失败累积出的孤儿实例），
// 只处理其中一个 master 的进程树会让其余 master 继续存活并占着端口。
func topLevelAncestors(pids []int) []int {
	procs, err := process.ListProcesses()
	if err != nil {
		// 拿不到进程表时退化为按自身处理，至少不会漏杀
		return pids
	}
	byPID := make(map[int]process.ProcInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}

	seen := make(map[int]struct{}, len(pids))
	roots := make([]int, 0, len(pids))
	for _, pid := range pids {
		self, ok := byPID[pid]
		if !ok {
			continue
		}
		root := pid
		hop := map[int]struct{}{pid: {}}
		// 沿父链上爬到最顶层同名祖先（master）
		for {
			parent, ok := byPID[self.PPID]
			if !ok || parent.Name != self.Name {
				break
			}
			if _, dup := hop[parent.PID]; dup {
				break
			}
			hop[parent.PID] = struct{}{}
			root, self = parent.PID, parent
		}
		if _, dup := seen[root]; !dup {
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}
	return roots
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

	return killRootTree(root)
}

// killRootTree 从根进程整树强制终止，并确认整棵子树真的退出。
func killRootTree(root int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(root))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()

	set := map[int]struct{}{root: {}}
	if alive := waitProcessesExited(set, 5*time.Second); len(alive) > 0 {
		return fmt.Errorf("残留进程 (PID %d) 未能终止", root)
	}
	return nil
}

// ownedPIDsByPort 返回监听在 port 上、且可执行文件路径位于 ownDir 目录内的全部进程 PID。
//
// 必须返回集合而不是单个 PID：master 与每个 worker 都在该端口上监听，
// 只拿一个 PID 会让"停止"永远杀不干净（master 重生 worker），
// 也会让"是否在运行"的判定随 netstat 行序漂移。
// 端口被其它程序或其它版本占用时返回空，避免把外部进程误认/误杀为本实例。
func ownedPIDsByPort(port int, ownDir string) []int {
	if ownDir == "" {
		return nil
	}
	return process.FindPIDsByPortOwned(port, ownDir)
}

// ownedPIDByPort 返回本实例在 port 上的一个代表 PID（无则 0）。
//
// 仅用于"是否在运行"的布尔判定与 UI 展示；需要停止/复核时必须用
// ownedPIDsByPort 拿到全集。
func ownedPIDByPort(port int, ownDir string) int {
	pids := ownedPIDsByPort(port, ownDir)
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
}

// collectOwnedTargets 汇总"本实例名下的全部待处理进程"：
// provider 自管 PID ∪ 端口上属于本版本安装目录的全部 PID。
//
// 两类信息都不可省略：自管 PID 可能是上个会话遗留的 master（不在 netstat 行里），
// 端口归属集合则覆盖自管记录丢失/从未记录的同版本孤儿进程（master + 全部 worker）。
// 只取其中一个会导致停止动作永远清不干净。
func collectOwnedTargets(port int, ownDir string, selfPIDs ...int) map[int]struct{} {
	pids := append([]int(nil), selfPIDs...)
	pids = append(pids, ownedPIDsByPort(port, ownDir)...)
	set := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			set[pid] = struct{}{}
		}
	}
	return set
}

// verifyStoppedByPort 复核端口上是否还残留本实例进程（最多等 timeout）。
// 返回非空切片表示"进程没了但端口上又冒出来本实例新进程"——即 master 重生 worker。
//
// 每轮都走真实扫描（scanPortPids 内已作废缓存）：刚执行过强杀，
// 读到 2s TTL 内的陈旧快照会把已停成功的动作误判成仍在监听。
func verifyStoppedByPort(port int, ownDir string, timeout time.Duration) []int {
	deadline := time.Now().Add(timeout)
	for {
		owned, _ := splitPidsByDir(scanPortPids(port), ownDir)
		if len(owned) == 0 || time.Now().After(deadline) {
			return owned
		}
		time.Sleep(200 * time.Millisecond)
	}
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
