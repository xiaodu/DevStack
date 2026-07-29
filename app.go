package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	stdlog "log"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

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
	AppVersion  string
}

var serviceNames = map[string]string{
	"nginx": "Nginx", "mysql": "MySQL", "redis": "Redis",
}

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

	opStore.LogInfo("[INIT] 初始化进程管理器...")
	procMgr := process.NewManager()
	opStore.LogInfo("[INIT] 进程管理器初始化完成")

	opStore.LogInfo("[INIT] 初始化资源管理器...")
	resMgr := resource.NewManager(filepath.Join(installDir, "config"))
	opStore.LogInfo("[INIT] 资源管理器初始化完成")

	opStore.LogInfo("[INIT] 初始化服务管理器...")
	svcMgr := service.NewManager(cfg, procMgr, resMgr)
	services := svcMgr.List()
	opStore.LogInfo("[INIT] 服务管理器初始化完成, 已注册服务数: %d", len(services))

	opStore.LogInfo("[INIT] 初始化运行时管理器...")
	rtMgr := runtime.NewManager(cfg, procMgr, resMgr)
	runtimes := rtMgr.List()
	opStore.LogInfo("[INIT] 运行时管理器初始化完成, 已注册运行时数: %d", len(runtimes))

	opStore.LogInfo("[INIT] 初始化项目管理器...")
	projMgr := project.NewManager(installDir, "1.26.0", rtMgr, resMgr)
	projMgr.SetServiceManager(svcMgr)
	projMgr.SetRuntimeManager(rtMgr)
	projects := projMgr.List()
	opStore.LogInfo("[INIT] 项目管理器初始化完成, 项目数: %d", len(projects))

	opStore.LogInfo("[INIT] 初始化环境管理器 (Environment Profiles)...")
	envMgr := environment.NewManager(installDir)
	opStore.LogInfo("[INIT] 环境管理器初始化完成, 内置预设已注册")

	opStore.LogInfo("[INIT] 初始化日志监视器...")
	logWatcher := log.NewLogWatcher()
	opStore.LogInfo("[INIT] 日志监视器初始化完成")

	opStore.LogInfo("[INIT] 初始化下载管理器...")
	dlMgr := download.NewManager(installDir)
	opStore.LogInfo("[INIT] 下载管理器初始化完成")

	opStore.LogInfo("[INIT] 初始化状态存储...")
	dataDir := filepath.Join(installDir, "config")
	stateStore := engine.NewStateStore(dataDir)
	opStore.LogInfo("[INIT] 状态存储初始化完成, 数据目录: %s", dataDir)

	opStore.LogInfo("[INIT] 初始化协调器(Reconciler)...")
	reconciler := engine.NewReconciler(stateStore, svcMgr, projMgr, rtMgr)
	opStore.LogInfo("[INIT] 协调器初始化完成")

	opStore.LogInfo("[INIT] 初始化响应式引擎...")
	reactiveEng := reactive.NewEngine(100)
	opStore.LogInfo("[INIT] 响应式引擎创建完成")

	opStore.LogInfo("[INIT] 注册服务提供者...")
	provider.NewNginxProvider(cfg, procMgr, resMgr)
	provider.NewMySQLProvider(cfg, procMgr)
	provider.NewPHPProvider(cfg, procMgr, resMgr)
	opStore.LogInfo("[INIT] 服务提供者注册完成")

	opStore.LogInfo("[INIT] 注册内置响应式规则...")
	reactive.RegisterBuiltinRules(reactiveEng,
		func(serviceID string) error {
			if strings.HasPrefix(serviceID, "php-") {
				return rtMgr.Start(serviceID)
			}
			return svcMgr.Start(serviceID)
		},
		func(owner string, oldPort int) (int, error) {
			resMgr.Ports().Release(oldPort)
			return resMgr.Ports().Allocate(owner, 0)
		},
		func(eventType reactive.EventType, source string, data map[string]interface{}) {
			opStore.Add("告警", string(eventType), source, fmt.Sprintf("%v", data))
		},
	)
	reactiveEng.Start()
	opStore.LogInfo("[INIT] 响应式引擎启动完成")

	elapsed := time.Since(startTime)
	opStore.LogInfo("[INIT] NewApp 初始化完成, 耗时: %s", elapsed.String())
	opStore.LogInfo("[INIT] 应用版本: 0.2.0")

	return &App{
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
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.opStore != nil {
		a.opStore.LogInfo("[STARTUP] Wails 应用启动完成, context 已设置")
		a.opStore.LogDebug("[STARTUP] 当前安装目录: %s", a.cfg.Get().InstallDir)
	}

	// ── Bridge reactive engine events → Wails frontend events ──
	if a.reactiveEng != nil {
		eventTypes := []reactive.EventType{
			reactive.EventServiceStarted,
			reactive.EventServiceStopped,
			reactive.EventServiceCrashed,
			reactive.EventServiceHealthFailed,
			reactive.EventServiceRestarted,
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

// ==================== Declarative Environment APIs ====================

func (a *App) ApplyEnvironment(env engine.EnvironmentState) error {
	if a.opStore != nil {
		serviceCount := 0
		if env.Services != nil {
			serviceCount = len(env.Services)
		}
		projectCount := 0
		if env.Projects != nil {
			projectCount = len(env.Projects)
		}
		runtimeCount := 0
		if env.Runtimes != nil {
			runtimeCount = len(env.Runtimes)
		}
		a.opStore.LogDebug("[API] ApplyEnvironment 调用, 服务数: %d, 项目数: %d, 运行时数: %d",
			serviceCount, projectCount, runtimeCount)
	}
	err := a.reconciler.Apply(env)
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] ApplyEnvironment 失败: %v", err)
		}
		return err
	}
	if a.opStore != nil {
		a.opStore.LogInfo("[API] ApplyEnvironment 成功")
	}
	return nil
}

func (a *App) GetEnvironment() (engine.EnvironmentState, error) {
	if a.opStore != nil {
		a.opStore.LogDebug("[API] GetEnvironment 调用")
	}
	result, err := a.reconciler.GetCurrentState()
	if err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] GetEnvironment 失败: %v", err)
		}
		return result, err
	}

	// 从持久化状态中读取当前激活的 profile ID
	if stored, serr := a.stateStore.Load(); serr == nil {
		result.ActiveProfileID = stored.ActiveProfileID
	}
	if a.opStore != nil {
		serviceCount := 0
		if result.Services != nil {
			serviceCount = len(result.Services)
		}
		projectCount := 0
		if result.Projects != nil {
			projectCount = len(result.Projects)
		}
		runtimeCount := 0
		if result.Runtimes != nil {
			runtimeCount = len(result.Runtimes)
		}
		a.opStore.LogDebug("[API] GetEnvironment 返回, 服务数: %d, 项目数: %d, 运行时数: %d",
			serviceCount, projectCount, runtimeCount)
	}
	return result, nil
}

func (a *App) DiffEnvironment(env engine.EnvironmentState) ([]engine.Change, error) {
	return a.reconciler.Diff(env)
}

// ==================== Environment Activation APIs (内部走依赖图顺序) ====================

// GetActiveEnvironment 返回当前活跃环境状态，等价于 GetEnvironment，命名更贴合核心概念
func (a *App) GetActiveEnvironment() (engine.EnvironmentState, error) {
	return a.GetEnvironment()
}

// ActivateEnvironment 按环境模板启动依赖的服务与运行时。
// - profileID 非空：按指定 EnvironmentProfile 组合声明需要运行的软件
// - profileID 为空：基于所有已启用项目联合所需的软件版本推导
// 所有启动顺序由 reconciler 内部依赖图统一处理，禁止前端写死顺序。
func (a *App) ActivateEnvironment(profileID string) error {
	if a.opStore != nil {
		a.opStore.LogInfo("[API] ActivateEnvironment 调用, profileID: %q", profileID)
	}

	current, err := a.reconciler.GetCurrentState()
	if err != nil {
		return fmt.Errorf("读取当前环境失败: %w", err)
	}

	desired := engine.EnvironmentState{
		Services: make(map[string]engine.ServiceDesiredState),
		Projects: make(map[string]engine.ProjectDesiredState),
		Runtimes: make(map[string]engine.RuntimeDesiredState),
	}
	for pid, proj := range current.Projects {
		desired.Projects[pid] = proj
	}

	collectFromProjects := func() {
		for _, proj := range current.Projects {
			if !proj.Enabled {
				continue
			}
			p := proj
			a.hydrateProjectVersionsFromCurrent(&p, proj.EnvironmentID)

			if p.NginxVersion != "" {
				svcID := "nginx"
				if _, ok := current.Services[svcID]; !ok {
					svcID = "nginx-" + p.NginxVersion
				}
				if cur, ok := current.Services[svcID]; ok {
					desired.Services[svcID] = engine.ServiceDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
					}
				}
			}
			if p.MySQLVersion != "" {
				svcID := "mysql"
				if _, ok := current.Services[svcID]; !ok {
					svcID = "mysql-" + p.MySQLVersion
				}
				if cur, ok := current.Services[svcID]; ok {
					desired.Services[svcID] = engine.ServiceDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
					}
				}
			}
			if p.PHPVersion != "" {
				rtID := "php-" + p.PHPVersion
				if cur, ok := current.Runtimes[rtID]; ok {
					desired.Runtimes[rtID] = engine.RuntimeDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
						Active:  cur.Active,
					}
				}
			}
			if p.NodeVersion != "" {
				rtID := "node-" + p.NodeVersion
				if cur, ok := current.Runtimes[rtID]; ok {
					desired.Runtimes[rtID] = engine.RuntimeDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
						Active:  cur.Active,
					}
				}
			}
			if p.PythonVersion != "" {
				rtID := "python-" + p.PythonVersion
				if cur, ok := current.Runtimes[rtID]; ok {
					desired.Runtimes[rtID] = engine.RuntimeDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
						Active:  cur.Active,
					}
				}
			}
			for capType, depID := range p.Dependencies {
				if capType == "service" {
					if cur, ok := current.Services[depID]; ok {
						desired.Services[depID] = engine.ServiceDesiredState{
							Version: cur.Version,
							State:   core.StatusRunning,
						}
					}
				} else if capType == "runtime" {
					if cur, ok := current.Runtimes[depID]; ok {
						desired.Runtimes[depID] = engine.RuntimeDesiredState{
							Version: cur.Version,
							State:   core.StatusRunning,
							Active:  cur.Active,
						}
					}
				} else if strings.HasPrefix(capType, "service:") {
					svcID := depID
					if cur, ok := current.Services[svcID]; ok {
						desired.Services[svcID] = engine.ServiceDesiredState{
							Version: cur.Version,
							State:   core.StatusRunning,
						}
					}
				}
			}
		}
	}

	collectFromProfile := func(profile environment.EnvironmentProfile) {
		for _, spec := range profile.Services {
			svcID := spec.ID
			if _, ok := current.Services[svcID]; !ok {
				svcID = spec.ID + "-" + spec.Version
			}
			if cur, ok := current.Services[svcID]; ok {
				desired.Services[svcID] = engine.ServiceDesiredState{
					Version: cur.Version,
					State:   core.StatusRunning,
				}
			}
		}
		for _, spec := range profile.Runtimes {
			rtID := spec.RuntimeID + "-" + spec.Version
			if cur, ok := current.Runtimes[rtID]; ok {
				desired.Runtimes[rtID] = engine.RuntimeDesiredState{
					Version: cur.Version,
					State:   core.StatusRunning,
					Active:  cur.Active || spec.Default,
				}
			}
		}
		for capType, depID := range profile.Dependencies {
			if strings.HasPrefix(capType, "service:") {
				if cur, ok := current.Services[depID]; ok {
					desired.Services[depID] = engine.ServiceDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
					}
				}
			} else if capType == "service" {
				if cur, ok := current.Services[depID]; ok {
					desired.Services[depID] = engine.ServiceDesiredState{
						Version: cur.Version,
						State:   core.StatusRunning,
					}
				}
			}
		}
	}

	if profileID != "" && a.envMgr != nil {
		profile, err := a.envMgr.Get(profileID)
		if err == nil {
			collectFromProfile(profile)
		} else {
			collectFromProjects()
		}
	} else {
		collectFromProjects()
	}

	for sid, svc := range current.Services {
		if _, already := desired.Services[sid]; !already {
			desired.Services[sid] = svc
		}
	}
	for rid, rt := range current.Runtimes {
		if _, already := desired.Runtimes[rid]; !already {
			desired.Runtimes[rid] = rt
		}
	}

	if err := a.ApplyEnvironment(desired); err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] ActivateEnvironment 失败: %v", err)
		}
		return err
	}

	// 持久化当前激活的 profile ID，供前端显示"当前在用"标记
	desired.ActiveProfileID = profileID
	if saveErr := a.stateStore.Save(desired); saveErr != nil {
		if a.opStore != nil {
			a.opStore.LogWarn("[API] ActivateEnvironment 保存 activeProfileID 失败: %v", saveErr)
		}
	}

	if a.opStore != nil {
		a.opStore.LogInfo("[API] ActivateEnvironment 成功, 已声明需运行的服务数: %d, 运行时数: %d",
			len(desired.Services), len(desired.Runtimes))
	}
	return nil
}

// hydrateProjectVersionsFromCurrent 把 engine.ProjectDesiredState 中声明的空版本字段按 profile 补齐
func (a *App) hydrateProjectVersionsFromCurrent(p *engine.ProjectDesiredState, envID string) {
	if p == nil || envID == "" || a.envMgr == nil {
		return
	}
	profile, err := a.envMgr.Get(envID)
	if err != nil {
		return
	}
	if p.PHPVersion == "" {
		if spec, ok := profile.Runtimes["php"]; ok {
			p.PHPVersion = spec.Version
		}
	}
	if p.NodeVersion == "" {
		if spec, ok := profile.Runtimes["node"]; ok {
			p.NodeVersion = spec.Version
		}
	}
	if p.PythonVersion == "" {
		if spec, ok := profile.Runtimes["python"]; ok {
			p.PythonVersion = spec.Version
		}
	}
	if p.NginxVersion == "" {
		if spec, ok := profile.Services["nginx"]; ok {
			p.NginxVersion = spec.Version
		}
	}
	if p.MySQLVersion == "" {
		if spec, ok := profile.Services["mysql"]; ok {
			p.MySQLVersion = spec.Version
		}
	}
}

// ActivateEnvironmentForProject 按单个项目关联的环境进行激活（供 Sites 页面按钮调用）
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
	// 未设置环境的项目，退化为走"所有已启用项目联合推导"（此时会启用本项目所需）
	if !proj.Enabled {
		_ = a.projMgr.Toggle(projectID)
	}
	return a.ActivateEnvironment("")
}

// DeactivateEnvironment 停止当前所有运行中的服务/运行时（逆向顺序，由 reconciler 负责）。
func (a *App) DeactivateEnvironment() error {
	if a.opStore != nil {
		a.opStore.LogInfo("[API] DeactivateEnvironment 调用")
	}
	current, err := a.reconciler.GetCurrentState()
	if err != nil {
		return fmt.Errorf("读取当前环境失败: %w", err)
	}
	desired := engine.EnvironmentState{
		Services: make(map[string]engine.ServiceDesiredState),
		Projects: make(map[string]engine.ProjectDesiredState),
		Runtimes: make(map[string]engine.RuntimeDesiredState),
	}
	for pid, proj := range current.Projects {
		desired.Projects[pid] = proj
	}
	for sid, svc := range current.Services {
		desired.Services[sid] = engine.ServiceDesiredState{
			Version: svc.Version,
			State:   core.StatusStopped,
		}
	}
	for rid, rt := range current.Runtimes {
		desired.Runtimes[rid] = engine.RuntimeDesiredState{
			Version: rt.Version,
			State:   core.StatusStopped,
			Active:  rt.Active,
		}
	}
	if err := a.ApplyEnvironment(desired); err != nil {
		if a.opStore != nil {
			a.opStore.LogError("[API] DeactivateEnvironment 失败: %v", err)
		}
		return err
	}

	// 清除当前激活的 profile ID
	desired.ActiveProfileID = ""
	if saveErr := a.stateStore.Save(desired); saveErr != nil {
		if a.opStore != nil {
			a.opStore.LogWarn("[API] DeactivateEnvironment 清除 activeProfileID 失败: %v", saveErr)
		}
	}

	if a.opStore != nil {
		a.opStore.LogInfo("[API] DeactivateEnvironment 成功")
	}
	return nil
}

// ==================== EnvironmentProfile APIs ====================

// hydrateProjectVersions 当项目引用了 EnvironmentProfile 且自身版本字段为空时，
// 用 Profile 中声明的版本填充，保持与旧版散字段的一致性。
func (a *App) hydrateProjectVersions(p *core.Project) {
	if p == nil || p.EnvironmentID == "" || a.envMgr == nil {
		return
	}
	profile, err := a.envMgr.Get(p.EnvironmentID)
	if err != nil {
		return
	}
	if p.PHPVersion == "" {
		if spec, ok := profile.Runtimes["php"]; ok {
			p.PHPVersion = spec.Version
		}
	}
	if p.NodeVersion == "" {
		if spec, ok := profile.Runtimes["node"]; ok {
			p.NodeVersion = spec.Version
		}
	}
	if p.PythonVersion == "" {
		if spec, ok := profile.Runtimes["python"]; ok {
			p.PythonVersion = spec.Version
		}
	}
	if p.NginxVersion == "" {
		if spec, ok := profile.Services["nginx"]; ok {
			p.NginxVersion = spec.Version
		}
	}
	if p.MySQLVersion == "" {
		if spec, ok := profile.Services["mysql"]; ok {
			p.MySQLVersion = spec.Version
		}
	}
	if len(p.Dependencies) == 0 && len(profile.Dependencies) > 0 {
		p.Dependencies = make(map[string]string)
		for k, v := range profile.Dependencies {
			p.Dependencies[k] = v
		}
	}
}

// profileProjectUsage 统计每个 profile 被多少项目引用，用于 UI 展示 & 删除校验
func (a *App) profileProjectUsage(profileID string) int {
	if a.projMgr == nil {
		return 0
	}
	cnt := 0
	for _, proj := range a.projMgr.List() {
		if proj.EnvironmentID == profileID {
			cnt++
		}
	}
	return cnt
}

// EnvironmentProfileWithUsage 给前端返回带引用计数的 Profile 列表
type EnvironmentProfileWithUsage struct {
	environment.EnvironmentProfile
	ProjectCount int `json:"projectCount"`
}

func (a *App) ListEnvironmentProfiles() ([]EnvironmentProfileWithUsage, error) {
	if a.envMgr == nil {
		return nil, nil
	}
	raw := a.envMgr.List(nil)
	result := make([]EnvironmentProfileWithUsage, 0, len(raw))
	for _, p := range raw {
		result = append(result, EnvironmentProfileWithUsage{
			EnvironmentProfile: p,
			ProjectCount:        a.profileProjectUsage(p.ID),
		})
	}
	return result, nil
}

func (a *App) GetEnvironmentProfile(id string) (EnvironmentProfileWithUsage, error) {
	if a.envMgr == nil {
		return EnvironmentProfileWithUsage{}, core.ErrNotFound
	}
	p, err := a.envMgr.Get(id)
	if err != nil {
		return EnvironmentProfileWithUsage{}, err
	}
	return EnvironmentProfileWithUsage{
		EnvironmentProfile: p,
		ProjectCount:        a.profileProjectUsage(id),
	}, nil
}

func (a *App) CreateEnvironmentProfile(profile environment.EnvironmentProfile) (environment.EnvironmentProfile, error) {
	if a.envMgr == nil {
		return environment.EnvironmentProfile{}, fmt.Errorf("环境管理器未初始化")
	}
	return a.envMgr.Create(profile)
}

func (a *App) UpdateEnvironmentProfile(profile environment.EnvironmentProfile) (environment.EnvironmentProfile, error) {
	if a.envMgr == nil {
		return environment.EnvironmentProfile{}, fmt.Errorf("环境管理器未初始化")
	}
	return a.envMgr.Update(profile)
}

func (a *App) DeleteEnvironmentProfile(id string) error {
	if a.envMgr == nil {
		return nil
	}
	if a.profileProjectUsage(id) > 0 {
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

func (a *App) SaveProjectAsEnvironment(args SaveProjectAsEnvironmentArgs) (environment.EnvironmentProfile, error) {
	if a.envMgr == nil {
		return environment.EnvironmentProfile{}, fmt.Errorf("环境管理器未初始化")
	}
	if a.projMgr == nil {
		return environment.EnvironmentProfile{}, fmt.Errorf("项目管理器未初始化")
	}
	proj, err := a.projMgr.Get(args.ProjectID)
	if err != nil {
		return environment.EnvironmentProfile{}, err
	}
	a.hydrateProjectVersions(&proj)

	serviceVersions := make(map[string]string)
	if proj.NginxVersion != "" {
		serviceVersions["nginx"] = proj.NginxVersion
	}
	if proj.MySQLVersion != "" {
		serviceVersions["mysql"] = proj.MySQLVersion
	}
	for capType, depID := range proj.Dependencies {
		if strings.HasPrefix(capType, "service:") {
			svcID := strings.TrimPrefix(capType, "service:")
			if serviceVersions[svcID] == "" {
				serviceVersions[svcID] = "latest"
			}
			_ = depID
		}
	}

	runtimeVersions := make(map[string]string)
	if proj.PHPVersion != "" {
		runtimeVersions["php"] = proj.PHPVersion
	}
	if proj.NodeVersion != "" {
		runtimeVersions["node"] = proj.NodeVersion
	}
	if proj.PythonVersion != "" {
		runtimeVersions["python"] = proj.PythonVersion
	}

	name := args.Name
	if name == "" {
		name = proj.Name + " 的环境"
	}
	return a.envMgr.SaveProjectAsEnvironment(args.ProjectID, name, args.Description, serviceVersions, runtimeVersions)
}



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
	return a.cfg.Update(s)
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
	installDir := a.cfg.Get().InstallDir
	if installDir == "" {
		return fmt.Errorf("安装目录未设置，请先完成初始化设置")
	}

	var urls []string
	if url != "" {
		urls = append(urls, url)
	}
	releases, err := a.dlMgr.GetAvailableVersions(serviceType)
	if err != nil {
		return fmt.Errorf("获取版本列表失败: %w", err)
	}
	for _, r := range releases {
		if r.Version == version {
			if r.MirrorURL != "" {
				urls = append(urls, r.MirrorURL)
			}
			if url == "" && r.URL != "" {
				urls = append(urls, r.URL)
			}
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

	if err := a.dlMgr.TryDownloadAndExtract(urls, destDir, progressFn); err != nil {
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
		return a.svcMgr.Configure(serviceType)
	}

	a.opStore.Add("下载", runtimeNames[serviceType], version, "成功")
	return nil
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
	} else if serviceID == "redis" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "programs", "redis", svc.Info.Version, "redis.log")
		}
	} else if strings.HasPrefix(serviceID, "php-") {
		version := strings.TrimPrefix(serviceID, "php-")
		logPath = joinPath(installDir, "programs", "php", version, "php_errors.log")
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

// ==================== Snapshot APIs ====================

func (a *App) TakeSnapshot() (string, error) {
	current, err := a.reconciler.GetCurrentState()
	if err != nil {
		return "", err
	}
	return a.reconciler.SnapshotManager().TakeSnapshot(a.ctx, current)
}

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

func (a *App) RollbackSnapshot(snapshotID string) error {
	env, err := a.reconciler.SnapshotManager().LoadSnapshot(a.ctx, snapshotID)
	if err != nil {
		return err
	}
	return a.reconciler.Apply(env)
}

func (a *App) GetSnapshotDetail(snapshotID string) (engine.EnvironmentState, error) {
	return a.reconciler.SnapshotManager().LoadSnapshot(a.ctx, snapshotID)
}

func (a *App) DeleteSnapshot(snapshotID string) error {
	return a.reconciler.SnapshotManager().DeleteSnapshot(a.ctx, snapshotID)
}
