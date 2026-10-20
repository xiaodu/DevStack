package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// ────────────────────────────────────────────────────────────────────────────
// 分级停止阶梯（stop ladder）
//
// 设计依据取自以下成熟实现，不是臆造：
//
//   - docker-library/mysql `docker-entrypoint.sh`：
//     "# Stop the server. When using a local socket file mysqladmin will block
//      until the shutdown is complete."
//     ⇒ 优雅停止命令本身是**同步阻塞**调用。若不给它自己的超时，mysqld 卡在
//       flush 阶段时 mysqladmin 永不返回，Stop 就永久挂起（旧实现的 D1 缺陷：
//       runStopCommand 用无 deadline 的 CombinedOutput）。
//     同一份脚本用 `--defaults-extra-file=<( _mysql_passfile )` 传凭据，
//     而不是命令行明文 `-p`，失败时直接报错而非静默忽略。
//
//   - vitessio/vitess `go/vt/mysqlctl/mysqld.go`：
//     `MysqldShutdownGracePeriod = 30 * time.Second`，注释原话是 bounds the
//     pid/socket file wait "**after mysqladmin has given up waiting**"。
//     ⇒ **命令超时 ≠ 停止失败**：命令放弃后必须再给一段独立预算观察进程退出，
//       否则会把"正在正常关闭"误判成失败并强杀（InnoDB 损坏风险）。
//     同一文件用 `mysqldExitPollInterval = 100ms` 轮询退出（旧实现 300ms）。
//
//   - systemd 的 mysql/Percona unit 不覆盖 TimeoutStopSec，落到默认 90s；
//     Percona 论坛反复出现"给足 MySQL 关闭时间、加大 stop timeout"的处置。
//     ⇒ MySQL 的优雅预算必须是"几十秒"量级，旧实现的 10s 会系统性误杀。
//
//   - winsw/winsw：`<stopexecutable>`+`<stoparguments>`+`<stoptimeout>` 超时才
//     force kill，`<stopparentprocessfirst>` 控制父/子击杀顺序。
//     supervisord：stopsignal → 等 stopwaitsecs → SIGKILL，并用 laststopreport
//     节流"waiting for x to stop"日志。
//     ⇒ 停止必须是**显式分级策略**（应用层命令 → 进程级关闭 → 整树强杀），
//       每级有独立预算，而不是"一条命令 + 一把 taskkill"。
// ────────────────────────────────────────────────────────────────────────────

// stopOutcome 优雅停止命令的结果分类。
//
// 分类的意义在于「要不要继续等」：旧实现用 `_ = runStopCommand(...)` 吞掉一切，
// 于是"认证失败（再等 45s 也不会成功）"与"命令已受理、正在 flush（必须多等）"
// 被混为一谈，只能靠固定短超时兜底 —— 既白等又误杀。
type stopOutcome int

const (
	// stopAccepted 命令已被受理，或"连不上"这类目标本就不在运行的情形：
	// 交给本级 Wait 预算观察进程退出即可。
	stopAccepted stopOutcome = iota
	// stopTransient 命令自身超时/连接超时：可能已经受理（mysqld 正在 flush 时
	// mysqladmin 就是不返回），因此**仍要给足等待预算**，不能立刻强杀。
	stopTransient
	// stopPermanent 认证被拒/权限不足/可执行缺失：等多久都不会成功，
	// 立即降级到下一级，并把原因如实带回错误信息。
	stopPermanent
	// stopRefused 组件**为了护数据主动拒绝关闭**并继续运行。
	// 目前仅 Redis 会走到这里：RDB/AOF 写入失败时它回复错误但**不退**
	// （官方文档原话 "the server continues to run"）。
	// 这类结果必须与 stopPermanent 区分：后者“换个手段就能停”，
	// 前者“强行停掉就会丢数据”，必须中止阶梯并让用户处置。
	stopRefused
)

func (o stopOutcome) String() string {
	switch o {
	case stopAccepted:
		return "已受理"
	case stopTransient:
		return "超时待定"
	case stopPermanent:
		return "永久失败"
	case stopRefused:
		return "拒绝关闭（保护数据）"
	}
	return "未知"
}

const (
	// GraceMySQL MySQL 优雅关闭等待预算。
	// 依据：systemd 默认 TimeoutStopSec=90s；innodb_fast_shutdown=0 需全量刷脏页，
	// 叠加 innodb_buffer_pool_dump_at_shutdown，大 buffer pool 可达分钟级。
	// 取 45s 是"桌面工具可接受的等待"与"绝不在正常关闭中途强杀"的折中。
	GraceMySQL = 45 * time.Second

	// GraceRedis Redis 优雅关闭等待预算（SHUTDOWN 可能先做整库 RDB save）。
	GraceRedis = 30 * time.Second

	// GraceNginx Nginx 优雅关闭等待预算（-s quit 等 worker 处理完当前请求）。
	GraceNginx = 5 * time.Second

	// graceAfterClose 进程级关闭请求之后的等待预算。
	graceAfterClose = 3 * time.Second

	// graceAfterKill 整树强杀之后的确认等待预算。
	graceAfterKill = 3 * time.Second
)

// 下面几个量做成 var 只为可测试：锚点测试需要验证"命令挂住不会把 Stop 带死"
// 与"轮询尊重 ctx"，若写死 10s/100ms，单测就要真跑几十秒。
// 生产默认值不变。
var (
	// stopCmdTimeout 优雅停止命令自身的最长执行时间（不含其后的等待预算）。
	stopCmdTimeout = 10 * time.Second

	// stopPollInterval 进程退出轮询粒度，取自 vitess mysqldExitPollInterval。
	stopPollInterval = 100 * time.Millisecond

	// stopProgressEvery 等待期间每多久记录一次进度（借鉴 supervisord
	// laststopreport 的节流思路），避免"看起来像卡死"。
	stopProgressEvery = 2 * time.Second
)

// classifyStopOutput 把优雅停止命令的失败原因分类。
//
// 关键字取自各组件的真实报错文本：
//
//	mysqladmin: "Access denied for user 'root'@'127.0.0.1" (ER_ACCESS_DENIED_ERROR 1045)
//	            "Can't connect to MySQL server on ..."（已关/未监听）
//	nginx     : "invalid PID number ... in \"logs/nginx.pid\""、
//	            "CreateFile() \"...nginx.pid\" failed (2: The system cannot find the file specified)"
//	redis-cli : "Could not connect to Redis"（多半已退出）
func classifyStopOutput(text string) stopOutcome {
	low := strings.ToLower(text)
	permanent := []string{
		"access denied",   // 用户在 MySQL 里改过 root 密码
		"using password",  // 认证失败伴随提示
		"denied for user", // SHUTDOWN / SYSTEM_VARIABLES_ADMIN 权限不足
		"command denied",  // ERROR 1143
		"unknown mysql server host",
		"no such file or directory",   // 可执行/配置文件缺失（本实例目录被删）
		"system cannot find the file", // nginx 找不到 PID 文件
		"invalid pid number",          // nginx -s 找不到 master
		// 下面两条是 nginx 在 Windows 上的真实报错（信号通道找不到 master）：
		//   nginx: [emerg] CreateFile() ".../logs/nginx.pid" failed (2: The system cannot find...)
		//   nginx: [error] OpenEvent("Global\ngx_quit_1234") failed (5: Access is denied)
		// 两者都意味着"pid 文件里的 master 早就不在了"，归为 permanent 后立刻
		// 降级到重扫端口 + 整树击杀，语义正确。
		"createfile()",
		"openevent(",
	}
	for _, k := range permanent {
		if strings.Contains(low, k) {
			// nginx "invalid PID number" 往往意味着 master 早就不在了，
			// 归为 permanent 后阶梯立即进入下一级清理（重扫端口 + 整树击杀），语义正确。
			return stopPermanent
		}
	}
	gone := []string{
		"can't connect to mysql server",
		"could not connect to redis",
		"connection refused",
		"unknown socket",
	}
	for _, k := range gone {
		if strings.Contains(low, k) {
			// "连不上"绝大多数情形是进程已经退出/没在监听，不算失败。
			return stopAccepted
		}
	}
	// 组件明确回复"我拒绝关闭，且还在运行"：绝不能当成普通失败去强杀。
	refused := []string{
		"shutdown failed",               // Redis 7: "-ERR Shutdown failed: Error saving the RDB file: ..."
		"error occurred trying to save", // 旧版: "An error occurred trying to save (RDB) to disk"
		"can't save",                    // "Can't save in background: fork: Cannot allocate memory"
		"cannot save",
		"readonly replica", // 只读副本上的 SHUTDOWN 保存会被拒
	}
	for _, k := range refused {
		if strings.Contains(low, k) {
			return stopRefused
		}
	}
	// 未知失败按 transient 处理：仍给等待预算，最后一级会强杀，不会漏停。
	return stopTransient
}

// runStopCommandCtx 执行优雅停止命令，带三重硬保障：
//
//	① context 超时（stopCmdTimeout 与父 ctx 取较早者）—— 解决"命令阻塞导致 Stop 永久挂起"；
//	② cmd.WaitDelay —— 子进程赖着不走（忽略 kill）也能强制回收，避免僵尸拖住整个阶梯；
//	③ 返回值分类，调用方据此决定"继续等"还是"立刻降级"。
func runStopCommandCtx(ctx context.Context, exe string, args []string, workdir string) (stopOutcome, error) {
	return runStopCommandEnv(ctx, exe, args, workdir, nil)
}

// runStopCommandEnv 同 runStopCommandCtx，但可追加环境变量。
//
// 用于不能把密码放上命令行的组件：redis-cli 官方推荐用 REDISCLI_AUTH
// 传凭据（而非 -a），否则密码会出现在进程列表里。
func runStopCommandEnv(ctx context.Context, exe string, args []string, workdir string, env []string) (stopOutcome, error) {
	if exe == "" {
		return stopPermanent, fmt.Errorf("可执行文件路径为空")
	}
	cctx, cancel := context.WithTimeout(ctx, stopCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	if workdir != "" {
		cmd.Dir = workdir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		// 命令自身超时：按 docker-library 的注释，这通常意味着服务端还在忙
		// （flush / 等连接退出），因此必须继续等进程退出，而不是判失败。
		return stopTransient, fmt.Errorf("%s 超过 %s 未返回: %s", baseName(exe), stopCmdTimeout, firstLine(text))
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return stopTransient, fmt.Errorf("%s 被取消: %w", baseName(exe), ErrStopCancelled)
	}
	if err == nil {
		return stopAccepted, nil
	}
	outcome := classifyStopOutput(text)
	return outcome, fmt.Errorf("%s 优雅停止失败(%s): %w, 输出: %s", baseName(exe), outcome, err, firstLine(text))
}

func baseName(exe string) string {
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		return exe[i+1:]
	}
	return exe
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// waitExit 等待 pids 全部退出；ctx 取消或超时返回仍未退出的集合。
//
// 与旧 waitProcessesExited 的差别：
//   - 尊重 ctx（旧实现完全忽略 ctx，退出应用/取消操作时无法中断，D4）；
//   - 100ms 轮询（旧 300ms，取 vitess 粒度）；
//   - 每 2s 记录一次进度，避免"看起来卡死"。
func waitExit(ctx context.Context, pids map[int]struct{}, timeout time.Duration) map[int]struct{} {
	if len(pids) == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	nextReport := time.Now().Add(stopProgressEvery)
	for {
		alive := alivePIDs(pids)
		if len(alive) == 0 {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return alive
		}
		if now := time.Now(); !now.Before(nextReport) {
			dlogf("等待 %d 个进程退出：预算 %s，剩余 PID %s", len(alive), timeout, pidList(alive))
			nextReport = now.Add(stopProgressEvery)
		}
		step := stopPollInterval
		if remaining < step {
			step = remaining
		}
		select {
		case <-ctx.Done():
			return alive
		case <-time.After(step):
		}
	}
}

// stopRung 阶梯上的一级。
type stopRung struct {
	// Name 用于错误信息与日志，必须能让用户看懂是哪一步失败。
	Name string
	// Run 发起本级的停止动作，pending 是本级需要处理的 PID 集合；
	// 返回 outcome 决定是否等满 Wait。
	Run func(ctx context.Context, pending map[int]struct{}) (stopOutcome, error)
	// Wait 本级动作之后的退出等待预算。
	Wait time.Duration
	// SkipWaitWhenPermanent 为 true 时，Run 返回 stopPermanent 将跳过本级等待，
	// 直接进入下一级（认证失败不必白等 45s）。
	SkipWaitWhenPermanent bool
	// HaltOnRefused 为 true 时，Run 返回 stopRefused 会**终止整条阶梯**、
	// 不再升级到强杀。用于"组件主动拒绝关闭以保护数据"的语义：
	// Redis 在 RDB/AOF 写入失败时会报错并**继续运行**（官方文档原话：
	// "the server continues to run"），此时强杀等于把用户数据打死，
	// 必须把失败如实上报、让人决定修磁盘还是改用 NOSAVE。
	// 注意它不管 stopPermanent（认证被拒）—— 那种情形数据无风险，应当照常降级强杀。
	HaltOnRefused bool
}

// stopLadderResult 阶梯执行结果。
type stopLadderResult struct {
	// Remaining 走完整条阶梯仍未退出的 PID（nil 表示停止成功）。
	Remaining map[int]struct{}
	// Trace 每级的可读记录，出错时整体并入错误信息，保证用户看到"卡在哪一级"。
	Trace []string
	// FirstOutcome 第一级（应用层优雅命令）的结果分类，供调用方区分
	// "优雅失败但已强杀成功"（可静默）与"优雅被拒且必须告知数据风险"。
	FirstOutcome stopOutcome
	// FirstErr 第一级的原始错误（可能为 nil）。
	FirstErr error
	// Halted 为 true 表示阶梯被中间一级主动终止（见 stopRung.HaltOnRefused），
	// 此时 Remaining 非空**不代表强杀失败**，而是"为保护数据故意没杀"，
	// 调用方必须据此给出与"停不掉"完全不同的处置建议。
	Halted bool
}

// runStopLadder 按顺序执行各级停止动作，每级"发动作 → 等退出"。
//
// 语义要点：
//   - 任一时刻进程全部退出即成功返回，不再继续升级（不会"已停还要再杀一遍"）；
//   - stopPermanent 且 SkipWaitWhenPermanent 时跳过等待，立刻降级；
//   - ctx 取消时立即返回，Remaining 为当时仍存活的 PID（调用方须如实上报，
//     不能把"被取消"写成"已停止"）；
//   - 每级开始前用 rescan 重扫端口归属集合，把 master 重生出来的 worker 并入
//     待处理集合 —— 沿用 nginx 已有的 2.1 教训：目标进程全退出 ≠ 实例已停。
func runStopLadder(
	ctx context.Context,
	targets map[int]struct{},
	rungs []stopRung,
	rescan func(map[int]struct{}) map[int]struct{},
) stopLadderResult {
	res := stopLadderResult{Remaining: targets, FirstOutcome: stopAccepted}
	// 目标本就不存在（实例未运行）：视为已停止，不发任何命令。
	if len(targets) == 0 {
		res.Remaining = nil
		return res
	}

	for idx, rung := range rungs {
		if err := ctx.Err(); err != nil {
			res.Trace = append(res.Trace, fmt.Sprintf("%s: 已取消(%v)", rung.Name, err))
			break
		}
		pending := alivePIDs(res.Remaining)
		if rescan != nil {
			pending = rescan(pending)
		}
		if len(pending) == 0 {
			res.Remaining = nil
			return res
		}
		res.Remaining = pending

		var (
			outcome = stopAccepted
			err     error
		)
		if rung.Run != nil {
			outcome, err = rung.Run(ctx, pending)
		}
		if idx == 0 {
			res.FirstOutcome, res.FirstErr = outcome, err
		}
		if err != nil {
			res.Trace = append(res.Trace, fmt.Sprintf("%s: %v", rung.Name, err))
		}
		if rung.HaltOnRefused && outcome == stopRefused {
			// 服务还活着但明确拒绝关闭：不等待、也不升级，立即中止并上报。
			res.Halted = true
			res.Trace = append(res.Trace, fmt.Sprintf("%s: %s，为保护数据不再升级强杀", rung.Name, outcome))
			return res
		}
		if rung.SkipWaitWhenPermanent && outcome == stopPermanent {
			res.Trace = append(res.Trace, fmt.Sprintf("%s: %s，跳过 %s 等待直接降级", rung.Name, outcome, rung.Wait))
			continue
		}

		start := time.Now()
		alive := waitExit(ctx, res.Remaining, rung.Wait)
		if len(alive) == 0 {
			res.Remaining = nil
			res.Trace = append(res.Trace, fmt.Sprintf("%s: 进程已退出（耗时 %s）",
				rung.Name, time.Since(start).Round(time.Millisecond)))
			return res
		}
		res.Remaining = alive
		res.Trace = append(res.Trace, fmt.Sprintf("%s: 等 %s 后仍有 %s 存活（outcome=%s）",
			rung.Name, rung.Wait, pidList(alive), outcome))
	}
	return res
}

// commandRung 用组件自带 CLI 做应用层优雅停止的第一级。
func commandRung(name string, wait time.Duration, run func(ctx context.Context) (stopOutcome, error)) stopRung {
	return stopRung{
		Name:                  name,
		Wait:                  wait,
		SkipWaitWhenPermanent: true,
		Run: func(ctx context.Context, _ map[int]struct{}) (stopOutcome, error) {
			return run(ctx)
		},
	}
}

// commandHaltRung 与 commandRung 同，但本级收到 stopRefused 时终止整条阶梯（不强杀）。
func commandHaltRung(name string, wait time.Duration, run func(ctx context.Context) (stopOutcome, error)) stopRung {
	r := commandRung(name, wait, run)
	r.HaltOnRefused = true
	return r
}

// closeRung 进程级关闭请求（taskkill 不带 /F，对有窗口进程等价 WM_CLOSE）。
// 控制台/服务进程通常收不到并立即报错，此时归类 permanent 让阶梯马上降级，不白等。
func closeRung(wait time.Duration) stopRung {
	return stopRung{
		Name:                  "进程级关闭",
		Wait:                  wait,
		SkipWaitWhenPermanent: true,
		Run: func(_ context.Context, pending map[int]struct{}) (stopOutcome, error) {
			var lastErr error
			for pid := range pending {
				if err := process.RequestClose(pid); err != nil {
					lastErr = err
				}
			}
			if lastErr != nil {
				return stopPermanent, lastErr
			}
			return stopAccepted, nil
		},
	}
}

// forceKillRung 阶梯最后一级：沿父链归并根进程 + 整树强杀。
// 沿用 topLevelAncestors 的约束：逐个杀 worker 会被 master 重生，必须整组同杀。
func forceKillRung(wait time.Duration) stopRung {
	return stopRung{
		Name: "整树强制终止",
		Wait: wait,
		Run: func(ctx context.Context, pending map[int]struct{}) (stopOutcome, error) {
			var lastErr error
			for _, root := range topLevelAncestors(pidSlice(pending)) {
				if err := killRootTree(ctx, root); err != nil {
					lastErr = err
				}
			}
			if lastErr != nil {
				return stopTransient, lastErr
			}
			return stopAccepted, nil
		},
	}
}

// rescanOwned 把端口上新出现的本目录进程并入待处理集合。
// nginx/mysqld 这类结构下"目标进程全退出"不等于"实例已停"（master 会重生 worker），
// 因此每级都要以端口归属集合为准重扫一次。
func rescanOwned(port int, ownDir string) func(map[int]struct{}) map[int]struct{} {
	return func(known map[int]struct{}) map[int]struct{} {
		if port <= 0 || ownDir == "" {
			return known
		}
		merged := make(map[int]struct{}, len(known))
		for pid := range known {
			merged[pid] = struct{}{}
		}
		for _, pid := range ownedPIDsByPort(port, ownDir) {
			if processAlive(pid) {
				merged[pid] = struct{}{}
			}
		}
		return merged
	}
}

// formatLadderFailure 把阶梯轨迹压成一条可执行的错误信息。
// 用户必须能看出：卡在哪一级、等了多久、是否已尝试强杀、以及原因。
func formatLadderFailure(kind string, port int, res stopLadderResult, cause string) error {
	reason := ""
	if res.FirstErr != nil {
		reason = "；原因：" + res.FirstErr.Error()
	}
	return fmt.Errorf("%s 停止失败: 端口 %d 上进程 %s 未退出（%s%s）。已尝试: %s",
		kind, port, pidList(res.Remaining), cause, reason, strings.Join(res.Trace, " → "))
}

// ErrStopCancelled 上层用它区分"用户/退出流程取消"与"真的停不掉"。
var ErrStopCancelled = errors.New("停止被取消")

func processAlive(pid int) bool { return process.IsProcessRunning(pid) }
