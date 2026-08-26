package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	stdlog "log"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xiaodu/DevStack/internal/autostart"
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
	"github.com/xiaodu/DevStack/internal/tray"
)

type App struct {
	ctx         context.Context
	cfg         *config.Config
	procMgr     *process.Manager
	resMgr      *resource.Manager
	svcMgr      *service.Manager
	rtMgr       *runtime.Manager
	projMgr     *project.Manager
	envMgr      *environment.Manager
	logWatcher  *log.LogWatcher
	dlMgr       *download.Manager
	opStore     *log.OpStore
	stateStore  *engine.StateStore
	reconciler  *engine.ReconcilerImpl
	reactiveEng *reactive.Engine
	tray        *tray.Tray
	AppVersion  string
	envOpMu     sync.Mutex // 串行化环境启停操作，防止并发激活

	downloadMu sync.Mutex               // 保护 downloads 单飞去重表
	downloads  map[string]struct{}      // 正在下载中的 service@version
}

var serviceNames = map[string]string{
	"nginx": "Nginx", "mysql": "MySQL", "redis": "Redis",
}

var wailsLogFile *os.File

var runtimeNames = map[string]string{
	"php": "PHP", "node": "Node.js", "python": "Python",
}

func NewApp() *App {
	var fileLogger *log.FileLogger
	var opStore *log.OpStore

	startTime := time.Now()

	opStore = log.NewOpStore(200)

	cfg := config.NewConfig()
	cfgSettings := cfg.Get()
	installDir := cfgSettings.InstallDir

	opStore.LogInfo("[INIT] NewApp 开始初始化, 当前时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	opStore.LogInfo("[INIT] 配置文件路径: %s", cfg.FilePath())
	opStore.LogInfo("[INIT] 安装目录: %s", installDir)

	if installDir != "" {
		wailsLogPath := filepath.Join(installDir, "logs", "wails.log")
		logFile, err := os.OpenFile(wailsLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			wailsLogFile = logFile
			stdlog.SetOutput(logFile)
			stdlog.Println("[WAILS] 日志文件初始化: " + wailsLogPath)
		} else {
			opStore.LogWarn("[INIT] 无法打开 Wails 日志文件 %s: %v", wailsLogPath, err)
		}
	} else {
		stdlog.Println("[WAILS] 安装目录为空，日志写入 stderr")
	}

	if installDir != "" {
		opStore.LogInfo("[INIT] 创建必要目录...")
		config.EnsureDirs(installDir)
		opStore.LogInfo("[INIT] 目录创建完成")
	} else {
		opStore.LogWarn("[INIT] 安装目录为空，将显示设置向导")
	}

	if installDir != "" {
		logDir := filepath.Join(installDir, "logs")
		opStore.LogInfo("[INIT] 初始化文件日志系统, 日志目录: %s", logDir)
		var err error
		fileLogger, err = log.NewFileLogger(log.FileLoggerConfig{
			LogDir:  logDir,
			MaxSize: 10 * 1024 * 1024,
			MaxDays: 30,
			Level:   log.LevelDebug,
		})
		if err != nil {
			opStore.LogError("[INIT] 文件日志系统初始化失败: %v", err)
		} else {
			opStore.SetLogger(fileLogger)
			opStore.LogInfo("[INIT] 文件日志系统初始化成功")
		}
	} else {
		opStore.LogInfo("[INIT] 安装目录为空，跳过文件日志系统初始化")
	}

	opStore.LogInfo("[INIT] 初始化进程管理器...")
	procMgr := process.NewManager()
	opStore.LogInfo("[INIT] 进程管理器初始化完成")

	var resMgr *resource.Manager
	if installDir != "" {
		opStore.LogInfo("[INIT] 初始化资源管理器...")
		resMgr = resource.NewManager(filepath.Join(installDir, "config"))
		opStore.LogInfo("[INIT] 资源管理器初始化完成")
	} else {
		opStore.LogInfo("[INIT] 安装目录为空，跳过资源管理器初始化")
	}

	opStore.LogInfo("[INIT] 初始化响应式引擎...")
	reactiveEng := reactive.NewEngine(100)
	opStore.LogInfo("[INIT] 响应式引擎创建完成")

	var svcMgr *service.Manager
	var rtMgr *runtime.Manager
	var projMgr *project.Manager
	var envMgr *environment.Manager
	var dlMgr *download.Manager
	var stateStore *engine.StateStore
	var reconciler *engine.ReconcilerImpl

	if installDir != "" {
		opStore.LogInfo("[INIT] 初始化服务管理器...")
		svcMgr = service.NewManager(cfg, procMgr, resMgr, reactiveEng)
		services := svcMgr.List()
		opStore.LogInfo("[INIT] 服务管理器初始化完成, 已注册服务数: %d", len(services))

		// 加载 Procfile 到编排引擎
		procfilePath := filepath.Join(installDir, "Procfile")
		if _, err := os.Stat(procfilePath); err == nil {
			opStore.LogInfo("[INIT] 加载 Procfile: %s", procfilePath)
			if err := svcMgr.LoadProcfile(procfilePath); err != nil {
				opStore.LogWarn("[INIT] 加载 Procfile 失败: %v", err)
			} else {
				opStore.LogInfo("[INIT] Procfile 加载成功")
			}
		} else {
			opStore.LogInfo("[INIT] Procfile 不存在，跳过加载")
		}

		opStore.LogInfo("[INIT] 初始化运行时管理器...")
		rtMgr = runtime.NewManager(cfg, procMgr, resMgr)
		runtimes := rtMgr.List()
		opStore.LogInfo("[INIT] 运行时管理器初始化完成, 已注册运行时数: %d", len(runtimes))

		opStore.LogInfo("[INIT] 初始化项目管理器...")
		projMgr = project.NewManager(installDir, "1.26.0", rtMgr, resMgr)
		projMgr.SetServiceManager(svcMgr)
		projMgr.SetRuntimeManager(rtMgr)
		projects := projMgr.List()
		opStore.LogInfo("[INIT] 项目管理器初始化完成, 项目数: %d", len(projects))

		opStore.LogInfo("[INIT] 初始化环境管理器 (Environment Profiles)...")
		envMgr = environment.NewManager(installDir)
		opStore.LogInfo("[INIT] 环境管理器初始化完成, 内置预设已注册")

		opStore.LogInfo("[INIT] 初始化下载管理器...")
		dlMgr = download.NewManager(installDir)
		opStore.LogInfo("[INIT] 下载管理器初始化完成")

		opStore.LogInfo("[INIT] 初始化状态存储...")
		dataDir := filepath.Join(installDir, "config")
		stateStore = engine.NewStateStore(dataDir)
		opStore.LogInfo("[INIT] 状态存储初始化完成, 数据目录: %s", dataDir)

		opStore.LogInfo("[INIT] 初始化协调器(Reconciler)...")
		reconciler = engine.NewReconciler(stateStore, svcMgr, projMgr, rtMgr)
		opStore.LogInfo("[INIT] 协调器初始化完成")

		opStore.LogInfo("[INIT] 注册服务提供者...")
		provider.NewNginxProvider(cfg, procMgr, resMgr)
		provider.NewMySQLProvider(cfg, procMgr)
		provider.NewRedisProvider(cfg, procMgr)
		provider.NewPHPProvider(cfg, procMgr, resMgr)
		opStore.LogInfo("[INIT] 服务提供者注册完成")
	} else {
		opStore.LogInfo("[INIT] 安装目录为空，跳过服务/运行时/项目/环境/下载/状态/协调器初始化")
		// 创建空的管理器实例，避免 nil 指针
		svcMgr = service.NewManager(cfg, procMgr, nil, reactiveEng)
		rtMgr = runtime.NewManager(cfg, procMgr, nil)
		projMgr = project.NewManager("", "1.26.0", rtMgr, nil)
		envMgr = environment.NewManager("")
		dlMgr = download.NewManager("")
		stateStore = engine.NewStateStore("")
		reconciler = engine.NewReconciler(stateStore, svcMgr, projMgr, rtMgr)
	}

	opStore.LogInfo("[INIT] 初始化日志监视器...")
	logWatcher := log.NewLogWatcher()
	opStore.LogInfo("[INIT] 日志监视器初始化完成")

	opStore.LogInfo("[INIT] 注册内置响应式规则...")
	reactive.RegisterBuiltinRules(reactiveEng,
		func(serviceID string) error {
			if strings.HasPrefix(serviceID, "php@") {
				return rtMgr.Start(serviceID)
			}
			return svcMgr.Start(serviceID)
		},
		func(owner string, oldPort int) (int, error) {
			if resMgr != nil {
				resMgr.Ports().Release(oldPort, owner)
				return resMgr.Ports().Allocate(owner, 0)
			}
			return 0, fmt.Errorf("资源管理器未初始化")
		},
		func(eventType reactive.EventType, source string, data map[string]interface{}) {
			opStore.Add("告警", string(eventType), source, fmt.Sprintf("%v", data))
		},
	)
	reactiveEng.Start()
	opStore.LogInfo("[INIT] 响应式引擎启动完成")

	opStore.LogInfo("[INIT] 初始化系统托盘...")
	app := &App{
		cfg:         cfg,
		procMgr:     procMgr,
		resMgr:      resMgr,
		svcMgr:      svcMgr,
		rtMgr:       rtMgr,
		projMgr:     projMgr,
		envMgr:      envMgr,
		logWatcher:  logWatcher,
		dlMgr:       dlMgr,
		opStore:     opStore,
		stateStore:  stateStore,
		reconciler:  reconciler,
		reactiveEng: reactiveEng,
		AppVersion:  "0.2.0",
		downloads:   map[string]struct{}{},
	}
	app.tray = tray.New(
		func() {
			if app.ctx == nil {
				return
			}
			wailsruntime.WindowShow(app.ctx)
			wailsruntime.WindowUnminimise(app.ctx)
		},
		func() {
			if app.ctx != nil {
				wailsruntime.Quit(app.ctx)
			}
			app.tray.Quit()
		},
	)
	opStore.LogInfo("[INIT] 系统托盘初始化完成")

	elapsed := time.Since(startTime)
	opStore.LogInfo("[INIT] NewApp 初始化完成, 耗时: %s", elapsed.String())
	opStore.LogInfo("[INIT] 应用版本: 0.2.0")

	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	appCtx = ctx
	if a.opStore != nil {
		a.opStore.LogInfo("[STARTUP] Wails 应用启动完成, context 已设置")
		a.opStore.LogDebug("[STARTUP] 当前安装目录: %s", a.cfg.Get().InstallDir)
	}

	// ── 开机自启状态与注册表对齐 ──
	if a.opStore != nil {
		settings := a.cfg.Get()
		if err := autostart.Sync(settings.StartOnBoot); err != nil {
			a.opStore.LogWarn("[STARTUP] 开机自启状态同步失败: %v", err)
		} else if settings.StartOnBoot != autostart.IsEnabled() {
			a.opStore.LogWarn("[STARTUP] 开机自启状态未对齐, 配置: %v, 注册表: %v", settings.StartOnBoot, autostart.IsEnabled())
		} else {
			a.opStore.LogInfo("[STARTUP] 开机自启状态已同步: %v", settings.StartOnBoot)
		}
	}

	// ── 系统托盘 ──
	if a.tray != nil {
		a.tray.SetContext(ctx)
		// 仅在启用"最小化到系统托盘"时创建图标，未启用则不占用通知区域。
		if a.cfg.Get().MinimizeToTray {
			a.tray.Start(trayIcon)
			if a.opStore != nil {
				a.opStore.LogInfo("[STARTUP] 系统托盘已启动")
			}
		} else if a.opStore != nil {
			a.opStore.LogInfo("[STARTUP] 未启用最小化到托盘, 跳过托盘启动")
		}
	}

	// 环境编排进度 → 前端（全屏操作进度）
	if a.reconciler != nil {
		a.reconciler.SetReporter(a)
	}

	// ── Bridge reactive engine events → Wails frontend events ──
	if a.reactiveEng != nil {
		eventTypes := []reactive.EventType{
			reactive.EventServiceStarted,
			reactive.EventServiceStopped,
			reactive.EventServiceStarting,
			reactive.EventServiceStopping,
			reactive.EventServiceCrashed,
			reactive.EventServiceHealthFailed,
			reactive.EventServiceRestarted,
			reactive.EventServiceError,
			reactive.EventPortConflict,
			reactive.EventConfigChanged,
			reactive.EventConfigReloaded,
			reactive.EventResourceAllocated,
			reactive.EventResourceReleased,
			reactive.EventProjectCreated,
			reactive.EventProjectDeleted,
			reactive.EventProjectEnabled,
			reactive.EventProjectDisabled,
		}
		for _, et := range eventTypes {
			etCopy := et
			a.reactiveEng.Subscribe(et, func(event *reactive.Event) {
				wailsruntime.EventsEmit(ctx, "reactive:"+string(etCopy), map[string]interface{}{
					"id":        event.ID,
					"type":      string(event.Type),
					"source":    event.Source,
					"timestamp": event.Timestamp,
					"data":      event.Data,
				})
			})
		}
		a.opStore.LogInfo("[STARTUP] 响应式事件桥接已启动, 注册 %d 个事件类型", len(eventTypes))
	}
}

// ==================== Environment APIs ====================

// HandleWindowClose 返回是否阻止窗口关闭：启用"最小化到系统托盘"时隐藏窗口而不是退出。
func (a *App) HandleWindowClose(ctx context.Context) bool {
	settings := a.cfg.Get()
	if settings.MinimizeToTray && a.tray != nil && a.tray.Running() {
		wailsruntime.WindowHide(ctx)
		if a.opStore != nil {
			a.opStore.LogInfo("[TRAY] 窗口关闭, 最小化到系统托盘")
		}
		return true
	}
	return false
}

// Shutdown 应用退出前清理托盘图标。
func (a *App) Shutdown() {
	if a.tray != nil {
		a.tray.Quit()
	}
}

// ── 环境编排进度事件 → 前端（operation:begin / operation:step / operation:end） ──

func (a *App) OnOperationBegin(action, title string, steps []engine.StepInfo) {
	wailsruntime.EventsEmit(a.ctx, "operation:begin", map[string]interface{}{
		"action": action,
		"title":  title,
		"steps":  steps,
	})
}

func (a *App) OnOperationStep(step engine.StepInfo) {
	wailsruntime.EventsEmit(a.ctx, "operation:step", step)
}

func (a *App) OnOperationEnd(action string, ok bool, message string) {
	wailsruntime.EventsEmit(a.ctx, "operation:end", map[string]interface{}{
		"action":  action,
		"ok":      ok,
		"message": message,
	})
}

// ActivateEnvironment 激活指定环境：停旧的全部，启新的全部。
// 全程在 envOpMu 内串行，配合 reconciler 的互斥锁，杜绝并发激活导致进程堆积。
func (a *App) ActivateEnvironment(envID string) error {
	a.envOpMu.Lock()
	defer a.envOpMu.Unlock()

	if a.opStore != nil {
		a.opStore.LogInfo("[API] ActivateEnvironment 调用, envID: %q", envID)
	}
	if a.envMgr == nil {
		return fmt.Errorf("环境管理器未初始化")
	}
	target, err := a.envMgr.Get(envID)
	if err != nil {
		return fmt.Errorf("环境 %q 未找到: %w", envID, err)
	}

	var oldEnv environment.Environment
	oldActiveID, err := a.getActiveEnvironmentID()
	if err != nil {
		return fmt.Errorf("读取当前激活环境失败: %w", err)
	}
	// 同一环境重复激活（如修改 units 版本后再次启动）也要先停旧实例：
	// 否则按旧版本运行的组件无人清理，新版本组件会因端口被占而启动失败。
	if oldActiveID != "" {
		if e, err := a.envMgr.Get(oldActiveID); err == nil {
			oldEnv = e
			if oldActiveID != envID {
				if a.opStore != nil {
					a.opStore.LogInfo("[API] 停止旧环境 %q", oldActiveID)
				}
			}
		} else if oldActiveID != envID {
			return fmt.Errorf("环境 %q 未找到: %w", oldActiveID, err)
		}
	}

	if a.opStore != nil {
		a.opStore.LogInfo("[API] 启动新环境 %q, 包含 %d 个组件", envID, len(target.Units))
	}
	return a.reconciler.Activate(oldEnv, target)
}

// ActivateEnvironmentOnly 仅激活环境：切换 activeEnvironmentId 标记，不启动任何组件。
func (a *App) ActivateEnvironmentOnly(envID string) error {
	a.envOpMu.Lock()
	defer a.envOpMu.Unlock()

	if a.opStore != nil {
		a.opStore.LogInfo("[API] ActivateEnvironmentOnly 调用, envID: %q", envID)
	}
	if a.envMgr == nil {
		return fmt.Errorf("环境管理器未初始化")
	}
	if _, err := a.envMgr.Get(envID); err != nil {
		return fmt.Errorf("环境 %q 未找到: %w", envID, err)
	}
	return a.reconciler.ActivateOnly(envID)
}

// RunEnvironment 运行环境：启动环境内所有组件，不修改激活标记。
func (a *App) RunEnvironment(envID string) error {
	a.envOpMu.Lock()
	defer a.envOpMu.Unlock()

	if a.opStore != nil {
		a.opStore.LogInfo("[API] RunEnvironment 调用, envID: %q", envID)
	}
	if a.envMgr == nil {
		return fmt.Errorf("环境管理器未初始化")
	}
	target, err := a.envMgr.Get(envID)
	if err != nil {
		return fmt.Errorf("环境 %q 未找到: %w", envID, err)
	}
	return a.reconciler.Run(target)
}

// DeactivateEnvironment 停止当前激活的环境。
func (a *App) DeactivateEnvironment() error {
	a.envOpMu.Lock()
	defer a.envOpMu.Unlock()

	if a.opStore != nil {
		a.opStore.LogInfo("[API] DeactivateEnvironment 调用")
	}
	activeID, err := a.getActiveEnvironmentID()
	if err != nil {
		return fmt.Errorf("读取当前激活环境失败: %w", err)
	}
	if activeID == "" {
		return nil
	}
	env, err := a.envMgr.Get(activeID)
	if err != nil {
		return fmt.Errorf("环境 %q 未找到: %w", activeID, err)
	}
	return a.reconciler.Deactivate(env)
}

// ActivateEnvironmentForProject 按项目关联的环境激活。
func (a *App) ActivateEnvironmentForProject(projectID string) error {
	if a.opStore != nil {
		a.opStore.LogInfo("[API] ActivateEnvironmentForProject 调用, projectID: %q", projectID)
	}
	if a.projMgr == nil {
		return fmt.Errorf("项目管理器未初始化")
	}
	proj, err := a.projMgr.Get(projectID)
	if err != nil {
		return err
	}
	if proj.EnvironmentID != "" {
		return a.ActivateEnvironment(proj.EnvironmentID)
	}
	return fmt.Errorf("项目 %q 未关联环境", projectID)
}

func (a *App) getActiveEnvironmentID() (string, error) {
	state, err := a.stateStore.Load()
	if err != nil {
		return "", err
	}
	return state.ActiveEnvironmentID, nil
}

// ==================== Environment CRUD APIs ====================

type EnvironmentWithUsage struct {
	environment.Environment
	ProjectCount int `json:"projectCount"`
}

func (a *App) environmentProjectUsage(envID string) int {
	if a.projMgr == nil {
		return 0
	}
	cnt := 0
	for _, proj := range a.projMgr.List() {
		if proj.EnvironmentID == envID {
			cnt++
		}
	}
	return cnt
}

func (a *App) ListEnvironments() ([]EnvironmentWithUsage, error) {
	if a.envMgr == nil {
		return nil, nil
	}
	raw := a.envMgr.List()
	result := make([]EnvironmentWithUsage, 0, len(raw))
	for _, e := range raw {
		result = append(result, EnvironmentWithUsage{
			Environment:  e,
			ProjectCount: a.environmentProjectUsage(e.ID),
		})
	}
	return result, nil
}

func (a *App) GetEnvironment(id string) (EnvironmentWithUsage, error) {
	if a.envMgr == nil {
		return EnvironmentWithUsage{}, core.ErrNotFound
	}
	e, err := a.envMgr.Get(id)
	if err != nil {
		return EnvironmentWithUsage{}, err
	}
	return EnvironmentWithUsage{
		Environment:  e,
		ProjectCount: a.environmentProjectUsage(id),
	}, nil
}

// GetEnvironmentState 返回当前运行状态快照（服务/项目/运行时 + 当前激活环境），
// 供前端环境页与仪表盘读取，替代不再适用的 GetEnvironment 模板形状。
func (a *App) GetEnvironmentState() engine.EnvironmentState {
	state := engine.EnvironmentState{
		Services: make(map[string]engine.ServiceDesiredState),
		Projects: make(map[string]engine.ProjectDesiredState),
		Runtimes: make(map[string]engine.RuntimeDesiredState),
	}
	activeID, err := a.getActiveEnvironmentID()
	if err != nil && a.opStore != nil {
		a.opStore.LogError("[API] GetEnvironmentState 读取激活环境失败: %v", err)
	}
	state.ActiveProfileID = activeID
	state.ActiveEnvironmentID = activeID

	if a.svcMgr != nil {
		for _, s := range a.svcMgr.List() {
			state.Services[s.ID] = engine.ServiceDesiredState{
				Version: s.Version,
				State:   s.Status,
			}
		}
	}
	if a.rtMgr != nil {
		for _, rt := range a.rtMgr.List() {
			state.Runtimes[rt.ID] = engine.RuntimeDesiredState{
				Version: rt.Version,
				State:   rt.Status,
				Active:  rt.Active,
			}
		}
	}
	if a.projMgr != nil {
		for _, p := range a.projMgr.List() {
			state.Projects[p.ID] = engine.ProjectDesiredState{
				Name:          p.Name,
				Type:          p.Type,
				Root:          p.Root,
				Domain:        p.Domain,
				Port:          p.Port,
				HTTPSPort:     p.HTTPSPort,
				SSL:           p.SSL,
				EnvironmentID: p.EnvironmentID,
				PHPVersion:    p.PHPVersion,
				MySQLVersion:  p.MySQLVersion,
				NginxVersion:  p.NginxVersion,
				NodeVersion:   p.NodeVersion,
				PythonVersion: p.PythonVersion,
				ProxyPass:     p.ProxyPass,
				TargetPort:    p.TargetPort,
				Rewrite:       p.Rewrite,
				Notes:         p.Notes,
				Enabled:       p.Enabled,
				Dependencies:  p.Dependencies,
			}
		}
	}
	return state
}

func (a *App) CreateEnvironment(env environment.Environment) (environment.Environment, error) {
	if a.envMgr == nil {
		return environment.Environment{}, fmt.Errorf("环境管理器未初始化")
	}
	return a.envMgr.Create(env)
}

func (a *App) UpdateEnvironment(env environment.Environment) (environment.Environment, error) {
	if a.envMgr == nil {
		return environment.Environment{}, fmt.Errorf("环境管理器未初始化")
	}
	return a.envMgr.Update(env)
}

func (a *App) DeleteEnvironment(id string) error {
	if a.envMgr == nil {
		return nil
	}
	if a.environmentProjectUsage(id) > 0 {
		return core.ErrEnvProfileInUse
	}
	return a.envMgr.Delete(id)
}

// SaveProjectAsEnvironment 以项目当前配置为模板，保存为一个自定义环境
type SaveProjectAsEnvironmentArgs struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (a *App) SaveProjectAsEnvironment(args SaveProjectAsEnvironmentArgs) (environment.Environment, error) {
	if a.envMgr == nil {
		return environment.Environment{}, fmt.Errorf("环境管理器未初始化")
	}
	if a.projMgr == nil {
		return environment.Environment{}, fmt.Errorf("项目管理器未初始化")
	}
	proj, err := a.projMgr.Get(args.ProjectID)
	if err != nil {
		return environment.Environment{}, err
	}

	units := make([]environment.UnitSpec, 0)
	if proj.NginxVersion != "" {
		units = append(units, environment.UnitSpec{Kind: "nginx", Version: proj.NginxVersion, Name: "Nginx"})
	}
	if proj.MySQLVersion != "" {
		units = append(units, environment.UnitSpec{Kind: "mysql", Version: proj.MySQLVersion, Name: "MySQL"})
	}
	if proj.PHPVersion != "" {
		units = append(units, environment.UnitSpec{Kind: "php", Version: proj.PHPVersion, Name: "PHP"})
	}
	if proj.NodeVersion != "" {
		units = append(units, environment.UnitSpec{Kind: "node", Version: proj.NodeVersion, Name: "Node.js"})
	}
	if proj.PythonVersion != "" {
		units = append(units, environment.UnitSpec{Kind: "python", Version: proj.PythonVersion, Name: "Python"})
	}

	name := args.Name
	if name == "" {
		name = proj.Name + " 的环境"
	}
	env := environment.Environment{
		Name:        name,
		Description: args.Description,
		Units:       units,
	}
	return a.envMgr.Create(env)
}

func isRuntime(kind string) bool {
	return kind == "php" || kind == "node" || kind == "python"
}

// 保留旧 API 别名，兼容前端
func (a *App) ListEnvironmentProfiles() ([]EnvironmentWithUsage, error) { return a.ListEnvironments() }
func (a *App) GetEnvironmentProfile(id string) (EnvironmentWithUsage, error) { return a.GetEnvironment(id) }
func (a *App) CreateEnvironmentProfile(env environment.Environment) (environment.Environment, error) { return a.CreateEnvironment(env) }
func (a *App) UpdateEnvironmentProfile(env environment.Environment) (environment.Environment, error) { return a.UpdateEnvironment(env) }
func (a *App) DeleteEnvironmentProfile(id string) error { return a.DeleteEnvironment(id) }



// ==================== Service APIs ====================

func (a *App) GetServices() []core.ServiceInfo {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetServices 调用")
	}
	result := a.svcMgr.List()
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetServices 返回, 数量: %d", len(result))
	}
	return result
}

func (a *App) StartService(id string) error {
	err := a.svcMgr.Start(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("启动", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("启动", name, "", "成功")
	}
	return err
}

func (a *App) StopService(id string) error {
	err := a.svcMgr.Stop(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("停止", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("停止", name, "", "成功")
	}
	return err
}

func (a *App) RestartService(id string) error {
	err := a.svcMgr.Restart(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("重启", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("重启", name, "", "成功")
	}
	return err
}

func (a *App) SetServiceVersion(id, version string) error {
	return a.svcMgr.SetVersion(id, version)
}

func (a *App) UninstallService(id string) error {
	return a.svcMgr.Uninstall(id)
}

func (a *App) CheckVersionInstalled(serviceType, version string) bool {
	return a.svcMgr.CheckVersionInstalled(serviceType, version)
}

func (a *App) UninstallVersion(serviceType, version string) error {
	return a.svcMgr.UninstallVersion(serviceType, version)
}

func (a *App) ConfigureService(serviceType string) error {
	return a.svcMgr.Configure(serviceType)
}

func (a *App) SaveServiceConfig(serviceType string, values map[string]string) error {
	return a.svcMgr.SaveConfig(serviceType, values)
}

func (a *App) GetServiceConfig(serviceType string) map[string]string {
	settings := a.cfg.Get()
	result := make(map[string]string)

	switch serviceType {
	case "nginx":
		nc := settings.NginxConfig
		result["workerConnections"] = fmt.Sprintf("%d", nc.WorkerConnections)
		result["clientMaxBodySize"] = nc.ClientMaxBodySize
		result["gzipEnabled"] = fmt.Sprintf("%v", nc.GzipEnabled)
		result["staticCacheDays"] = fmt.Sprintf("%d", nc.StaticCacheDays)
		result["keepaliveTimeout"] = fmt.Sprintf("%d", nc.KeepaliveTimeout)
		result["serverTokens"] = fmt.Sprintf("%v", nc.ServerTokens)
	case "php":
		pc := settings.PHPConfig
		result["memoryLimit"] = pc.MemoryLimit
		result["maxExecutionTime"] = fmt.Sprintf("%d", pc.MaxExecutionTime)
		result["uploadMaxFilesize"] = pc.UploadMaxFilesize
		result["postMaxSize"] = pc.PostMaxSize
		result["displayErrors"] = fmt.Sprintf("%v", pc.DisplayErrors)
		result["opcacheEnabled"] = fmt.Sprintf("%v", pc.OPcacheEnabled)
		result["realpathCacheSize"] = pc.RealpathCacheSize
		result["maxInputTime"] = fmt.Sprintf("%d", pc.MaxInputTime)
		result["maxInputVars"] = fmt.Sprintf("%d", pc.MaxInputVars)
		result["maxFileUploads"] = fmt.Sprintf("%d", pc.MaxFileUploads)
		result["timezone"] = pc.Timezone
		result["defaultCharset"] = pc.DefaultCharset
		result["fileUploads"] = fmt.Sprintf("%v", pc.FileUploads)
		result["sessionGCMaxLifetime"] = fmt.Sprintf("%d", pc.SessionGCMaxLifetime)
		result["logErrors"] = fmt.Sprintf("%v", pc.LogErrors)
		result["errorLog"] = pc.ErrorLog
		result["fixPathinfo"] = fmt.Sprintf("%v", pc.FixPathinfo)
	case "mysql":
		mc := settings.MySQLConfig
		result["innodbBufferPoolSize"] = mc.InnoDBBufferPoolSize
		result["innodbLogFileSize"] = mc.InnoDBLogFileSize
		result["innodbFlushLogAtTrxCommit"] = fmt.Sprintf("%d", mc.InnoDBFlushLogAtTrxCommit)
		result["maxConnections"] = fmt.Sprintf("%d", mc.MaxConnections)
		result["tmpTableSize"] = mc.TmpTableSize
		result["maxHeapTableSize"] = mc.MaxHeapTableSize
		result["threadCacheSize"] = fmt.Sprintf("%d", mc.ThreadCacheSize)
		result["tableOpenCache"] = fmt.Sprintf("%d", mc.TableOpenCache)
		result["bindAddress"] = mc.BindAddress
		result["port"] = fmt.Sprintf("%d", mc.Port)
		result["characterSet"] = mc.CharacterSet
		result["collation"] = mc.Collation
		result["sqlMode"] = mc.SQLMode
		result["skipNameResolve"] = fmt.Sprintf("%v", mc.SkipNameResolve)
		result["generalLog"] = fmt.Sprintf("%v", mc.GeneralLog)
		result["slowQueryLog"] = fmt.Sprintf("%v", mc.SlowQueryLog)
		result["longQueryTime"] = fmt.Sprintf("%d", mc.LongQueryTime)
		result["maxAllowedPacket"] = mc.MaxAllowedPacket
		result["keyBufferSize"] = mc.KeyBufferSize
		result["waitTimeout"] = fmt.Sprintf("%d", mc.WaitTimeout)
		result["sortBufferSize"] = mc.SortBufferSize
		result["joinBufferSize"] = mc.JoinBufferSize
	}

	return result
}

func (a *App) ApplyServiceConfig(serviceType string) error {
	if err := a.svcMgr.Configure(serviceType); err != nil {
		return err
	}
	// Reload nginx if it's running
	if serviceType == "nginx" {
		if svc := a.svcMgr.GetService("nginx"); svc != nil && svc.Info.Status == core.StatusRunning {
			return a.svcMgr.Restart("nginx")
		}
	}
	return nil
}

// ==================== Runtime APIs ====================

func (a *App) GetRuntimes() []core.RuntimeInfo {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetRuntimes 调用")
	}
	result := a.rtMgr.List()
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetRuntimes 返回, 数量: %d", len(result))
	}
	return result
}

func (a *App) StartRuntime(id string) error {
	if a.opStore != nil {
		a.opStore.LogInfo("[API] StartRuntime 调用, id: %q", id)
	}
	name := runtimeNames[id]
	if name == "" {
		name = id
	}
	if err := a.rtMgr.Start(id); err != nil {
		if a.opStore != nil {
			a.opStore.Add("启动", name, "", "失败: "+err.Error())
		}
		return err
	}
	if a.opStore != nil {
		a.opStore.Add("启动", name, "", "成功")
	}
	return nil
}

func (a *App) StopRuntime(id string) error {
	if a.opStore != nil {
		a.opStore.LogInfo("[API] StopRuntime 调用, id: %q", id)
	}
	name := runtimeNames[id]
	if name == "" {
		name = id
	}
	if err := a.rtMgr.Stop(id); err != nil {
		if a.opStore != nil {
			a.opStore.Add("停止", name, "", "失败: "+err.Error())
		}
		return err
	}
	if a.opStore != nil {
		a.opStore.Add("停止", name, "", "成功")
	}
	return nil
}

func (a *App) GetPHPRegistry() []runtime.PHPVersionInfo {
	return a.rtMgr.GetPHPRegistryVersions()
}

func (a *App) RefreshPHPRegistry() []runtime.PHPVersionInfo {
	return a.rtMgr.RefreshPHPRegistry()
}

func (a *App) GetPHPExtensions(version string) ([]runtime.PHPExtensionInfo, error) {
	return a.rtMgr.GetExtensions(version)
}

func (a *App) SavePHPExtensions(version string, extensions []string) error {
	return a.rtMgr.SaveExtensions(version, extensions)
}

// hydrateProjectVersions 当项目引用了 Environment 且自身版本字段为空时，从 Environment 的 Unit 列表中填充
func (a *App) hydrateProjectVersions(p *core.Project) {
	if p == nil || p.EnvironmentID == "" || a.envMgr == nil {
		return
	}
	env, err := a.envMgr.Get(p.EnvironmentID)
	if err != nil {
		return
	}
	for _, u := range env.Units {
		if p.PHPVersion == "" && u.Kind == "php" {
			p.PHPVersion = u.Version
		}
		if p.NodeVersion == "" && u.Kind == "node" {
			p.NodeVersion = u.Version
		}
		if p.PythonVersion == "" && u.Kind == "python" {
			p.PythonVersion = u.Version
		}
		if p.NginxVersion == "" && u.Kind == "nginx" {
			p.NginxVersion = u.Version
		}
		if p.MySQLVersion == "" && u.Kind == "mysql" {
			p.MySQLVersion = u.Version
		}
	}
}

// ==================== Project APIs ====================

func (a *App) GetProjects() []core.Project {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetProjects 调用")
	}
	result := a.projMgr.List()
	for i := range result {
		a.hydrateProjectVersions(&result[i])
	}
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetProjects 返回, 数量: %d", len(result))
	}
	return result
}

func (a *App) CreateProject(p core.Project) (*core.Project, error) {
	a.hydrateProjectVersions(&p)
	result, err := a.projMgr.Create(p)
	if err != nil {
		a.opStore.Add("创建项目", p.Name, p.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("创建项目", p.Name, p.Domain, "成功")
	}
	return result, err
}

func (a *App) UpdateProject(p core.Project) error {
	a.hydrateProjectVersions(&p)
	err := a.projMgr.Update(p)
	if err != nil {
		a.opStore.Add("更新项目", p.Name, p.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("更新项目", p.Name, p.Domain, "成功")
	}
	return err
}

func (a *App) DeleteProject(id string) error {
	projects := a.projMgr.List()
	var name, domain string
	for _, p := range projects {
		if p.ID == id {
			name, domain = p.Name, p.Domain
			break
		}
	}
	err := a.projMgr.Delete(id)
	if err != nil {
		a.opStore.Add("删除项目", name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("删除项目", name, domain, "成功")
	}
	return err
}

func (a *App) ToggleProject(id string) error {
	projects := a.projMgr.List()
	var name, domain string
	var enabled bool
	for _, p := range projects {
		if p.ID == id {
			name, domain, enabled = p.Name, p.Domain, p.Enabled
			break
		}
	}
	err := a.projMgr.Toggle(id)
	action := "禁用项目"
	if !enabled {
		action = "启用项目"
	}
	if err != nil {
		a.opStore.Add(action, name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add(action, name, domain, "成功")
	}
	return err
}

// ==================== Settings APIs ====================

func (a *App) GetRecentOps(n int) []log.OpEntry {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetRecentOps 调用, n: %d", n)
	}
	result := a.opStore.Recent(n)
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetRecentOps 返回, 数量: %d", len(result))
	}
	return result
}

func (a *App) GetLogFiles() ([]string, error) {
	if a.opStore == nil {
		return nil, fmt.Errorf("日志存储未初始化")
	}
	return a.opStore.GetLogFiles()
}

func (a *App) ReadLogFile(path string) ([]string, error) {
	if a.opStore == nil {
		return nil, fmt.Errorf("日志存储未初始化")
	}
	return a.opStore.ReadLogFile(path)
}

func (a *App) GetRecentLogLines(count int) ([]string, error) {
	if a.opStore == nil {
		return nil, fmt.Errorf("日志存储未初始化")
	}
	return a.opStore.ReadRecentLines(count)
}

// LogToast 将前端 toast 消息写入操作日志与日志文件，保证所有可见消息可被查询。
func (a *App) LogToast(message, toastType string) {
	if a.opStore == nil || message == "" {
		return
	}
	status := "成功"
	switch toastType {
	case "error":
		status = "失败"
	case "warning":
		status = "警告"
	case "info":
		status = "提示"
	}
	a.opStore.Add("前端提示", "", message, status)
}

// ClearLogs 清空操作记录缓冲与当前日志文件。
func (a *App) ClearLogs() error {
	if a.opStore == nil {
		return nil
	}
	return a.opStore.Clear()
}

func (a *App) GetSettings() core.AppSettings {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSettings 调用")
	}
	result := a.cfg.Get()
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSettings 返回, installDir: %s, theme: %s", result.InstallDir, result.Theme)
	}
	return result
}

func (a *App) SaveSettings(s core.AppSettings) error {
	old := a.cfg.Get()
	if old.StartOnBoot != s.StartOnBoot {
		if s.StartOnBoot {
			if err := autostart.Enable(); err != nil {
				a.opStore.Add("设置", "开机自启", "", "启用失败: "+err.Error())
				return fmt.Errorf("启用开机自启失败: %w", err)
			}
			a.opStore.Add("设置", "开机自启", "", "已启用")
		} else {
			if err := autostart.Disable(); err != nil {
				a.opStore.Add("设置", "开机自启", "", "关闭失败: "+err.Error())
				return fmt.Errorf("关闭开机自启失败: %w", err)
			}
			a.opStore.Add("设置", "开机自启", "", "已关闭")
		}
	}
	if err := a.cfg.Update(s); err != nil {
		return err
	}
	a.syncTray(s.MinimizeToTray)
	return nil
}

// syncTray 使托盘状态与"最小化到系统托盘"设置保持一致：
// 启用时确保托盘已启动；禁用时移除图标并停止消息循环。
func (a *App) syncTray(enable bool) {
	if a.tray == nil {
		return
	}
	switch {
	case enable && !a.tray.Running():
		a.tray.Start(trayIcon)
		a.opStore.Add("设置", "系统托盘", "", "已启用托盘图标")
	case !enable && a.tray.Running():
		a.tray.Quit()
		a.opStore.Add("设置", "系统托盘", "", "已移除托盘图标")
	}
}

// GetConfigPath 返回配置文件的绝对路径，便于排查保存问题
func (a *App) GetConfigPath() string {
	return a.cfg.FilePath()
}

// ==================== MySQL APIs ====================

func (a *App) CheckMySQLRootAuth() (bool, error) {
	return a.svcMgr.CheckMySQLRootAuth()
}

// ==================== Download APIs ====================

func (a *App) GetAvailableVersions(serviceType string) ([]download.ReleaseInfo, error) {
	return a.dlMgr.GetAvailableVersions(serviceType)
}

func (a *App) GetInstalledVersions(serviceType string) []string {
	return a.svcMgr.GetInstalledVersions(serviceType)
}

func (a *App) DownloadService(serviceType, version, url string) error {
	if !isSafePathComponent(serviceType) || !isSafePathComponent(version) {
		return fmt.Errorf("非法的服务类型或版本号")
	}

	installDir := a.cfg.Get().InstallDir
	if installDir == "" {
		return fmt.Errorf("安装目录未设置，请先完成初始化设置")
	}

	// 已安装版本直接跳过下载
	for _, v := range a.svcMgr.GetInstalledVersions(serviceType) {
		if v == version {
			return nil
		}
	}

	// 同版本并发下载去重：单飞
	dedupKey := serviceType + "@" + version
	a.downloadMu.Lock()
	if _, dup := a.downloads[dedupKey]; dup {
		a.downloadMu.Unlock()
		return fmt.Errorf("%s %s 正在下载中，请勿重复操作", serviceType, version)
	}
	a.downloads[dedupKey] = struct{}{}
	a.downloadMu.Unlock()
	defer func() {
		a.downloadMu.Lock()
		delete(a.downloads, dedupKey)
		a.downloadMu.Unlock()
	}()

	var urls []string
	var checksum string
	if url != "" {
		urls = append(urls, url)
	}
	releases, err := a.dlMgr.GetAvailableVersions(serviceType)
	if err != nil {
		return fmt.Errorf("获取版本列表失败: %w", err)
	}
	for _, r := range releases {
		if r.Version == version {
			if url == "" && r.URL != "" {
				urls = append(urls, r.URL)
			}
			if r.MirrorURL != "" {
				urls = append(urls, r.MirrorURL)
			}
			checksum = r.Checksum
			break
		}
	}
	if len(urls) == 0 {
		return fmt.Errorf("未找到 %s %s 的下载地址", serviceType, version)
	}

	destDir := joinPath(installDir, "programs", serviceType, version)

	progressKey := fmt.Sprintf("download:%s:%s", serviceType, version)
	progressFn := func(downloaded, total int64) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "download:progress", map[string]interface{}{
				"key":        progressKey,
				"service":    serviceType,
				"version":    version,
				"downloaded": downloaded,
				"total":      total,
			})
		}
	}

	if err := a.dlMgr.TryDownloadAndExtract(progressKey, urls, destDir, progressFn, checksum); err != nil {
		name := serviceNames[serviceType]
		if name == "" {
			name = runtimeNames[serviceType]
		}
		a.opStore.Add("下载", name, version, "失败: "+err.Error())
		return err
	}

	if svcNames, ok := serviceNames[serviceType]; ok && svcNames != "" {
		a.svcMgr.MarkInstalled(serviceType, version)
		a.opStore.Add("下载", serviceNames[serviceType], version, "成功")
		return a.svcMgr.Configure(serviceType + "@" + version)
	}

	a.opStore.Add("下载", runtimeNames[serviceType], version, "成功")
	return nil
}

// CancelDownload 中止指定 key 的下载任务（key 形如 "download:nginx:1.24.0"）。
func (a *App) CancelDownload(key string) {
	a.dlMgr.Cancel(key)
}

// ==================== Provider APIs ====================

type ProviderInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

func (a *App) GetProviders() []ProviderInfo {
	var result []ProviderInfo
	for _, name := range provider.ListAll() {
		p := provider.Get(name)
		if p != nil {
			caps := make([]string, 0)
			for _, cap := range p.Capabilities() {
				caps = append(caps, string(cap))
			}
			result = append(result, ProviderInfo{
				Name:        p.Name(),
				Version:     p.Version(),
				Description: p.Description(),
				Capabilities: caps,
			})
		}
	}
	return result
}

func (a *App) GetProvidersByCapability(cap string) []ProviderInfo {
	var result []ProviderInfo
	capability := provider.Capability(cap)
	for _, p := range provider.ListByCapability(capability) {
		caps := make([]string, 0)
		for _, c := range p.Capabilities() {
			caps = append(caps, string(c))
		}
		result = append(result, ProviderInfo{
			Name:        p.Name(),
			Version:     p.Version(),
			Description: p.Description(),
			Capabilities: caps,
		})
	}
	return result
}

func (a *App) GetAllCapabilities() []string {
	return []string{
		string(provider.CapabilityWebServer),
		string(provider.CapabilityDatabase),
		string(provider.CapabilityFPMRuntime),
		string(provider.CapabilityCache),
		string(provider.CapabilityMessageQueue),
	}
}

// ==================== Reactive Engine APIs ====================

type EventInfo struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

type RuleInfo struct {
	Name      string `json:"name"`
	EventType string `json:"eventType"`
}

func (a *App) GetRules(eventType string) []RuleInfo {
	var result []RuleInfo
	et := reactive.EventType(eventType)
	rules := a.reactiveEng.GetRules(et)
	for _, rule := range rules {
		result = append(result, RuleInfo{
			Name:      rule.Name(),
			EventType: string(et),
		})
	}
	return result
}

func (a *App) PublishEvent(eventType, source string, data map[string]interface{}) {
	event := &reactive.Event{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      reactive.EventType(eventType),
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}
	a.reactiveEng.Publish(event)
}

// ==================== System APIs ====================

func (a *App) GetSystemInfo() core.SystemInfo {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSystemInfo 调用")
	}
	result := core.SystemInfo{
		GoVersion:  goruntime.Version(),
		Platform:   goruntime.GOOS,
		Arch:       goruntime.GOARCH,
		AppVersion: a.AppVersion,
		InstallDir: a.cfg.Get().InstallDir,
	}
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSystemInfo 返回, appVersion: %s", result.AppVersion)
	}
	return result
}

func (a *App) GetLogs(serviceID string) ([]log.LogEntry, error) {
	installDir := a.cfg.Get().InstallDir
	var logPath string

	if serviceID == "nginx" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "programs", "nginx", svc.Info.Version, "logs", "error.log")
		}
	} else if serviceID == "mysql" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "programs", "mysql", svc.Info.Version, "error.log")
		}
	} else if strings.HasPrefix(serviceID, "redis@") {
		version := strings.TrimPrefix(serviceID, "redis@")
		logPath = joinPath(installDir, "programs", "redis", version, "redis.log")
	} else if strings.HasPrefix(serviceID, "php@") {
		version := strings.TrimPrefix(serviceID, "php@")
		logPath = joinPath(installDir, "programs", "php", version, "php_errors.log")
	} else if strings.HasPrefix(serviceID, "node@") {
		version := strings.TrimPrefix(serviceID, "node@")
		logPath = joinPath(installDir, "programs", "node", version, "node.log")
	} else if strings.HasPrefix(serviceID, "python@") {
		version := strings.TrimPrefix(serviceID, "python@")
		logPath = joinPath(installDir, "programs", "python", version, "python.log")
	}
	if logPath == "" {
		return []log.LogEntry{}, nil
	}
	entries, err := a.logWatcher.ReadRecent(logPath, 100)
	if err != nil {
		return nil, fmt.Errorf("读取日志文件失败: %w", err)
	}
	return entries, nil
}

func (a *App) OpenDirectory(defaultPath string) (string, error) {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "选择目录",
		DefaultDirectory: defaultPath,
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) ShowMessage(title, message string) {
	wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Title:   title,
		Message: message,
	})
}

func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// isSafePathComponent 校验单个路径组件是否可用于拼接文件系统路径，
// 防止 .. 路径穿越与 Windows 非法字符注入。
func isSafePathComponent(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\<>:"|?*`) {
		return false
	}
	return true
}

// ==================== Setup Wizard APIs ====================

// SelectInstallDir 打开目录选择对话框
func (a *App) SelectInstallDir(defaultPath string) (string, error) {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "选择 DevStack 安装目录",
		DefaultDirectory: defaultPath,
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// PortConflict 端口冲突信息
type PortConflict struct {
	Port       int    `json:"port"`
	Service    string `json:"service"`
	InUse      bool   `json:"inUse"`
	PID        int    `json:"pid"`
	ExePath    string `json:"exePath"`
	OwnedByUs  bool   `json:"ownedByUs"`
	Message    string `json:"message"`
}

// CheckSetupPorts 检查向导阶段的默认端口冲突
func (a *App) CheckSetupPorts(services []string) []PortConflict {
	if a.opStore != nil {
		a.opStore.LogInfo("[SETUP] 检查端口冲突, services: %v", services)
	}
	defaultPorts := map[string]int{
		"nginx": 80,
		"mysql": 3306,
		"redis": 6379,
		"php":   9000,
	}
	var conflicts []PortConflict
	for _, svc := range services {
		port, ok := defaultPorts[svc]
		if !ok {
			continue
		}
		status := a.resMgr.Ports().CheckPort(port)
		conflict := PortConflict{
			Port:      port,
			Service:   svc,
			InUse:     status.InUse,
			PID:       status.PID,
			ExePath:   status.ExePath,
			OwnedByUs: status.OwnedByUs,
		}
		if status.InUse && !status.OwnedByUs {
			conflict.Message = fmt.Sprintf("端口 %d 已被外部进程占用 (PID=%d: %s)", port, status.PID, status.ExePath)
		} else if status.InUse && status.OwnedByUs {
			conflict.Message = fmt.Sprintf("端口 %d 已被 DevStack 占用 (PID=%d)", port, status.PID)
		} else {
			conflict.Message = fmt.Sprintf("端口 %d 可用", port)
		}
		conflicts = append(conflicts, conflict)
	}
	return conflicts
}

// CompleteSetupArgs 向导完成参数
type CompleteSetupArgs struct {
	InstallDir string   `json:"installDir"`
	Services   []string `json:"services"`
}

// CompleteSetup 完成向导：写配置、下载服务、创建默认环境
func (a *App) CompleteSetup(args CompleteSetupArgs) error {
	if a.opStore != nil {
		a.opStore.LogInfo("[SETUP] 完成向导, installDir: %s, services: %v", args.InstallDir, args.Services)
	}

	// 1. 再次检查端口冲突（阻断性）
	conflicts := a.CheckSetupPorts(args.Services)
	for _, c := range conflicts {
		if c.InUse && !c.OwnedByUs {
			return fmt.Errorf("端口冲突: %s", c.Message)
		}
	}

	// 2. 写入安装目录配置
	settings := a.cfg.Get()
	settings.InstallDir = args.InstallDir
	if err := a.cfg.Update(settings); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	config.EnsureDirs(args.InstallDir)

	// 3. 重新初始化依赖安装目录的管理器
	if err := a.reinitManagers(args.InstallDir); err != nil {
		return fmt.Errorf("重新初始化管理器失败: %w", err)
	}

	// 4. 下载选中服务（串行，带进度）
	for _, svc := range args.Services {
		versions, err := a.dlMgr.GetAvailableVersions(svc)
		if err != nil || len(versions) == 0 {
			if a.opStore != nil {
				a.opStore.LogWarn("[SETUP] 获取 %s 版本列表失败: %v", svc, err)
			}
			continue
		}
		latest := versions[len(versions)-1].Version
		if a.opStore != nil {
			a.opStore.LogInfo("[SETUP] 开始下载 %s %s", svc, latest)
		}
		progressKey := fmt.Sprintf("setup-download:%s:%s", svc, latest)
		progressFn := func(downloaded, total int64) {
			if a.ctx != nil {
				wailsruntime.EventsEmit(a.ctx, "setup:download-progress", map[string]interface{}{
					"key":        progressKey,
					"service":    svc,
					"version":    latest,
					"downloaded": downloaded,
					"total":      total,
				})
			}
		}
		destDir := joinPath(args.InstallDir, "programs", svc, latest)
		if err := a.dlMgr.TryDownloadAndExtract(progressKey, []string{versions[len(versions)-1].URL}, destDir, progressFn, versions[len(versions)-1].Checksum); err != nil {
			if a.opStore != nil {
				a.opStore.LogError("[SETUP] 下载 %s %s 失败: %v", svc, latest, err)
			}
			return fmt.Errorf("下载 %s %s 失败: %w", svc, latest, err)
		}
		if svcNames, ok := serviceNames[svc]; ok && svcNames != "" {
			a.svcMgr.MarkInstalled(svc, latest)
			if a.opStore != nil {
				a.opStore.Add("下载", serviceNames[svc], latest, "成功")
			}
			_ = a.svcMgr.Configure(svc + "@" + latest)
		}
	}

	// 5. 创建并激活默认环境
	units := make([]environment.UnitSpec, 0)
	for _, svc := range args.Services {
		vers := a.svcMgr.GetInstalledVersions(svc)
		if len(vers) > 0 {
			name := serviceNames[svc]
			if name == "" {
				name = runtimeNames[svc]
			}
			units = append(units, environment.UnitSpec{Kind: svc, Version: vers[0], Name: name})
		}
	}
	if len(units) > 0 {
		env := environment.Environment{
			Name:        "默认环境",
			Description: "首次运行自动创建",
			Units:       units,
		}
		created, err := a.envMgr.Create(env)
		if err != nil {
			return fmt.Errorf("创建默认环境失败: %w", err)
		}
		// 仅标记激活，不启动服务
		if err := a.reconciler.ActivateOnly(created.ID); err != nil {
			return fmt.Errorf("激活默认环境失败: %w", err)
		}
		if a.opStore != nil {
			a.opStore.LogInfo("[SETUP] 默认环境已创建并激活: %s", created.ID)
		}
	}

	if a.opStore != nil {
		a.opStore.LogInfo("[SETUP] 向导完成")
	}
	return nil
}

// reinitManagers 向导完成后重新初始化依赖安装目录的管理器
func (a *App) reinitManagers(installDir string) error {
	// 重新初始化资源管理器（端口池等）
	a.resMgr = resource.NewManager(filepath.Join(installDir, "config"))

	// 重新初始化项目管理器
	a.projMgr = project.NewManager(installDir, "1.26.0", a.rtMgr, a.resMgr)
	a.projMgr.SetServiceManager(a.svcMgr)
	a.projMgr.SetRuntimeManager(a.rtMgr)

	// 重新初始化环境管理器
	a.envMgr = environment.NewManager(installDir)

	// 重新初始化下载管理器
	a.dlMgr = download.NewManager(installDir)

	// 重新初始化状态存储
	dataDir := filepath.Join(installDir, "config")
	a.stateStore = engine.NewStateStore(dataDir)

	// 重新初始化协调器
	a.reconciler = engine.NewReconciler(a.stateStore, a.svcMgr, a.projMgr, a.rtMgr)
	a.reconciler.SetReporter(a)

	// 重新注册响应式规则（需要新的端口分配器）
	a.reactiveEng = reactive.NewEngine(100)
	reactive.RegisterBuiltinRules(a.reactiveEng,
		func(serviceID string) error {
			if strings.HasPrefix(serviceID, "php@") {
				return a.rtMgr.Start(serviceID)
			}
			return a.svcMgr.Start(serviceID)
		},
		func(owner string, oldPort int) (int, error) {
			a.resMgr.Ports().Release(oldPort, owner)
			return a.resMgr.Ports().Allocate(owner, 0)
		},
		func(eventType reactive.EventType, source string, data map[string]interface{}) {
			a.opStore.Add("告警", string(eventType), source, fmt.Sprintf("%v", data))
		},
	)
	a.reactiveEng.Start()

	// 重新订阅响应式事件到前端
	if a.ctx != nil {
		eventTypes := []reactive.EventType{
			reactive.EventServiceStarted, reactive.EventServiceStopped,
			reactive.EventServiceStarting, reactive.EventServiceStopping,
			reactive.EventServiceCrashed, reactive.EventServiceHealthFailed,
			reactive.EventServiceRestarted, reactive.EventServiceError,
			reactive.EventPortConflict, reactive.EventConfigChanged,
			reactive.EventConfigReloaded, reactive.EventResourceAllocated,
			reactive.EventResourceReleased, reactive.EventProjectCreated,
			reactive.EventProjectDeleted, reactive.EventProjectEnabled,
			reactive.EventProjectDisabled,
		}
		for _, et := range eventTypes {
			etCopy := et
			a.reactiveEng.Subscribe(et, func(event *reactive.Event) {
				wailsruntime.EventsEmit(a.ctx, "reactive:"+string(etCopy), map[string]interface{}{
					"id":        event.ID,
					"type":      string(event.Type),
					"source":    event.Source,
					"timestamp": event.Timestamp,
					"data":      event.Data,
				})
			})
		}
	}

	// 重新加载 Procfile
	procfilePath := filepath.Join(installDir, "Procfile")
	if _, err := os.Stat(procfilePath); err == nil {
		_ = a.svcMgr.LoadProcfile(procfilePath)
	}

	return nil
}

// ==================== Snapshot APIs ====================

func (a *App) GetSnapshots() ([]string, error) {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSnapshots 调用")
	}
	result, err := a.reconciler.SnapshotManager().ListSnapshots(a.ctx)
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] GetSnapshots 失败: %v", err)
		}
		return nil, err
	}
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetSnapshots 返回, 数量: %d", len(result))
	}
	return result, nil
}

func (a *App) GetSnapshotDetail(snapshotID string) (engine.EnvironmentState, error) {
	return a.reconciler.SnapshotManager().LoadSnapshot(a.ctx, snapshotID)
}

func (a *App) DeleteSnapshot(snapshotID string) error {
	return a.reconciler.SnapshotManager().DeleteSnapshot(a.ctx, snapshotID)
}

// TakeSnapshot 保存当前环境状态为快照，返回快照标识。
func (a *App) TakeSnapshot() (string, error) {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] TakeSnapshot 调用")
	}
	state, err := a.stateStore.Load()
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] TakeSnapshot 读取状态失败: %v", err)
		}
		return "", err
	}
	snapshotID, err := a.reconciler.SnapshotManager().TakeSnapshot(a.ctx, state)
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] TakeSnapshot 失败: %v", err)
		}
		return "", err
	}
	if a.opStore != nil {
		a.opStore.LogDebug("[API] TakeSnapshot 完成: %s", snapshotID)
	}
	return snapshotID, nil
}

// RollbackSnapshot 恢复指定快照保存的环境状态（覆盖当前状态文件）。
func (a *App) RollbackSnapshot(snapshotID string) error {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] RollbackSnapshot 调用: %s", snapshotID)
	}
	state, err := a.reconciler.SnapshotManager().LoadSnapshot(a.ctx, snapshotID)
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] RollbackSnapshot 加载失败: %v", err)
		}
		return err
	}
	if err := a.stateStore.Save(state); err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] RollbackSnapshot 写入失败: %v", err)
		}
		return err
	}
	if a.opStore != nil {
		a.opStore.LogDebug("[API] RollbackSnapshot 完成")
	}
	return nil
}
