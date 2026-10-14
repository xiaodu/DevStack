package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/download"
	"github.com/xiaodu/DevStack/internal/engine"
	"github.com/xiaodu/DevStack/internal/environment"
	"github.com/xiaodu/DevStack/internal/log"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/project"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/reactive"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
)

// ────────────────────────────────────────────────────────────────────────────
// 用户用例 E2E：添加环境 → 下载安装组件 → 激活 → 创建站点 → 启动/停止服务
//
// 使用专门的测试目录（默认项目根/.e2e，可用 DEVSTACK_E2E_DIR 覆盖），
// 在真实文件系统上读写（environments.json / projects.json / environment.json /
// nginx 站点配置等）。组件下载→解压→配置走真实代码路径（本地 httptest 提供
// 假安装包）；仅服务进程的启停由假 provider 接管（不启动真实 nginx/mysql 进程）。
// ────────────────────────────────────────────────────────────────────────────

func e2eDir() string {
	if d := os.Getenv("DEVSTACK_E2E_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return ".e2e"
	}
	return filepath.Join(wd, ".e2e")
}

// e2eAppDir 计算并清理单个用例的专用目录。
func e2eAppDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(e2eDir(), strings.ReplaceAll(name, "/", "_"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("清理测试目录失败: %v", err)
	}
	return dir
}

// newE2EApp 装配一个完整 App（复用真实 manager 构造），组件由假 provider 接管。
func newE2EApp(t *testing.T, dir string) *App {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建安装目录失败: %v", err)
	}
	config.EnsureDirs(dir)
	cfg := config.NewConfigForDir(dir)

	opStore := log.NewOpStore(200)
	procMgr := process.NewManager()
	resMgr := resource.NewManager(filepath.Join(dir, "config"))
	reactiveEng := reactive.NewEngine(100)
	svcMgr := service.NewManager(cfg, procMgr, resMgr, reactiveEng)
	rtMgr := runtime.NewManager(cfg, procMgr, resMgr)
	projMgr := project.NewManager(dir, "1.26.0", rtMgr, resMgr)
	projMgr.SetServiceManager(svcMgr)
	projMgr.SetRuntimeManager(rtMgr)
	envMgr := environment.NewManager(dir)
	dlMgr := download.NewManager(dir)
	stateStore := engine.NewStateStore(filepath.Join(dir, "config"))
	reconciler := engine.NewReconciler(stateStore, svcMgr, projMgr, rtMgr)

	app := &App{
		cfg:         cfg,
		procMgr:     procMgr,
		resMgr:      resMgr,
		svcMgr:      svcMgr,
		rtMgr:       rtMgr,
		projMgr:     projMgr,
		envMgr:      envMgr,
		dlMgr:       dlMgr,
		opStore:     opStore,
		stateStore:  stateStore,
		reconciler:  reconciler,
		reactiveEng: reactiveEng,
		AppVersion:  "e2e-test",
		downloads:   map[string]struct{}{},
	}

	// 假 provider 接管服务启动/停止（不启动真实进程），注册为覆盖式。
	provider.Register("nginx", newFakeProvider("nginx"), provider.CapabilityWebServer)
	provider.Register("mysql", newFakeProvider("mysql"), provider.CapabilityDatabase)
	return app
}

// ── 假 provider / entity ──

type fakeProvider struct {
	*provider.BaseProvider
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{
		BaseProvider: provider.NewBaseProvider(name, "0.0.0-e2e", "e2e fake provider", nil),
	}
}

func (p *fakeProvider) NewInstance(ctx context.Context, cfg map[string]interface{}) (provider.ManagedEntity, error) {
	version := "unknown"
	if v, ok := cfg["version"].(string); ok {
		version = v
	}
	port := 0
	if pt, ok := cfg["port"].(int); ok {
		port = pt
	}
	return &fakeEntity{id: p.Name() + "@" + version, kind: p.Name(), version: version, port: port}, nil
}

func (p *fakeProvider) IsInstalled(version string) bool { return true }

// fakeEntity 用「拷贝到假安装目录内的测试二进制」作为真实服务进程：
// 子进程监听指定端口后阻塞，使 GetServices 的真实状态同步
// （FindPIDByPort + exePathMatchesPID）能诚实验证"运行中"，
// 而不是仅靠 provider 自报状态。
type fakeEntity struct {
	id      string
	kind    string
	version string
	port    int
	status  core.ServiceStatus
	cmd     *exec.Cmd
}

func (e *fakeEntity) ID() string              { return e.id }
func (e *fakeEntity) Type() string            { return e.kind }
func (e *fakeEntity) Status() (string, error) { return string(e.status), nil }
func (e *fakeEntity) LogPaths() []string      { return nil }
func (e *fakeEntity) ConfigSchema() provider.ConfigSchema {
	return provider.ConfigSchema{}
}

// TestFakeServiceHelper 是假服务进程模式：以 DEVSTACK_E2E_HELPER_PORT 环境变量
// 激活，在指定端口监听 TCP 并阻塞，直到进程被杀。被父测试进程 exec 启动。
func TestFakeServiceHelper(t *testing.T) {
	port := os.Getenv("DEVSTACK_E2E_HELPER_PORT")
	if port == "" {
		t.Skip("helper 模式需 DEVSTACK_E2E_HELPER_PORT")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Skipf("helper 监听 %s 失败: %v", port, err)
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}
}

func (e *fakeEntity) Start(ctx context.Context) error {
	// 把测试二进制自身拷贝进假安装目录，作为"属于本服务安装目录"的可执行进程
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取测试二进制路径失败: %w", err)
	}
	// 安装目录由测试目录约定：fakeEntity 不知道 installDir，用环境变量传入
	installDir := os.Getenv("DEVSTACK_E2E_DIR")
	helperDir := filepath.Join(installDir, "programs", e.kind, e.version)
	if err := os.MkdirAll(helperDir, 0755); err != nil {
		return err
	}
	helperExe := filepath.Join(helperDir, "e2e-service.exe")
	if data, err := os.ReadFile(self); err == nil {
		if err := os.WriteFile(helperExe, data, 0755); err != nil {
			return fmt.Errorf("拷贝 helper 失败: %w", err)
		}
	} else {
		return fmt.Errorf("读取测试二进制失败: %w", err)
	}

	cmd := exec.Command(helperExe, "-test.run=TestFakeServiceHelper", "-test.timeout=10m")
	cmd.Env = append(os.Environ(), "DEVSTACK_E2E_HELPER_PORT="+strconv.Itoa(e.port))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动假服务进程失败: %w", err)
	}
	e.cmd = cmd

	// 等端口真正监听，启动才算成功（与真实服务的就绪语义一致）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid := process.FindPIDByPort(e.port); pid == cmd.Process.Pid {
			e.status = core.StatusRunning
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd.Process.Kill()
	return fmt.Errorf("假服务进程未能在端口 %d 就绪", e.port)
}

func (e *fakeEntity) Stop(ctx context.Context) error {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_, _ = e.cmd.Process.Wait()
	}
	e.cmd = nil
	e.status = core.StatusStopped
	return nil
}

func (e *fakeEntity) HealthCheck(ctx context.Context) error { return nil }
func (e *fakeEntity) Configure(values map[string]interface{}) error {
	return nil
}

func (e *fakeEntity) Metrics(ctx context.Context) (provider.Metrics, error) {
	return provider.Metrics{}, nil
}

// ── 断言辅助 ──

func serviceStateMap(t *testing.T, infos []core.ServiceInfo) map[string]core.ServiceStatus {
	t.Helper()
	m := make(map[string]core.ServiceStatus, len(infos))
	for _, s := range infos {
		m[s.ID] = s.Status
	}
	return m
}

func assertServiceState(t *testing.T, m map[string]core.ServiceStatus, id string, want core.ServiceStatus) {
	t.Helper()
	got, ok := m[id]
	if !ok {
		t.Fatalf("服务 %s 未注册（已注册: %v）", id, keysOfStatus(m))
	}
	if got != want {
		t.Fatalf("服务 %s 状态期望 %s，实际 %s", id, want, got)
	}
}

func keysOfStatus(m map[string]core.ServiceStatus) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func fileContains(t *testing.T, path, sub string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), sub)
}

// makeZip 构造内存中的 zip 包字节（map: zip 内路径 → 内容）。
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目 %s 失败: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 zip 条目 %s 失败: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip writer 失败: %v", err)
	}
	return buf.Bytes()
}

// ── 用户用例：添加环境 → 激活 → 创建站点 → 启动/停止服务 → 停用环境 ──

func TestUserFlow_AddEnvActivateCreateSiteStartStopDeactivate(t *testing.T) {
	dir := e2eAppDir(t, t.Name())
	app := newE2EApp(t, dir)

	// 1. 添加环境
	env, err := app.CreateEnvironment(environment.Environment{
		Name:        "e2e-测试环境",
		Description: "用户用例自动测试环境",
		Units: []environment.UnitSpec{
			{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
			{Kind: "mysql", Version: "8.0.39", Name: "MySQL"},
		},
	})
	if err != nil {
		t.Fatalf("添加环境失败: %v", err)
	}
	if env.ID == "" {
		t.Fatal("环境 ID 为空")
	}
	if !fileContains(t, filepath.Join(dir, "config", "environments.json"), env.ID) {
		t.Fatal("环境未持久化到 environments.json")
	}

	// 2. 真实下载并安装组件：本地 httptest 提供假安装包，完整走下载→解压→配置链路
	nginxZip := makeZip(t, map[string]string{"nginx-1.24.0/README.md": "fake nginx"})
	mysqlZip := makeZip(t, map[string]string{"mysql-8.0.39/README.md": "fake mysql"})
	mux := http.NewServeMux()
	mux.HandleFunc("/nginx-1.24.0.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(nginxZip) })
	mux.HandleFunc("/mysql-8.0.39.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(mysqlZip) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := app.DownloadService("nginx", "1.24.0", srv.URL+"/nginx-1.24.0.zip"); err != nil {
		t.Fatalf("下载安装 nginx 失败: %v", err)
	}
	if err := app.DownloadService("mysql", "8.0.39", srv.URL+"/mysql-8.0.39.zip"); err != nil {
		t.Fatalf("下载安装 mysql 失败: %v", err)
	}
	if !fileContains(t, filepath.Join(dir, "programs", "nginx", "1.24.0", "conf", "nginx.conf"), "worker_processes") {
		t.Fatal("nginx 安装后未生成配置文件 programs/nginx/1.24.0/conf/nginx.conf")
	}
	if !fileContains(t, filepath.Join(dir, "programs", "mysql", "8.0.39", "conf", "my.ini"), "mysqld") {
		t.Fatal("mysql 安装后未生成配置文件 programs/mysql/8.0.39/conf/my.ini")
	}

	// 3. 激活环境：批量启动全部组件 + 记录当前环境
	if err := app.ActivateEnvironment(env.ID); err != nil {
		t.Fatalf("激活环境失败: %v", err)
	}
	activeID, _ := app.getActiveEnvironmentID()
	if activeID != env.ID {
		t.Fatalf("激活后 activeEnvironmentId 期望 %s，实际 %q", env.ID, activeID)
	}
	svcState := serviceStateMap(t, app.GetServices())
	assertServiceState(t, svcState, "nginx@1.24.0", core.StatusRunning)
	assertServiceState(t, svcState, "mysql@8.0.39", core.StatusRunning)
	if !fileContains(t, filepath.Join(dir, "config", "environment.json"), env.ID) {
		t.Fatal("激活状态未持久化到 environment.json")
	}

	// 4. 创建站点（Enabled=false 避免写真实系统 hosts；其余真实写盘）
	proj, err := app.CreateProject(core.Project{
		Name:          "e2e-demo",
		Domain:        "e2e-demo.local",
		Root:          filepath.Join(dir, "www", "e2e-demo"),
		EnvironmentID: env.ID,
		NginxVersion:  "1.24.0",
		MySQLVersion:  "8.0.39",
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("创建站点失败: %v", err)
	}
	if proj == nil || proj.ID == "" {
		t.Fatal("站点 ID 为空")
	}
	if !fileContains(t, filepath.Join(dir, "config", "projects.json"), proj.ID) {
		t.Fatal("站点未持久化到 projects.json")
	}

	// 5. 启动/停止单个服务（区别于激活：不影响当前环境标记）
	if err := app.StartService("nginx@1.24.0"); err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	svcState = serviceStateMap(t, app.GetServices())
	assertServiceState(t, svcState, "nginx@1.24.0", core.StatusRunning)
	if activeID, _ = app.getActiveEnvironmentID(); activeID != env.ID {
		t.Fatalf("单服务启动不应改变当前环境，期望 %s，实际 %q", env.ID, activeID)
	}

	if err := app.StopService("nginx@1.24.0"); err != nil {
		t.Fatalf("停止服务失败: %v", err)
	}
	svcState = serviceStateMap(t, app.GetServices())
	assertServiceState(t, svcState, "nginx@1.24.0", core.StatusStopped)
	if activeID, _ = app.getActiveEnvironmentID(); activeID != env.ID {
		t.Fatalf("单服务停止不应改变当前环境，期望 %s，实际 %q", env.ID, activeID)
	}

	// 6. 停用环境：停止全部组件 + 清空当前环境标记
	if err := app.DeactivateEnvironment(); err != nil {
		t.Fatalf("停用环境失败: %v", err)
	}
	if activeID, _ = app.getActiveEnvironmentID(); activeID != "" {
		t.Fatalf("停用后 activeEnvironmentId 应为空，实际 %q", activeID)
	}
	svcState = serviceStateMap(t, app.GetServices())
	assertServiceState(t, svcState, "nginx@1.24.0", core.StatusStopped)
	assertServiceState(t, svcState, "mysql@8.0.39", core.StatusStopped)
}

// ── 回归：激活状态重启后恢复（environment.json 持久化读回） ──

func TestUserFlow_ActiveEnvironmentSurvivesRestart(t *testing.T) {
	dir := e2eAppDir(t, t.Name())
	app := newE2EApp(t, dir)

	env, err := app.CreateEnvironment(environment.Environment{
		Name: "e2e-restart-env",
		Units: []environment.UnitSpec{
			{Kind: "nginx", Version: "1.24.0", Name: "Nginx"},
		},
	})
	if err != nil {
		t.Fatalf("添加环境失败: %v", err)
	}
	app.svcMgr.MarkInstalled("nginx", "1.24.0")
	if err := app.ActivateEnvironment(env.ID); err != nil {
		t.Fatalf("激活环境失败: %v", err)
	}

	// 重新装配（模拟重启），环境/站点/激活标记应从磁盘恢复
	app2 := newE2EApp(t, dir)
	if activeID, _ := app2.getActiveEnvironmentID(); activeID != env.ID {
		t.Fatalf("重启后 activeEnvironmentId 期望 %s，实际 %q", env.ID, activeID)
	}
	if _, err := app2.envMgr.Get(env.ID); err != nil {
		t.Fatalf("重启后环境 %s 应存在: %v", env.ID, err)
	}
}
