package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/environment"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/project"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/reactive"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
)

// ── 假 provider：Start 可配置为失败 ──

type reconcilerFakeProvider struct {
	*provider.BaseProvider
	failStart bool
	startCalls int32
	stopCalls  int32
}

func newReconcilerFakeProvider(name string, failStart bool) *reconcilerFakeProvider {
	return &reconcilerFakeProvider{
		BaseProvider: provider.NewBaseProvider(name, "0.0.0-test", "test fake", nil),
		failStart:    failStart,
	}
}

func (p *reconcilerFakeProvider) NewInstance(ctx context.Context, cfg map[string]interface{}) (provider.ManagedEntity, error) {
	version := "unknown"
	if v, ok := cfg["version"].(string); ok {
		version = v
	}
	return &reconcilerFakeEntity{id: p.Name() + "@" + version, kind: p.Name(), version: version, p: p}, nil
}

func (p *reconcilerFakeProvider) IsInstalled(version string) bool { return true }

type reconcilerFakeEntity struct {
	id      string
	kind    string
	version string
	status  core.ServiceStatus
	p       *reconcilerFakeProvider
}

func (e *reconcilerFakeEntity) ID() string   { return e.id }
func (e *reconcilerFakeEntity) Type() string { return e.kind }

func (e *reconcilerFakeEntity) Start(ctx context.Context) error {
	e.p.startCalls++
	if e.p.failStart {
		return errors.New("模拟启动失败")
	}
	e.status = core.StatusRunning
	return nil
}

func (e *reconcilerFakeEntity) Stop(ctx context.Context) error {
	e.p.stopCalls++
	e.status = core.StatusStopped
	return nil
}

func (e *reconcilerFakeEntity) Status() (string, error) { return string(e.status), nil }
func (e *reconcilerFakeEntity) LogPaths() []string        { return nil }
func (e *reconcilerFakeEntity) ConfigSchema() provider.ConfigSchema {
	return provider.ConfigSchema{}
}
func (e *reconcilerFakeEntity) HealthCheck(ctx context.Context) error { return nil }
func (e *reconcilerFakeEntity) Configure(values map[string]interface{}) error {
	return nil
}
func (e *reconcilerFakeEntity) Metrics(ctx context.Context) (provider.Metrics, error) {
	return provider.Metrics{}, nil
}

// ── 测试装配 ──

type reconcilerTestEnv struct {
	reconciler *ReconcilerImpl
	svcMgr     *service.Manager
	memcached  *reconcilerFakeProvider
	nginx      *reconcilerFakeProvider
}

func newReconcilerTestEnv(t *testing.T, failKinds map[string]bool) *reconcilerTestEnv {
	t.Helper()
	dir := t.TempDir()
	config.EnsureDirs(dir)
	cfg := config.NewConfigForDir(dir)

	// registerDefaults 只注册 installDir/programs/<kind>/<version> 已存在的版本
	install := func(kind, version string) {
		if err := os.MkdirAll(filepath.Join(dir, "programs", kind, version), 0755); err != nil {
			t.Fatalf("创建安装目录失败: %v", err)
		}
	}
	install("memcached", "1.6.0")
	install("nginx", "1.26.0")

	procMgr := process.NewManager()
	resMgr := resource.NewManager(filepath.Join(dir, "config"))
	reactiveEng := reactive.NewEngine(100)
	svcMgr := service.NewManager(cfg, procMgr, resMgr, reactiveEng)
	rtMgr := runtime.NewManager(cfg, procMgr, resMgr)
	projMgr := project.NewManager(dir, "1.26.0", rtMgr, resMgr)
	stateStore := NewStateStore(filepath.Join(dir, "config"))
	reconciler := NewReconciler(stateStore, svcMgr, projMgr, rtMgr)

	memcachedP := newReconcilerFakeProvider("memcached", failKinds["memcached"])
	nginxP := newReconcilerFakeProvider("nginx", failKinds["nginx"])
	provider.Register("memcached", memcachedP, provider.CapabilityCache)
	provider.Register("nginx", nginxP, provider.CapabilityWebServer)

	return &reconcilerTestEnv{reconciler: reconciler, svcMgr: svcMgr, memcached: memcachedP, nginx: nginxP}
}

// probe: 部分单元启动失败时，应回滚（停止）本次已启动的单元，避免半启状态残留。
func TestStartUnitsRollbackOnFailure(t *testing.T) {
	env := newReconcilerTestEnv(t, map[string]bool{"nginx": true})
	units := []environment.UnitSpec{
		{Kind: "memcached", Version: "1.6.0"},
		{Kind: "nginx", Version: "1.26.0"},
	}
	re := environment.Environment{Name: "dev", Units: units}

	err := env.reconciler.Run(re)
	if err == nil {
		t.Fatalf("nginx 启动失败时 Run 应返回错误")
	}
	if !strings.Contains(err.Error(), "nginx@1.26.0") {
		t.Fatalf("错误应包含失败单元 id, got: %v", err)
	}

	if env.memcached.startCalls != 1 {
		t.Fatalf("memcached 应被启动过 1 次, 实际 %d", env.memcached.startCalls)
	}
	if env.memcached.stopCalls != 1 {
		t.Fatalf("memcached 应在回滚中被停止 1 次, 实际 %d", env.memcached.stopCalls)
	}
	if env.nginx.startCalls != 1 || env.nginx.stopCalls != 0 {
		t.Fatalf("nginx 应仅尝试启动 1 次且不被停止, start=%d stop=%d", env.nginx.startCalls, env.nginx.stopCalls)
	}
}

// probe: 全部单元正常启动时不应回滚，且错误为 nil。
func TestStartUnitsAllSucceed(t *testing.T) {
	env := newReconcilerTestEnv(t, nil)
	units := []environment.UnitSpec{
		{Kind: "memcached", Version: "1.6.0"},
		{Kind: "nginx", Version: "1.26.0"},
	}
	re := environment.Environment{Name: "dev", Units: units}

	if err := env.reconciler.Run(re); err != nil {
		t.Fatalf("全部正常时 Run 不应报错: %v", err)
	}
	if env.memcached.startCalls != 1 || env.nginx.startCalls != 1 {
		t.Fatalf("两个单元应各启动 1 次, memcached=%d nginx=%d", env.memcached.startCalls, env.nginx.startCalls)
	}
	if env.memcached.stopCalls != 0 || env.nginx.stopCalls != 0 {
		t.Fatalf("启动成功阶段不应有停止, memcached=%d nginx=%d", env.memcached.stopCalls, env.nginx.stopCalls)
	}

	// Deactivate 应停止全部单元
	if err := env.reconciler.Deactivate(re); err != nil {
		t.Fatalf("Deactivate 不应报错: %v", err)
	}
	if env.memcached.stopCalls != 1 || env.nginx.stopCalls != 1 {
		t.Fatalf("Deactivate 应停止全部单元, memcached=%d nginx=%d", env.memcached.stopCalls, env.nginx.stopCalls)
	}
}
