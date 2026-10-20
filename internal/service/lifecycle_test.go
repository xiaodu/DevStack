package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

// 回归：Start 成功曾无条件把崩溃计数清零，"起得来但几秒就死"的服务每轮都是 1，
// 永远够不到 maxRestartCount 熔断，表现为无限重启。
func TestDecayRestartCount(t *testing.T) {
	if got := decayRestartCount(2, nil); got != 2 {
		t.Errorf("上一轮没有运行记录时不应清零, 实际 %d", got)
	}
	fresh := &process.ProcessHandle{PID: 1, Started: time.Now().Add(-5 * time.Second)}
	if got := decayRestartCount(2, fresh); got != 2 {
		t.Errorf("上一轮只活了 5s 不应清零, 实际 %d", got)
	}
	stable := &process.ProcessHandle{PID: 1, Started: time.Now().Add(-2 * stableRunWindow)}
	if got := decayRestartCount(2, stable); got != 0 {
		t.Errorf("上一轮稳定运行过应清零, 实际 %d", got)
	}
	if got := decayRestartCount(0, stable); got != 0 {
		t.Errorf("本来就是 0 时不应变化, 实际 %d", got)
	}
}

// 回归：启动进行中再点启动曾被幂等地当成"成功"返回（其实进程还没起来）；
// 启动进行中点停止则与 Start 尾段互相覆盖状态。
func TestStartAndStopDuringStarting(t *testing.T) {
	m, _ := newTestManager(t, false)

	setStatus := func(status core.ServiceStatus) {
		t.Helper()
		m.mu.Lock()
		s := m.services["memcached@1.6.0"]
		if s == nil {
			t.Fatalf("服务未注册")
		}
		s.Info.Status = status
		m.mu.Unlock()
	}

	setStatus(core.StatusStarting)
	if err := m.Start("memcached@1.6.0"); !errors.Is(err, core.ErrStartInProgress) {
		t.Errorf("启动中再启动应返回 ErrStartInProgress, 实际 %v", err)
	}
	if err := m.Stop("memcached@1.6.0"); !errors.Is(err, core.ErrStopBlocked) {
		t.Errorf("启动中停止应返回 ErrStopBlocked, 实际 %v", err)
	}

	// 已运行仍是幂等成功（不重复拉起）
	setStatus(core.StatusRunning)
	if err := m.Start("memcached@1.6.0"); err != nil {
		t.Errorf("已运行应幂等返回 nil, 实际 %v", err)
	}
}

// 兄弟版本占用同一端口时，Start 需要能认出"该先停谁"。
func TestInstanceOnPortFindsSiblingVersion(t *testing.T) {
	m := nginxMgr(
		inst("nginx@1.24.0", "nginx", "1.24.0", core.StatusRunning),
		inst("nginx@1.26.2", "nginx", "1.26.2", core.StatusStopped),
		inst("redis@7.4.2", "redis", "7.4.2", core.StatusRunning),
	)
	m.mu.Lock()
	m.services["nginx@1.24.0"].Info.Port = 80
	m.services["nginx@1.26.2"].Info.Port = 80
	m.services["redis@7.4.2"].Info.Port = 80
	m.mu.Unlock()

	if got := m.instanceOnPort("nginx", 80, "nginx@1.26.2"); got != "nginx@1.24.0" {
		t.Errorf("应找到占用 80 端口的兄弟版本 nginx@1.24.0, 实际 %q", got)
	}
	// 不同 kind 不算兄弟
	if got := m.instanceOnPort("nginx", 80, "nginx@1.24.0"); got != "" {
		t.Errorf("排除自身后不应命中其它 kind, 实际 %q", got)
	}
	// 已停止的实例不该被要求先停
	m.mu.Lock()
	m.services["nginx@1.24.0"].Info.Status = core.StatusStopped
	m.mu.Unlock()
	if got := m.instanceOnPort("nginx", 80, "nginx@1.26.2"); got != "" {
		t.Errorf("已停止的兄弟版本不应被视为占用者, 实际 %q", got)
	}
}

// 并发不变量固化（code-review P0-2/P0-3 的守护缺口）：
// 并发 Start 只允许真正拉起一份进程——状态守卫（Starting/Running）必须在
// 同一把锁内完成“检查+置位”，否则两个 goroutine 会各自拉起一个实例。
func TestConcurrentStartSpawnsProcessOnlyOnce(t *testing.T) {
	m, fp := newTestManager(t, false)

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- m.Start("memcached@1.6.0")
		}()
	}
	wg.Wait()
	close(errCh)

	if calls := atomic.LoadInt32(&fp.startCalls); calls > 1 {
		t.Fatalf("并发 Start 实体拉起进程至多一次，实际 entity.Start 调用 %d 次", calls)
	}
	if atomic.LoadInt32(&fp.startCalls) < 1 {
		t.Fatalf("start 一次都没发生，守卫把启动拦死了")
	}
	var ok bool
	for err := range errCh {
		if err == nil || errors.Is(err, core.ErrStartInProgress) {
			ok = true
			continue
		}
		t.Fatalf("并发 Start 不应出现其它错误: %v", err)
	}
	if !ok {
		t.Fatalf("并发 Start 全部失败")
	}
}

// 并发不变量固化（code-review P0-3：Stop 不长期持锁）：
// 真实关闭（等进程退出/端口复核）是秒级慢操作，若跨过它持有 Manager.mu，
// 周期状态同步/日志等所有读路径都会被拖死。这里用带延迟的假实体实测：
// Stop 进行中，GetService 必须在短窗口内返回。
func TestStopDoesNotHoldManagerLockAcrossSlowClose(t *testing.T) {
	m, fp := newTestManager(t, false)
	fp.stopDelay = 600 * time.Millisecond
	if err := m.Start("memcached@1.6.0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		_ = m.Stop("memcached@1.6.0")
	}()
	// 等 Stop 进入慢关闭阶段
	time.Sleep(100 * time.Millisecond)

	begin := time.Now()
	_ = m.GetService("memcached@1.6.0")
	if elapsed := time.Since(begin); elapsed > 200*time.Millisecond {
		t.Fatalf("Stop 持 Manager.mu 跨过慢关闭（读状态被拖 %dms），周期同步/日志均会卡顿", elapsed.Milliseconds())
	}
	<-stopDone
}
