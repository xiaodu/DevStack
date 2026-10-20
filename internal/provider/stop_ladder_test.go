package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// 分级停止阶梯的锚点测试。
//
// 每条测试都对应方案里一个真实缺陷（修复前必红、修复后必绿），
// 而不是给新代码补覆盖率：
//
//	D1  优雅命令无超时 → mysqladmin 一阻塞，Stop 永久挂起
//	D2  `_ =` 吞掉一切失败原因 → 认证被拒与"正在 flush"被混为一谈
//	D3  等待预算写死 10s → 把"正常关闭的中途"误判成失败并强杀（InnoDB 损坏风险）
//	D4  完全忽略 ctx → 应用退出/用户取消时只能干等
//	D7  判据单一 → 目标进程全退出 ≠ 实例已停（master 重生 worker）
//	D8  三段流程三份拷贝 → 四个 provider 各写一遍、行为互不一致
//	D9  Redis 为保护数据拒绝关闭时被强杀 → 丢用户数据
//	────────────────────────────────────────────────────────────────────────
// 不依赖任何真实 MySQL/Redis/Nginx 安装：用"当前测试进程 PID = 必然存活"
// 与"高号段 PID = 必然不存在"两个稳定事实模拟进程在/不在。
// ────────────────────────────────────────────────────────────────────────────

// livePID 返回一个保证存活的 PID（本测试进程自身）。
func livePID() int { return os.Getpid() }

// deadPID 返回一个保证不存在的 PID。
//
// 不能用"上一个测试里被杀掉的进程"——Windows 会复用 PID，判活结果不可预期。
// 高号段 PID 在 OpenProcess 阶段就失败，IsProcessRunning 稳定返回 false。
func deadPID() int { return 0x0FFFFFFF }

// withShortTimeouts 临时缩小阶梯的时间常量，让锚点测试跑在毫秒级。
// 生产默认值不受影响（var 的存在意义就是这里可替换）。
func withShortTimeouts(t *testing.T, cmdTimeout, poll time.Duration) {
	t.Helper()
	oldCmd, oldPoll, oldReport := stopCmdTimeout, stopPollInterval, stopProgressEvery
	stopCmdTimeout, stopPollInterval, stopProgressEvery = cmdTimeout, poll, time.Hour
	t.Cleanup(func() {
		stopCmdTimeout, stopPollInterval, stopProgressEvery = oldCmd, oldPoll, oldReport
	})
}

// D1：优雅停止命令挂住时，Stop 必须有上限地返回，而不是永久卡死。
//
// 依据 docker-library/mysql 的注释："When using a local socket file mysqladmin
// will block until the shutdown is complete." —— 阻塞是常态，不是异常。
// 旧实现 runStopCommand 用无 deadline 的 CombinedOutput，一挂就是永久。
func TestRunStopCommandCtx_BoundedWhenCommandHangs(t *testing.T) {
	withShortTimeouts(t, 300*time.Millisecond, 10*time.Millisecond)

	start := time.Now()
	outcome, err := runStopCommandCtx(context.Background(),
		filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
		[]string{"/c", "ping", "-n", "30", "127.0.0.1"}, "")
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("挂起的停止命令耗时 %s，阶梯没有兜住（预期 <5s）", elapsed)
	}
	// 命令自身超时 ≠ 停止失败：必须分类成 transient，让调用方继续等进程退出。
	if outcome != stopTransient {
		t.Fatalf("命令超时的 outcome = %s，期望 %s（否则会把'还在 flush'误判成失败）", outcome, stopTransient)
	}
	if err == nil {
		t.Fatal("命令超时必须带回错误，供 stopNote/错误信息如实上报（D2：旧实现直接吞掉）")
	}
}

// D1/D4：父 ctx 取消时立即返回，并带上可识别的取消语义。
func TestRunStopCommandCtx_HonoursParentCancellation(t *testing.T) {
	withShortTimeouts(t, 30*time.Second, 10*time.Millisecond) // 命令级超时故意给不满

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	outcome, err := runStopCommandCtx(ctx,
		filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
		[]string{"/c", "ping", "-n", "30", "127.0.0.1"}, "")
	elapsed := time.Since(start)
	cancel()

	if elapsed > 3*time.Second {
		t.Fatalf("父 ctx 取消后仍耗时 %s，未响应取消信号", elapsed)
	}
	if !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("取消必须可用 errors.Is(ErrStopCancelled) 识别，实际 err=%v", err)
	}
	if outcome != stopTransient {
		t.Fatalf("取消归类为 %s，期望 %s（不能谎报成功）", outcome, stopTransient)
	}
}

// D2：失败原因必须分类，决定"继续等"还是"立刻降级"。
func TestClassifyStopOutput(t *testing.T) {
	cases := []struct {
		name string
		text string
		want stopOutcome
	}{
		{"mysql 认证被拒（等多久都不会成功，应立即降级）",
			"mysqladmin: Access denied for user 'root'@'127.0.0.1 (using password: NO)", stopPermanent},
		{"mysql 权限不足（SHUTDOWN/SYSTEM_VARIABLES_ADMIN）",
			"ERROR 1143 (42000) at line 1: DELETE command denied to user 'app'@'localhost'", stopPermanent},
		{"nginx 找不到 PID 文件（master 早就不在了），该马上清理端口",
			`nginx: [emerg] CreateFile() "G:\nginx\logs\nginx.pid" failed (2: The system cannot find the file specified)`, stopPermanent},
		{"nginx PID 文件内容无效", `nginx: [error] invalid PID number "" in "logs/nginx.pid"`, stopPermanent},
		{"nginx 信号通道找不到 master",
			`nginx: [error] OpenEvent("Global\ngx_quit_1234") failed (5: Access is denied)`, stopPermanent},
		{"服务端已不在监听（不是失败，是已经停了）",
			"mysqladmin: connect to server at '127.0.0.1' failed\nerror: 'Can't connect to MySQL server on '127.0.0.1:3306' (10061)'", stopAccepted},
		{"redis 已退出", "Could not connect to Redis at 127.0.0.1:6379", stopAccepted},
		{"redis 为保护数据拒绝关闭（绝不能强杀）",
			"SHUTDOWN failed: -ERR Shutdown failed: Error saving the RDB file: No space left on device", stopRefused},
		{"redis 只读副本保存被拒", "Can't save in background: Readonly replica", stopRefused},
		{"未知失败保守按待定处理（仍给等待预算）",
			"mysqladmin: unknown error 2013", stopTransient},
		{"空输出按待定处理", "", stopTransient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyStopOutput(c.text); got != c.want {
				t.Fatalf("classifyStopOutput(%q) = %s，期望 %s", firstLine(c.text), got, c.want)
			}
		})
	}
}

// D2/D3：认证被拒（permanent）时跳过本级等待，立刻降级到下一级。
//
// 反例：旧实现即便 100% 拿不到权限，也要先傻等一个固定超时才强杀；
// 新阶梯把 45s 的 MySQL 预算变成"永不多等一秒"。
func TestStopLadder_PermanentFailureEscalatesWithoutWaiting(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	var secondRan bool
	start := time.Now()
	res := runStopLadder(context.Background(),
		map[int]struct{}{livePID(): {}},
		[]stopRung{
			{
				Name:                  "应用层优雅命令",
				Wait:                  30 * time.Second, // 故意给一个不可能等满的预算
				SkipWaitWhenPermanent: true,
				Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
					return stopPermanent, errors.New("Access denied for user 'root'")
				},
			},
			{
				Name: "整树强制终止",
				Wait: 10 * time.Millisecond,
				Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
					secondRan = true
					return stopTransient, nil
				},
			},
		}, nil)

	if !secondRan {
		t.Fatalf("permanent 结果没有降级到下一级，轨迹: %v", res.Trace)
	}
	if res.FirstOutcome != stopPermanent {
		t.Fatalf("FirstOutcome = %s，期望 permanent", res.FirstOutcome)
	}
	if res.FirstErr == nil {
		t.Fatal("第一级错误必须被保留（D2：旧实现用 `_ =` 吞掉，用户看不到失败原因）")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("认证被拒后仍白等了 %s（本级预算 30s 应被跳过）", elapsed)
	}
}

// D9：组件为保护数据拒绝关闭时，阶梯必须中止，绝不升级到强杀。
//
// Redis 官方语义：RDB 保存失败时回复错误但 "the server continues to run"。
// 此时强杀等于把用户数据打死，只能如实上报让人处置。
func TestStopLadder_RefusedHaltsEscalation(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	var killed bool
	res := runStopLadder(context.Background(),
		map[int]struct{}{livePID(): {}},
		[]stopRung{
			commandHaltRung("redis-cli shutdown", 10*time.Millisecond, func(context.Context) (stopOutcome, error) {
				return stopRefused, errors.New("ERR Shutdown failed: Error saving the RDB file")
			}),
			{
				Name: "整树强制终止",
				Wait: 10 * time.Millisecond,
				Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
					killed = true
					return stopAccepted, nil
				},
			},
		}, nil)

	if !res.Halted {
		t.Fatalf("拒绝关闭应中止阶梯，实际轨迹: %v", res.Trace)
	}
	if killed {
		t.Fatal("拒绝关闭后仍然执行了强杀 —— 会丢用户数据")
	}
	if len(res.Remaining) == 0 {
		t.Fatal("服务其实还在运行，Remaining 不能为空（否则上层会谎报已停止）")
	}
}

// D3：命令超时（transient）时仍要给满本级等待预算。
//
// 依据 vitess：MysqldShutdownGracePeriod 的注释是 "after mysqladmin has given up
// waiting" —— 命令放弃后还要单独给一段观察进程退出的时间。
// 旧实现 10s 一到就强杀，正好打死"正在正常关闭"的 mysqld。
func TestStopLadder_TransientStillWaitsFullBudget(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	wait := 1200 * time.Millisecond
	var escalatedAt time.Time
	start := time.Now()
	res := runStopLadder(context.Background(),
		map[int]struct{}{livePID(): {}},
		[]stopRung{
			{
				Name: "应用层优雅命令",
				Wait: wait,
				Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
					return stopTransient, errors.New("命令超过 10s 未返回")
				},
			},
			{
				Name: "整树强制终止",
				Wait: 10 * time.Millisecond,
				Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
					escalatedAt = time.Now()
					return stopTransient, nil
				},
			},
		}, nil)

	if waited := escalatedAt.Sub(start); waited < wait {
		t.Fatalf("transient 结果只等了 %s 就强杀（本级预算 %s）—— 慢关闭被误杀", waited.Round(time.Millisecond), wait)
	}
	if len(res.Remaining) == 0 {
		t.Fatal("进程还在，Remaining 不应为空")
	}
}

// "已经停了就别再补刀"：目标全退出时阶梯在第一级前就返回，不发任何命令。
func TestStopLadder_NoEscalationWhenAlreadyGone(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	var ran int
	res := runStopLadder(context.Background(),
		map[int]struct{}{deadPID(): {}},
		[]stopRung{
			{Name: "应用层优雅命令", Wait: time.Second, Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
				ran++
				return stopAccepted, nil
			}},
			{Name: "整树强制终止", Wait: time.Second, Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
				ran++
				return stopAccepted, nil
			}},
		}, nil)

	if ran != 0 {
		t.Fatalf("目标进程早就不在了，却仍发了 %d 次停止动作（重复强杀会打到 PID 复用后的无关进程）", ran)
	}
	if len(res.Remaining) != 0 {
		t.Fatalf("Remaining = %v，期望空", res.Remaining)
	}
}

// D7 + 每级重扫：master 重生出来的 worker 必须被并入下一级的目标。
func TestStopLadder_RescansBetweenRungs(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	var seen []map[int]struct{}
	rescanCalls := 0
	// 模拟 master 重生 worker：只在两级之间（第 2 次 rescan）冒出一个新 PID。
	// 不能两级都注入 —— 那样本级 wait 后新 PID 已退出，会被存活过滤掉。
	stray := livePID() + 1
	res := runStopLadder(context.Background(),
		map[int]struct{}{livePID(): {}},
		[]stopRung{
			{Name: "r1", Wait: 20 * time.Millisecond, Run: func(_ context.Context, pending map[int]struct{}) (stopOutcome, error) {
				seen = append(seen, pending)
				return stopTransient, nil
			}},
			{Name: "r2", Wait: 20 * time.Millisecond, Run: func(_ context.Context, pending map[int]struct{}) (stopOutcome, error) {
				seen = append(seen, pending)
				return stopTransient, nil
			}},
		}, func(known map[int]struct{}) map[int]struct{} {
			rescanCalls++
			merged := make(map[int]struct{}, len(known)+1)
			for pid := range known {
				merged[pid] = struct{}{}
			}
			if rescanCalls >= 2 {
				merged[stray] = struct{}{}
			}
			return merged
		})

	if len(seen) != 2 {
		t.Fatalf("期望两级都执行，实际 %d 级", len(seen))
	}
	if _, ok := seen[0][stray]; ok {
		t.Fatal("第一级不应该看到尚未出现的新进程")
	}
	if _, ok := seen[1][stray]; !ok {
		t.Fatalf("第二级没有拿到重扫出来的新 worker：%v（master 重生进程会被漏杀，正是同端口累积多个 master 的机制）", seen[1])
	}
	if len(res.Remaining) == 0 {
		t.Fatal("仍有进程存活，Remaining 不应为空")
	}
}

// D4：等待期间 ctx 取消必须立即返回，并把仍存活的进程如实带出。
func TestStopLadder_HonoursContextCancellation(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	start := time.Now()
	res := runStopLadder(ctx,
		map[int]struct{}{livePID(): {}},
		[]stopRung{
			{Name: "应用层优雅命令", Wait: 45 * time.Second, Run: func(context.Context, map[int]struct{}) (stopOutcome, error) {
				return stopTransient, nil
			}},
		}, nil)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("ctx 取消后仍等了 %s（旧实现完全忽略 ctx）", elapsed)
	}
	if len(res.Remaining) == 0 {
		t.Fatal("被取消不等于停成功：Remaining 必须包含仍存活的进程，否则上层会谎报已停止")
	}
}

// waitExit 的 ctx 语义单独锚定（旧 waitProcessesExited 完全不看 ctx）。
func TestWaitExit_RespectsContext(t *testing.T) {
	withShortTimeouts(t, time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	start := time.Now()
	alive := waitExit(ctx, map[int]struct{}{livePID(): {}}, 30*time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("取消后仍等了 %s", elapsed)
	}
	if len(alive) != 1 {
		t.Fatalf("存活进程必须原样带出，实际 %v", alive)
	}

	// 目标本就不存在时必须立刻返回，不能白等一个轮询周期以上。
	start = time.Now()
	if got := waitExit(context.Background(), map[int]struct{}{deadPID(): {}}, 30*time.Second); len(got) != 0 {
		t.Fatalf("不存在的 PID 被判成存活: %v", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("无目标时耗时 %s，应直接返回", elapsed)
	}
}

// D8：四个 provider 走同一条阶梯，且失败信息能指出"卡在哪一级"。
func TestFormatLadderFailureCarriesTrace(t *testing.T) {
	res := stopLadderResult{
		Remaining:    map[int]struct{}{1234: {}},
		Trace:        []string{"mysqladmin shutdown: 等 45s 后仍有 1234 存活", "整树强制终止: taskkill 拒绝访问"},
		FirstOutcome: stopPermanent,
		FirstErr:     errors.New("Access denied for user 'root'@'127.0.0.1"),
	}
	err := formatLadderFailure("MySQL", 3306, res, "优雅关闭、进程级关闭、整树强杀均已尝试")
	msg := err.Error()
	for _, want := range []string{"MySQL", "3306", "1234", "mysqladmin shutdown", "Access denied", "整树强制终止"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息缺少 %q，实际: %s", want, msg)
		}
	}
}

// L4：Stop 全程不得长期持锁 —— 只读调用必须能在毫秒级拿到状态。
//
// 旧 instanceGuard.begin() 直接 Lock() 到 end() 才释放，于是"停止中"
// 任何 Status/日志路径/健康检查都要跟着等完整条阶梯（新实现最长 45s+）。
func TestInstanceGuard_ReadsNotBlockedByBusy(t *testing.T) {
	var g instanceGuard
	if err := g.begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer g.end()

	if !g.isBusy() {
		t.Fatal("begin 之后应处于忙碌态")
	}
	// 忙碌期间读状态：必须立即返回，而不是等 end()。
	done := make(chan struct{})
	go func() {
		g.Lock()
		g.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("忙碌期间只读调用被锁住 —— 停止过程会把整个 UI 的状态查询一起拖死")
	}

	// 单飞：忙碌期间任何并发 begin 必须 fail-fast（避免同一次停止被跑两遍）。
	if err := g.begin(); err == nil {
		t.Fatal("重复 begin 应被拒绝，而不是排队等到前一次结束")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- g.begin() }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("并发 begin 应被拒绝")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("begin 在忙碌时阻塞了 —— 应立报“操作进行中”")
	}

	g.setStopNote("已强杀收尾")
	g.end()
	if got := g.StopNote(); got != "已强杀收尾" {
		t.Fatalf("end() 不应清掉 stopNote（状态落账后才被上层读取），实际 %q", got)
	}
	g.clearStopNote()
	if got := g.StopNote(); got != "" {
		t.Fatalf("clearStopNote 后仍残留 %q", got)
	}
}
