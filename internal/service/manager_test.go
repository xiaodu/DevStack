package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/reactive"
	"github.com/xiaodu/DevStack/internal/resource"
)

type testFakeProvider struct {
	*provider.BaseProvider
	failStart  bool
	startCalls int32
	stopCalls  int32
	// stopDelay 模拟真实关闭耗时（等进程退出/端口复核），用于验证
	// Stop 不在持有 Manager.mu 的情况下跨过慢操作。
	stopDelay time.Duration
}

func newTestFakeProvider(name string, failStart bool) *testFakeProvider {
	return &testFakeProvider{
		BaseProvider: provider.NewBaseProvider(name, "0.0.0-test", "test fake", nil),
		failStart:    failStart,
	}
}

func (p *testFakeProvider) NewInstance(ctx context.Context, cfg map[string]interface{}) (provider.ManagedEntity, error) {
	version := "unknown"
	if v, ok := cfg["version"].(string); ok {
		version = v
	}
	return &testFakeEntity{id: p.Name() + "@" + version, p: p}, nil
}

func (p *testFakeProvider) IsInstalled(version string) bool { return true }

type testFakeEntity struct {
	id     string
	status core.ServiceStatus
	p      *testFakeProvider
}

func (e *testFakeEntity) ID() string   { return e.id }
func (e *testFakeEntity) Type() string { return kindFromID(e.id) }

func (e *testFakeEntity) Start(ctx context.Context) error {
	atomic.AddInt32(&e.p.startCalls, 1)
	if e.p.failStart {
		return errors.New("模拟启动失败")
	}
	e.status = core.StatusRunning
	return nil
}

func (e *testFakeEntity) Stop(ctx context.Context) error {
	atomic.AddInt32(&e.p.stopCalls, 1)
	if e.p.stopDelay > 0 {
		time.Sleep(e.p.stopDelay)
	}
	e.status = core.StatusStopped
	return nil
}

func (e *testFakeEntity) Status() (string, error) { return string(e.status), nil }
func (e *testFakeEntity) LogPaths() []string      { return nil }
func (e *testFakeEntity) ConfigSchema() provider.ConfigSchema {
	return provider.ConfigSchema{}
}
func (e *testFakeEntity) HealthCheck(ctx context.Context) error { return nil }
func (e *testFakeEntity) Configure(values map[string]interface{}) error {
	return nil
}
func (e *testFakeEntity) Metrics(ctx context.Context) (provider.Metrics, error) {
	return provider.Metrics{}, nil
}

func newTestManager(t *testing.T, failStart bool) (*Manager, *testFakeProvider) {
	t.Helper()
	dir := t.TempDir()
	config.EnsureDirs(dir)
	cfg := config.NewConfigForDir(dir)
	installDir := filepath.Join(dir, "programs", "memcached", "1.6.0")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatalf("创建安装目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "memcached.exe"), []byte("x"), 0755); err != nil {
		t.Fatalf("创建测试可执行文件失败: %v", err)
	}

	fp := newTestFakeProvider("memcached", failStart)
	provider.Register("memcached", fp, provider.CapabilityCache)

	procMgr := process.NewManager()
	resMgr := resource.NewManager(filepath.Join(dir, "config"))
	reactiveEng := reactive.NewEngine(100)
	return NewManager(cfg, procMgr, resMgr, reactiveEng), fp
}

// probe: 启动失败必须把状态回写为 Stopped，不能残留 Starting/Unknown。
func TestManager_StartFailureResetsStatus(t *testing.T) {
	m, _ := newTestManager(t, true)
	if err := m.Start("memcached@1.6.0"); err == nil {
		t.Fatalf("Start 应返回失败")
	}
	s := m.GetService("memcached@1.6.0")
	if s == nil {
		t.Fatalf("服务未注册")
	}
	if s.Info.Status != core.StatusStopped {
		t.Fatalf("启动失败后状态应为 Stopped, 实际 %s", s.Info.Status)
	}
	if s.process != nil {
		t.Fatalf("启动失败后不应残留进程句柄")
	}
}

// probe: 启动成功置 Running；重复 Start 应是无操作（不重复拉起）。
func TestManager_StartSucceedsAndIdempotent(t *testing.T) {
	m, fp := newTestManager(t, false)
	if err := m.Start("memcached@1.6.0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if s := m.GetService("memcached@1.6.0"); s.Info.Status != core.StatusRunning {
		t.Fatalf("启动后状态应为 Running, 实际 %s", s.Info.Status)
	}
	if atomic.LoadInt32(&fp.startCalls) != 1 {
		t.Fatalf("Start 应调用 provider 1 次, 实际 %d", fp.startCalls)
	}
	// 重复 Start：无操作
	if err := m.Start("memcached@1.6.0"); err != nil {
		t.Fatalf("重复 Start 不应报错: %v", err)
	}
	if atomic.LoadInt32(&fp.startCalls) != 1 {
		t.Fatalf("重复 Start 不应再次拉起, 实际 %d 次", fp.startCalls)
	}
}

// probe: Stop 后状态回写 Stopped 且清理进程句柄（无 watcher 复活通道）；
// 重复 Stop 幂等且不 panic。
func TestManager_StopClearsAndIdempotent(t *testing.T) {
	m, fp := newTestManager(t, false)
	if err := m.Start("memcached@1.6.0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := m.Stop("memcached@1.6.0"); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	s := m.GetService("memcached@1.6.0")
	if s.Info.Status != core.StatusStopped {
		t.Fatalf("Stop 后状态应为 Stopped, 实际 %s", s.Info.Status)
	}
	if s.process != nil {
		t.Fatalf("Stop 后应清理进程句柄（自愈不得复活已停服务）")
	}
	if s.stopped != nil {
		t.Fatalf("Stop 后 stopped 通道应被清空")
	}
	if err := m.Stop("memcached@1.6.0"); err != nil {
		t.Fatalf("重复 Stop 不应报错: %v", err)
	}
	if atomic.LoadInt32(&fp.stopCalls) == 0 {
		t.Fatalf("Stop 未调用 provider")
	}
}

// probe: 多版本共存时，精确 ID 必须稳定命中对应版本——不能按 kind 模糊匹配
// （Go map 随机遍历会导致随机启动/停止任意版本，即"环境写 1.26.0 却拉起
// 1.20.2"的根源）。模糊匹配仅作为显示名/kind 查询的兜底保留。
func TestManager_FindServiceByNameExactIDWins(t *testing.T) {
	m, _ := newTestManager(t, false)
	m.mu.Lock()
	for _, ver := range []string{"1.20.2", "1.24.0", "1.26.0"} {
		id := "nginx@" + ver
		m.services[id] = &ServiceInstance{
			Info: core.ServiceInfo{
				ID: id, Kind: "nginx", Name: "Nginx",
				Version: ver, Port: 80, Status: core.StatusStopped,
			},
		}
	}
	m.mu.Unlock()

	// map 遍历顺序随机，多次查询确保精确 ID 始终命中同一实例
	for i := 0; i < 100; i++ {
		s := m.findServiceByName("nginx@1.26.0")
		if s == nil || s.Info.Version != "1.26.0" {
			t.Fatalf("第 %d 次: 精确 ID 应命中 1.26.0, 实际 %+v", i, s)
		}
	}
	// 兜底：按显示名/kind 仍可查到（返回哪个版本均可）
	if s := m.findServiceByName("Nginx"); s == nil {
		t.Fatalf("按显示名应能查到服务")
	}
}

// probe: 并发 Start/Stop 不应 panic / data race，且不丢失服务实例。
func TestManager_ConcurrentStartStop(t *testing.T) {
	m, _ := newTestManager(t, false)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				err = m.Start("memcached@1.6.0")
			} else {
				err = m.Stop("memcached@1.6.0")
			}
			if err != nil {
				// Stop 在未运行时可返回“未找到或已退出”类错误，属于正常竞态结果
				t.Logf("goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	s := m.GetService("memcached@1.6.0")
	if s == nil {
		t.Fatalf("并发后服务实例丢失")
	}
	valid := s.Info.Status == core.StatusStopped || s.Info.Status == core.StatusRunning
	if !valid {
		t.Fatalf("并发后状态非法: %s", s.Info.Status)
	}
}

// TestVersionInstalled 覆盖批次B#5：目录存在 ≠ 已安装，主可执行文件存在才算。
func TestVersionInstalled(t *testing.T) {
	tmp := t.TempDir()
	mk := func(kind, ver, rel string) {
		d := filepath.Join(tmp, "programs", kind, ver)
		if rel != "" {
			if err := os.MkdirAll(filepath.Join(d, filepath.Dir(rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, rel), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	mk("mysql", "8.0.38", "")                                 // 空目录（半解压/误建）
	mk("nginx", "1.26.0", "")                                 // 空目录
	mk("redis", "7.2.0", "redis-server.exe")                  // 有 exe
	mk("mysql", "8.0.39", filepath.Join("bin", "mysqld.exe")) // 有 exe
	mk("apache", "2.4.60", "")                                // 未知类型：目录存在即算

	if versionInstalled(tmp, core.TypeMySQL, "8.0.38") {
		t.Errorf("空 mysqld 目录不应判为已安装")
	}
	if versionInstalled(tmp, core.TypeNginx, "1.26.0") {
		t.Errorf("空 nginx 目录不应判为已安装")
	}
	if !versionInstalled(tmp, core.TypeRedis, "7.2.0") {
		t.Errorf("含 redis-server.exe 应判为已安装")
	}
	if !versionInstalled(tmp, core.TypeMySQL, "8.0.39") {
		t.Errorf("含 bin/mysqld.exe 应判为已安装")
	}
	if !versionInstalled(tmp, core.TypeApache, "2.4.60") {
		t.Errorf("未知类型应退化为目录存在判断")
	}
}

// TestUninstallVersionRenameQuarantine 覆盖 P2：卸载走"同目录改名隔离→删除"，
// 不再整目录拷贝备份；成功后原位与 .backup 都不应残留。
func TestUninstallVersionRenameQuarantine(t *testing.T) {
	mgr, _ := newTestManager(t, false)
	dir := mgr.cfg.Get().InstallDir
	binPath := filepath.Join(dir, "programs", "memcached", "1.6.0")
	backupPath := binPath + ".backup"
	if err := os.WriteFile(filepath.Join(binPath, "memcached.exe"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := mgr.UninstallVersion("memcached", "1.6.0"); err != nil {
		t.Fatalf("UninstallVersion: %v", err)
	}
	if dirExists(binPath) {
		t.Errorf("原位目录应已被移走/删除")
	}
	if dirExists(backupPath) {
		t.Errorf(".backup 隔离目录应在事务成功后被删除")
	}
	if s := mgr.GetService("memcached@1.6.0"); s != nil && s.Info.Installed {
		t.Errorf("卸载后 Installed 应为 false")
	}
}
