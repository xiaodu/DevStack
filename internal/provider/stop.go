package provider

import (
	"context"
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

// kindDirOf 返回服务安装根目录（如 <install>\programs\nginx），即版本目录的父目录。
func kindDirOf(instanceDir string) string {
	if instanceDir == "" {
		return ""
	}
	return filepath.Dir(instanceDir)
}

// siblingVersionOf 判断某个可执行文件是否属于同 kind 的**其它版本目录**，是则返回该版本目录名。
// 用于把"端口被自己另一个版本占着"和"端口被外部程序占着"分清楚，前者该提示先停那个版本。
func siblingVersionOf(exe, instanceDir string) string {
	if exe == "" || instanceDir == "" {
		return ""
	}
	kindDir := kindDirOf(instanceDir)
	if kindDir == "" || !pathWithinDir(exe, kindDir) {
		return ""
	}
	rel, err := filepath.Rel(kindDir, exe)
	if err != nil {
		return ""
	}
	version := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	if version == "" || version == "." {
		return ""
	}
	// 与本实例同目录的不算兄弟版本
	if pathWithinDir(exe, instanceDir) {
		return ""
	}
	return version
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

// ensurePortFree 确保端口可被本实例（instanceDir = <install>\programs\<kind>\<version>）使用，
// 返回 nil 时保证端口当前未被任何进程监听。
//
//   - 端口未被占用 → 直接返回；
//   - 占用者的可执行文件落在**本版本目录**内 → 是本实例自己的残留进程
//     （上次停止失败留下的孤儿 master/worker）：先尝试优雅关闭，再一次性清掉全部根进程。
//     关键约束：同一端口上 master 与每个 worker 都会出现在 netstat 里，
//     逐个杀"随机一个 worker"永远清不干净（master 会重生 worker 抢回端口），
//     因此每一轮都必须把本目录名下的全部根进程一起击杀。
//   - 占用者属于同 kind 的**其它版本目录**（兄弟版本）或外部程序 → 一律不杀，返回可执行的错误。
//     旧实现按 kind 目录（所有版本的父目录）判归属，于是"启动 nginx 1.26"会静默强杀掉
//     正在运行的 1.24，而界面上 1.24 仍显示运行中（要等 5s 周期同步才改判）——
//     跨版本接管改由上层 service.Manager.Start 显式先停兄弟版本并广播状态后再启动。
//
// ctx 贯穿全流程：旧实现忽略调用方的取消信号（应用退出时只能死等），
// 且优雅关闭后的等待写死 8s，对需要刷脏页的 mysqld 不够（会被当成失败后强杀）。
// 现在等待预算由调用方的 timeout 派生，MySQL 侧传 GraceMySQL+缓冲即可等完正常关闭。
func ensurePortFree(ctx context.Context, port int, instanceDir string, timeout time.Duration, graceful ...func(ctx context.Context, port int) (stopOutcome, error)) error {
	deadline := time.Now().Add(timeout)
	gracefulTried := false
	for {
		pids := scanPortPids(port)
		if len(pids) == 0 {
			return nil
		}
		owned, foreign := splitPidsByDir(pids, instanceDir)
		dlogf("端口 %d 监听者: 本实例 %v / 非本实例 %v", port, owned, foreign)
		if len(owned) == 0 {
			pid := foreign[0]
			exe := process.GetExecutablePath(pid)
			if sib := siblingVersionOf(exe, instanceDir); sib != "" {
				return fmt.Errorf("端口 %d 被本工具已安装的其它版本 %s 占用（%s），请先停止该版本再启动", port, sib, exe)
			}
			return fmt.Errorf("端口 %d 被外部程序占用: %s (PID %d)，请手动处理后再启动", port, exe, pid)
		}

		// 先走优雅关闭（仅对残留的自家实例；对 mysqld 直接强杀有 InnoDB 数据损坏风险）
		if !gracefulTried && len(graceful) > 0 && graceful[0] != nil {
			gracefulTried = true
			dlogf("尝试优雅关闭端口 %d 的残留实例", port)
			outcome, err := graceful[0](ctx, port)
			if err != nil {
				dlogf("优雅关闭未成功(%s): %v", outcome, err)
			}
			// 认证/可执行缺失类「永久失败」白等没有意义，直接降级强杀；
			// 其余情形（含命令自身超时）必须给足退出预算 —— mysqladmin 超时
			// 往往意味着 mysqld 还在 flush，而不是没受理。
			if outcome != stopPermanent {
				budget := time.Until(deadline)
				if budget < 2*time.Second {
					budget = 2 * time.Second
				}
				if alive := waitExit(ctx, pidSet(owned), budget); len(alive) == 0 {
					dlogf("残留实例已优雅退出")
					continue
				}
				// 优雅关闭比预期慢不等于失败：把 deadline 往后推一个预算，
				// 避免因调用方传的短 timeout 而在正常关闭中途误判超时。
				if !time.Now().Before(deadline) {
					deadline = time.Now().Add(timeout)
				}
			}
		}

		t0 := time.Now()
		// 整组清除：本轮一次性干掉端口上属于本工具的全部根进程，
		// 避免"杀一个 worker → master 重生 → 下一轮又看到新 worker"的死循环。
		var killErr error
		for _, root := range topLevelAncestors(owned) {
			if err := killRootTree(ctx, root); err != nil {
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待端口 %d 释放被取消: %w", port, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pathWithinDir 判断路径是否落在某个目录之内（大小写不敏感、按路径分隔符取边界）。
// 旧实现用 strings.Contains，任何包含该片段的路径都会被误判为"自家"，
// 进而被当成自己的进程强杀掉。
func pathWithinDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	cleanDir := filepath.Clean(dir)
	sep := string(filepath.Separator)
	if !strings.HasSuffix(cleanDir, sep) {
		cleanDir += sep
	}
	return len(path) >= len(cleanDir) &&
		strings.EqualFold(path[:len(cleanDir)], cleanDir)
}

// splitPidsByDir 按可执行文件路径是否落在 dir 内，把监听 PID 分成自家与外部两组。
func splitPidsByDir(pids []int, dir string) (owned, foreign []int) {
	for _, pid := range pids {
		exe := process.GetExecutablePath(pid)
		if pathWithinDir(exe, dir) {
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

// killRootTree 从根进程整树强制终止，并确认整棵子树真的退出。
//
// nginx 等服务是 master/worker 结构：占端口的 worker 被杀后 master 会重生
// 新 worker 抢回端口，因此调用方需先用 topLevelAncestors 沿父链找到同名根
// 进程（master），再从根整树击杀。
func killRootTree(ctx context.Context, root int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(root))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()

	set := map[int]struct{}{root: {}}
	if alive := waitExit(ctx, set, 5*time.Second); len(alive) > 0 {
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
func verifyStoppedByPort(ctx context.Context, port int, ownDir string, timeout time.Duration) []int {
	deadline := time.Now().Add(timeout)
	for {
		owned, _ := splitPidsByDir(scanPortPids(port), ownDir)
		if len(owned) == 0 || time.Now().After(deadline) {
			return owned
		}
		if err := ctx.Err(); err != nil {
			return owned
		}
		select {
		case <-ctx.Done():
			return owned
		case <-time.After(stopPollInterval):
		}
	}
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

// runStopCommand 与 waitProcessesExited 已下线：
// 前者无 deadline 也无法分类失败原因（mysqladmin 阻塞即 Stop 永久挂起），
// 后者忽略 ctx 且轮询粒度偏粗。替代实现见 stop_ladder.go 的
// runStopCommandCtx / waitExit / runStopLadder。

func pidList(pids map[int]struct{}) string {
	parts := make([]string, 0, len(pids))
	for pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
