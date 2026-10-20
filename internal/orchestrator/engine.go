package orchestrator

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// ServiceState 服务状态
type ServiceState int

const (
	StateStopped  ServiceState = iota // 已停止
	StateStarting                     // 启动中
	StateRunning                      // 运行中
	StateStopping                     // 停止中
	StateError                        // 错误状态
)

// String 返回服务状态的字符串表示
func (s ServiceState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// Service 运行时服务
type Service struct {
	// 配置信息（来自 Procfile）
	Name     string            // 服务名称
	Command  string            // 可执行文件路径
	Args     []string          // 命令行参数
	Env      map[string]string // 环境变量
	WorkDir  string            // 工作目录
	AutoRun  bool              // 是否自动启动
	GraceSec int               // 优雅关闭等待秒数
	Port     int               // 服务监听端口（provider 注册时提供；0 表示未知）

	// 运行时状态
	PID     int          // 进程 PID
	State   ServiceState // 当前状态
	Error   error        // 最后一次错误
	Started time.Time    // 启动时间

	// 依赖关系
	DependsOn []string // 依赖的服务名列表

	// FromProcfile 标记该条目是否来自 Procfile。
	// LoadProcfile 重新加载时只替换 Procfile 来源的条目，必须保留程序化注册
	// （内置服务由 service.Manager 懒注册）的条目及其运行状态，否则正在运行的
	// 进程会失去编排登记，之后 Stop 查不到条目直接报"服务不存在"却不停进程。
	FromProcfile bool
}

// clone 返回服务当前状态的安全副本，避免调用方在锁外持有并修改内部对象。
func (svc *Service) clone() Service {
	c := *svc
	return c
}

// Engine 服务编排引擎
//
// 核心职责：
// 1. 管理服务生命周期（启动、停止、重启）
// 2. 管理依赖关系（启动顺序、停止顺序）
// 3. 端口冲突检测
// 4. 使用 Windows Job Object 管理进程组
//
// 设计原则：
// - 所有操作都是幂等的
// - 错误不会中断其他服务的操作
// - 状态变更通过回调通知调用方
type Engine struct {
	mu       sync.RWMutex
	group    *process.Group
	services map[string]*Service
	procfile *Procfile

	// 启动函数（由 service.Manager 提供，调用 provider 启动服务）
	startFunc func(name string) (pid int, err error)

	// 停止函数（由 service.Manager 提供，调用 provider 停止服务）
	stopFunc func(name string) error

	// 状态变更回调
	onStateChange func(name string, oldState, newState ServiceState, err error)

	// 日志回调
	onLog func(msg string)
}

// StartFunc 启动函数类型
//
// 由 service.Manager 提供，内部调用 provider 启动服务。
// 返回启动后的 PID。
type StartFunc func(name string) (pid int, err error)

// StopFunc 停止函数类型
//
// 由 service.Manager 提供，内部调用 provider 停止服务。
type StopFunc func(name string) error

// EngineOption 引擎选项
type EngineOption func(*Engine)

// WithStateChangeCallback 设置状态变更回调
func WithStateChangeCallback(fn func(name string, oldState, newState ServiceState, err error)) EngineOption {
	return func(e *Engine) {
		e.onStateChange = fn
	}
}

// WithLogCallback 设置日志回调
func WithLogCallback(fn func(msg string)) EngineOption {
	return func(e *Engine) {
		e.onLog = fn
	}
}

// WithStartFunc 设置启动函数
func WithStartFunc(fn StartFunc) EngineOption {
	return func(e *Engine) {
		e.startFunc = fn
	}
}

// WithStopFunc 设置停止函数
func WithStopFunc(fn StopFunc) EngineOption {
	return func(e *Engine) {
		e.stopFunc = fn
	}
}

// SetStartFunc 设置启动函数（运行时设置）
func (e *Engine) SetStartFunc(fn StartFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.startFunc = fn
}

// SetStopFunc 设置停止函数（运行时设置）
func (e *Engine) SetStopFunc(fn StopFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopFunc = fn
}

// NewEngine 创建编排引擎
func NewEngine(opts ...EngineOption) (*Engine, error) {
	group, err := process.NewGroup("devstack-orchestrator")
	if err != nil {
		return nil, fmt.Errorf("创建进程组失败: %w", err)
	}

	engine := &Engine{
		group:    group,
		services: make(map[string]*Service),
	}

	for _, opt := range opts {
		opt(engine)
	}

	return engine, nil
}

// LoadProcfile 加载 Procfile
func (e *Engine) LoadProcfile(path string) error {
	parser, err := NewProcfileParser(path)
	if err != nil {
		return fmt.Errorf("创建 Procfile 解析器失败: %w", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		return fmt.Errorf("解析 Procfile 失败: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 只重建 Procfile 来源的条目：程序化注册的内置服务（nginx@1.24.0 等）连同其
	// 运行状态必须保留。之前这里整表清空，导致 reinitManagers（改安装目录、装完组件）
	// 之后正在运行的进程再无任何编排登记 —— 停止时查不到条目，报"服务不存在"且什么都不杀。
	merged := make(map[string]*Service, len(procfile.Entries)+len(e.services))
	for name, svc := range e.services {
		if !svc.FromProcfile {
			merged[name] = svc
		}
	}

	for _, entry := range procfile.Entries {
		if old, ok := merged[entry.Name]; ok {
			// 同名条目已存在（多为程序化注册）：只更新配置，保留 PID/状态
			old.Command = entry.Command
			old.Args = entry.Args
			old.Env = entry.Env
			old.WorkDir = entry.WorkDir
			old.AutoRun = entry.AutoRun
			old.GraceSec = entry.GraceSec
			old.FromProcfile = true
			continue
		}
		svc := &Service{
			Name:         entry.Name,
			Command:      entry.Command,
			Args:         entry.Args,
			Env:          entry.Env,
			WorkDir:      entry.WorkDir,
			AutoRun:      entry.AutoRun,
			GraceSec:     entry.GraceSec,
			State:        StateStopped,
			DependsOn:    e.inferDependencies(entry.Name),
			FromProcfile: true,
		}
		merged[svc.Name] = svc
	}
	e.services = merged

	e.procfile = procfile

	e.log(fmt.Sprintf("加载 Procfile 成功，共 %d 个服务", len(procfile.Entries)))

	return nil
}

// RegisterService 程序化注册服务
//
// 不依赖 Procfile，直接从 provider 获取服务定义并注册到编排引擎。
// 用于内置服务（nginx、mysql、php、redis）的编排管理。
func (e *Engine) RegisterService(name string, command string, args []string, env map[string]string, workDir string, graceSec int) error {
	if graceSec <= 0 {
		graceSec = 5
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查是否已注册
	if _, exists := e.services[name]; exists {
		// 更新已有服务
		e.services[name].Command = command
		e.services[name].Args = args
		e.services[name].Env = env
		e.services[name].WorkDir = workDir
		e.services[name].GraceSec = graceSec
		e.log(fmt.Sprintf("更新服务 %s", name))
		return nil
	}

	// 注册新服务
	svc := &Service{
		Name:      name,
		Command:   command,
		Args:      args,
		Env:       env,
		WorkDir:   workDir,
		GraceSec:  graceSec,
		State:     StateStopped,
		DependsOn: e.inferDependencies(name),
	}
	e.services[name] = svc

	e.log(fmt.Sprintf("注册服务 %s: %s", name, command))
	return nil
}

// UnregisterService 注销服务
func (e *Engine) UnregisterService(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.services[name]; !exists {
		return fmt.Errorf("服务 %s 未注册", name)
	}

	// 如果服务正在运行，先停止
	svc := e.services[name]
	if svc.State == StateRunning || svc.State == StateStarting {
		e.mu.Unlock()
		if err := e.Stop(context.Background(), name); err != nil {
			e.mu.Lock()
			e.log(fmt.Sprintf("停止服务 %s 失败: %v", name, err))
		}
		e.mu.Lock()
	}

	delete(e.services, name)
	e.log(fmt.Sprintf("注销服务 %s", name))
	return nil
}

// HasService 检查服务是否已注册
func (e *Engine) HasService(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.services[name]
	return exists
}

// AssignPID 将已运行的进程加入 Job Object 管理
//
// 在 provider 启动服务后调用，将进程纳入 Job Object，
// 确保 Group.Close() 时自动终止该进程。
func (e *Engine) AssignPID(name string, pid int) error {
	if pid <= 0 {
		return nil
	}

	e.mu.RLock()
	_, exists := e.services[name]
	e.mu.RUnlock()

	if !exists {
		// 服务未注册，先自动注册
		e.RegisterService(name, "", nil, nil, "", 5)
	}

	// 获取优雅关闭秒数
	graceSec := 5
	e.mu.RLock()
	if s, ok := e.services[name]; ok && s.GraceSec > 0 {
		graceSec = s.GraceSec
	}
	e.mu.RUnlock()

	// 将进程加入 Job Object
	if err := e.group.AssignPID(pid, name, graceSec); err != nil {
		e.log(fmt.Sprintf("将进程 %s (PID=%d) 加入 Job Object 失败: %v", name, pid, err))
		return err
	}

	// 更新服务状态
	e.mu.Lock()
	if s, ok := e.services[name]; ok {
		s.PID = pid
		s.State = StateRunning
		s.Started = time.Now()
	}
	e.mu.Unlock()

	e.log(fmt.Sprintf("将进程 %s (PID=%d) 纳入 Job Object 管理", name, pid))
	return nil
}

// ReleasePID 从 Job Object 中移除进程（不终止进程）
func (e *Engine) ReleasePID(name string, pid int) {
	if pid <= 0 {
		return
	}
	e.group.ReleasePID(pid)

	e.mu.Lock()
	if s, ok := e.services[name]; ok && s.PID == pid {
		s.PID = 0
		s.State = StateStopped
	}
	e.mu.Unlock()
}

// Adopt 把一个"进程已经在跑、但编排表里没有登记"的服务实例接管进来（幂等）。
//
// 存在理由：运行态的真相当在端口与进程上，而登记是 Manager.Start 的副作用。
// DevStack 重启、外部手动启动、reinit 等场景下会出现"进程活着但没登记"，
// 此时若不允许接管，Stop 就会以记账缺失为由拒绝执行（历史表现：显示运行中，
// 点停止报"服务 xxx 不存在"）。pids 应传该实例在端口上的全部进程，
// 首个作为代表 PID（调用方给根进程），其余仅纳入 Job Object。
func (e *Engine) Adopt(name string, port int, pids []int) error {
	if name == "" {
		return fmt.Errorf("Adopt 需要服务名")
	}
	if len(pids) == 0 {
		return fmt.Errorf("服务 %s 没有可接管的进程", name)
	}

	e.mu.Lock()
	svc, ok := e.services[name]
	if !ok {
		svc = &Service{
			Name:      name,
			GraceSec:  5,
			State:     StateStopped,
			DependsOn: e.inferDependencies(name),
		}
		e.services[name] = svc
	}
	if port > 0 {
		svc.Port = port
	}
	e.mu.Unlock()

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		// Job Object 纳入失败不影响登记有效性（进程仍归本工具管理）
		if err := e.AssignPID(name, pid); err != nil {
			e.log(fmt.Sprintf("接管 %s (PID=%d) 失败: %v", name, pid, err))
		}
	}

	// 代表 PID 固定为调用方给出的首个（根进程），避免最后 assign 的 worker 顶掉它
	e.mu.Lock()
	if s, ok := e.services[name]; ok && pids[0] > 0 {
		s.PID = pids[0]
		s.State = StateRunning
	}
	e.mu.Unlock()
	return nil
}

// inferDependencies 推断服务依赖关系
//
// 基于常见模式推断：
// - PHP 依赖 Nginx（PHP 需要 Web 服务器）
// - MySQL 不依赖其他服务
// - Redis 不依赖其他服务
//
// 后续可以扩展为显式配置依赖关系。
func (e *Engine) inferDependencies(name string) []string {
	var deps []string

	switch {
	case containsCI(name, "php"):
		// PHP 通常依赖 Nginx 或 Apache
		for n := range e.services {
			if containsCI(n, "nginx") || containsCI(n, "apache") {
				deps = append(deps, n)
			}
		}
	case containsCI(name, "worker"):
		// Worker 通常依赖队列服务
		for n := range e.services {
			if containsCI(n, "redis") || containsCI(n, "rabbit") || containsCI(n, "kafka") {
				deps = append(deps, n)
			}
		}
	}

	return deps
}

// containsCI 不区分大小写的字符串包含检查
func containsCI(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && len(substr) > 0 &&
				containsLower(s, substr))
}

func containsLower(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return contains(s, substr)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// StartAll 启动所有自动启动的服务
// SetServicePort 设置服务监听端口（provider 注册后由 service.Manager 提供）。
// waitForReady/checkPortConflict 依赖它做真实探活；经 RegisterService(name,"",nil,...)
// 注册的内置服务 Command/Args 为空，从命令行提取不到端口。
func (e *Engine) SetServicePort(name string, port int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.services[name]; ok {
		s.Port = port
	}
}

// StartAll 启动所有 AutoRun 服务
func (e *Engine) StartAll(ctx context.Context) error {
	order, cycle := e.getStartupOrder()
	if len(cycle) > 0 {
		return fmt.Errorf("服务依赖存在循环: %v，无法确定启动顺序", cycle)
	}

	for _, name := range order {
		svc := e.getService(name)
		if svc == nil || !svc.AutoRun {
			continue
		}

		if err := e.Start(ctx, name); err != nil {
			e.log(fmt.Sprintf("启动 %s 失败: %v", name, err))
			// 继续启动其他服务
		}
	}

	return nil
}

// Start 启动单个服务
func (e *Engine) Start(ctx context.Context, name string) error {
	svc := e.getService(name)
	if svc == nil {
		return fmt.Errorf("服务 %s 不存在", name)
	}

	e.mu.Lock()
	if svc.State == StateRunning || svc.State == StateStarting {
		e.mu.Unlock()
		return nil
	}
	svc.State = StateStarting
	svc.Error = nil
	e.mu.Unlock()

	e.notifyStateChange(name, StateStopped, StateStarting, nil)
	e.log(fmt.Sprintf("正在启动 %s ...", name))

	// 1. 检查依赖
	if err := e.checkDependencies(name); err != nil {
		e.setError(name, err)
		return err
	}

	// 2. 检查端口冲突
	if err := e.checkPortConflict(svc); err != nil {
		e.setError(name, err)
		return err
	}

	// 3. 启动进程（使用 startFunc 回调，由 provider 实际启动）
	var pid int
	if e.startFunc != nil {
		var err error
		pid, err = e.startFunc(name)
		if err != nil {
			e.setError(name, err)
			return err
		}
	} else {
		// 回退到直接启动（Procfile 模式）
		proc, err := e.group.Start(
			svc.Name,
			svc.Command,
			svc.Args,
			svc.buildEnv(),
			svc.WorkDir,
			svc.GraceSec,
		)
		if err != nil {
			e.setError(name, err)
			return err
		}
		pid = proc.PID
	}

	// 4. 将进程加入 Job Object 管理
	if pid > 0 {
		if err := e.group.AssignPID(pid, name, svc.GraceSec); err != nil {
			e.log(fmt.Sprintf("将进程 %s 加入 Job Object 失败: %v", name, err))
		}
	}

	// 5. 就绪验证
	//    - provider 模式（startFunc）：就绪是 provider 的契约——entity.Start 返回成功
	//      即已监听端口（nginx/php/redis/mysql 内部均轮询至端口就绪才返回）。
	//      编排引擎只复核进程存活，防止 provider 谎报（秒退进程）。
	//    - Procfile 模式：进程由编排引擎自己拉起，自己负责端口探活。
	if e.startFunc != nil {
		if pid > 0 {
			time.Sleep(300 * time.Millisecond)
			if !isPIDRunning(pid) {
				err := fmt.Errorf("%s 启动失败: 进程在启动确认前退出（PID=%d）", name, pid)
				e.group.Stop(pid)
				e.setError(name, err)
				return err
			}
		}
	} else {
		if err := e.waitForReady(svc, pid, 10*time.Second); err != nil {
			// 启动超时或失败，停止进程
			e.group.Stop(pid)
			e.setError(name, err)
			return err
		}
	}

	// 6. 更新状态
	e.mu.Lock()
	svc.PID = pid
	svc.State = StateRunning
	svc.Started = time.Now()
	e.mu.Unlock()

	e.notifyStateChange(name, StateStarting, StateRunning, nil)
	e.log(fmt.Sprintf("%s 启动成功 (PID=%d)", name, pid))

	return nil
}

// Stop 停止单个服务
//
// 停止是"清理动作"，不允许因编排表里没有登记就拒绝执行：
// 未登记时按需补登记（与 AssignPID 的自愈一致）继续走停止流程。
// 之前直接 return "服务 %s 不存在"，会让上层既没杀掉进程、又把内部记账
// 缺失当作用户可见错误抛出，表现为"显示运行中、停止报服务不存在"的死循环。
func (e *Engine) Stop(ctx context.Context, name string) error {
	svc := e.getService(name)
	if svc == nil {
		if e.stopFunc == nil {
			// Procfile 模式下条目确实不存在，无从停止，如实报错
			return fmt.Errorf("服务 %s 不存在", name)
		}
		e.mu.Lock()
		svc = &Service{
			Name:      name,
			GraceSec:  5,
			State:     StateStopped,
			DependsOn: e.inferDependencies(name),
		}
		e.services[name] = svc
		e.mu.Unlock()
		e.log(fmt.Sprintf("服务 %s 未登记，按停止请求补登记（可能是外部启动或状态同步遗漏）", name))
	}

	e.mu.Lock()
	if svc.State == StateStopped || svc.State == StateStopping {
		pidKnown := svc.PID
		e.mu.Unlock()
		// 状态说"已停止"不等于进程真的没在跑：以端口为准做一次收尸式复核。
		// 端口上仍有本服务的监听进程说明记账与现实不一致，必须继续停止，
		// 否则 UI 会永远停在"已停止但端口被占"的状态。
		if pidKnown > 0 || (svc.Port > 0 && process.FindPIDByPort(svc.Port) > 0) {
			e.mu.Lock()
			svc.State = StateRunning
			e.mu.Unlock()
		} else {
			return nil
		}
	}
	oldState := svc.State
	svc.State = StateStopping
	// 在锁内快照 PID，停止/等待全程用这个局部值，避免无锁反复读 svc.PID 的数据竞争。
	pid := svc.PID
	if pid <= 0 && svc.Port > 0 {
		// 记账里没有 PID（冷启动接管的进程、或整表被清过）：
		// 从端口发现一个真实 PID，否则后面的退出复核会对着 PID=0 秒判"已停止"。
		pid = process.FindPIDByPort(svc.Port)
		svc.PID = pid
	}
	e.mu.Unlock()

	e.notifyStateChange(name, oldState, StateStopping, nil)
	e.log(fmt.Sprintf("正在停止 %s ...", name))

	// 1. 检查是否有服务依赖此服务
	if deps := e.getDependents(name); len(deps) > 0 {
		e.log(fmt.Sprintf("警告: %s 被以下服务依赖: %v", name, deps))
	}

	// 2. 优雅停止（使用 stopFunc 回调，由 provider 实际停止）
	if e.stopFunc != nil {
		// provider 的停止已包含"优雅 → 强杀 → 按端口集合复核"，它返回错误就是真没停干净。
		// 这里绝不能再吞掉错误后靠 PID 存活判断自行宣布"已停止"——那正是
		// UI 显示已停止、进程却还在监听、下次 Start 被幂等跳过的成因。
		if err := e.stopFunc(name); err != nil {
			e.log(fmt.Sprintf("停止 %s 失败: %v", name, err))
			if pid > 0 {
				e.group.ForceStop(pid) // 尽力兜底，同时把进程移出 Job Object
			}
			e.setError(name, err)
			return err
		}
	} else {
		// 回退到直接停止（Procfile 模式）
		if err := e.group.Stop(pid); err != nil {
			// 优雅停止失败，强制停止
			e.log(fmt.Sprintf("优雅停止 %s 失败，尝试强制停止: %v", name, err))
			e.group.ForceStop(pid)
		}
	}

	// 3. 等待进程退出
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// markStopped 复核进程真的退出后才允许标记 Stopped；
	// 进程仍在运行却置 Stopped 是"记账性僵尸"——后续 Start 会被幂等跳过，用户被欺骗
	markStopped := func() bool {
		if isPIDRunning(pid) {
			return false
		}
		e.mu.Lock()
		svc.State = StateStopped
		svc.PID = 0
		e.mu.Unlock()
		e.notifyStateChange(name, StateStopping, StateStopped, nil)
		e.log(fmt.Sprintf("%s 已停止", name))
		return true
	}

	for {
		select {
		case <-ctx.Done():
			if !markStopped() {
				err := fmt.Errorf("停止 %s 被取消: 进程 %d 仍在运行", name, pid)
				e.setError(name, err)
				return err
			}
			return ctx.Err()
		case <-deadline:
			// 超时，强制停止
			e.group.ForceStop(pid)
			// 强杀后给进程 1 秒退出时间再复核
			time.Sleep(1 * time.Second)
			if !markStopped() {
				err := fmt.Errorf("停止 %s 失败: 强制终止后进程 %d 仍在运行", name, pid)
				e.setError(name, err)
				return err
			}
			return nil
		case <-ticker.C:
			if markStopped() {
				return nil
			}
		}
	}
}

// StopAll 停止所有服务（反向顺序）
func (e *Engine) StopAll(ctx context.Context) error {
	order := e.getShutdownOrder()

	var lastErr error
	for _, name := range order {
		if err := e.Stop(ctx, name); err != nil {
			e.log(fmt.Sprintf("停止 %s 失败: %v", name, err))
			lastErr = err
			// 继续停止其他服务
		}
	}

	return lastErr
}

// Restart 重启服务
func (e *Engine) Restart(ctx context.Context, name string) error {
	if err := e.Stop(ctx, name); err != nil {
		e.log(fmt.Sprintf("重启时停止 %s 失败: %v", name, err))
	}

	// 等待一小段时间确保端口释放
	time.Sleep(500 * time.Millisecond)

	return e.Start(ctx, name)
}

// Status 获取服务状态（返回副本，调用方修改不影响引擎内部状态）
func (e *Engine) Status(name string) (Service, error) {
	svc := e.getService(name)
	if svc == nil {
		return Service{}, fmt.Errorf("服务 %s 不存在", name)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 运行中但进程已消失（被外部杀死等）：修正为已停止。
	// 状态修正必须走写锁——旧实现在 RLock 下写是数据竞争。
	if svc.State == StateRunning && !isPIDRunning(svc.PID) {
		svc.State = StateStopped
		svc.PID = 0
	}

	return svc.clone(), nil
}

// List 获取所有服务状态（返回副本）
func (e *Engine) List() []Service {
	e.mu.Lock()
	defer e.mu.Unlock()

	result := make([]Service, 0, len(e.services))
	for _, svc := range e.services {
		// 检查进程是否真的在运行（写锁下修正）
		if svc.State == StateRunning && !isPIDRunning(svc.PID) {
			svc.State = StateStopped
			svc.PID = 0
		}
		result = append(result, svc.clone())
	}

	return result
}

// Shutdown 关闭编排引擎
func (e *Engine) Shutdown(ctx context.Context) error {
	// 1. 停止所有服务
	e.StopAll(ctx)

	// 2. 关闭进程组
	return e.group.Close()
}

// ---------- 内部方法 ----------

// getService 获取服务（读锁）
func (e *Engine) getService(name string) *Service {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.services[name]
}

// setError 设置服务错误状态
func (e *Engine) setError(name string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	svc := e.services[name]
	if svc == nil {
		return
	}

	oldState := svc.State
	svc.State = StateError
	svc.Error = err

	// 在锁外通知状态变更
	e.mu.Unlock()
	e.notifyStateChange(name, oldState, StateError, err)
	e.mu.Lock()
}

// notifyStateChange 通知状态变更
func (e *Engine) notifyStateChange(name string, oldState, newState ServiceState, err error) {
	if e.onStateChange != nil {
		e.onStateChange(name, oldState, newState, err)
	}
}

// log 输出日志
func (e *Engine) log(msg string) {
	if e.onLog != nil {
		e.onLog(msg)
	}
	log.Printf("[Orchestrator] %s", msg)
}

// getStartupOrder 获取启动顺序（拓扑排序）
//
// 规则：
// 1. 被依赖的服务先启动
// 2. 同级服务按名称排序
//
// 返回 (有序列表, 环上服务)。环上的服务无法确定启动顺序，调用方必须显式处理（报错），
// 不能像旧实现那样静默丢弃——那样环上服务不启动也不报错，用户无从得知。
func (e *Engine) getStartupOrder() ([]string, []string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 计算入度
	inDegree := make(map[string]int)
	for name := range e.services {
		inDegree[name] = 0
	}

	for _, svc := range e.services {
		for _, dep := range svc.DependsOn {
			if _, ok := e.services[dep]; ok {
				inDegree[svc.Name]++
			}
		}
	}

	// BFS 拓扑排序
	var order []string
	queue := make([]string, 0)

	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	// 排序以保证确定性
	sortStrings(queue)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		// 更新依赖此服务的服务的入度
		for name, svc := range e.services {
			for _, dep := range svc.DependsOn {
				if dep == current {
					inDegree[name]--
					if inDegree[name] == 0 {
						queue = append(queue, name)
						sortStrings(queue)
					}
				}
			}
		}
	}

	// 拓扑排序后仍未出队的即环上（或依赖环）服务
	var cycle []string
	for name := range e.services {
		found := false
		for _, o := range order {
			if o == name {
				found = true
				break
			}
		}
		if !found {
			cycle = append(cycle, name)
		}
	}
	sortStrings(cycle)

	return order, cycle
}

// getShutdownOrder 获取停止顺序（反向拓扑排序）。环上服务按名称序附在末尾，保证不被遗漏。
func (e *Engine) getShutdownOrder() []string {
	order, cycle := e.getStartupOrder()

	// 反转顺序
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	return append(order, cycle...)
}

// getDependents 获取依赖指定服务的服务列表
func (e *Engine) getDependents(name string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []string
	for _, svc := range e.services {
		for _, dep := range svc.DependsOn {
			if dep == name {
				result = append(result, svc.Name)
				break
			}
		}
	}

	return result
}

// checkDependencies 检查服务依赖
func (e *Engine) checkDependencies(name string) error {
	svc := e.getService(name)
	if svc == nil {
		return fmt.Errorf("服务 %s 不存在", name)
	}

	for _, dep := range svc.DependsOn {
		depSvc := e.getService(dep)
		if depSvc == nil {
			continue // 依赖不存在，跳过
		}

		if depSvc.State != StateRunning {
			return fmt.Errorf("服务 %s 依赖的 %s 未运行", name, dep)
		}
	}

	return nil
}

// checkPortConflict 检查端口冲突
func (e *Engine) checkPortConflict(svc *Service) error {
	// 内置服务经 RegisterService(name,"",...) 注册，Command/Args 为空，
	// 必须优先使用 provider 提供的 Port 字段
	ports := extractPorts(svc.Command, svc.Args)
	if svc.Port > 0 {
		ports = append(ports, svc.Port)
	}

	for _, port := range ports {
		if isPortInUse(port) {
			// 检查是否是自己的进程
			pid := findPIDByPort(port)
			if pid != svc.PID {
				// 报上占用者可执行文件路径：落在 DevStack 自身 programs 目录内
				// 就是会话残留，否则是外部程序——两者处置完全不同，不能只给个 PID。
				if exe := process.GetExecutablePath(pid); exe != "" {
					return fmt.Errorf("端口 %d 被进程 %d 占用（%s）", port, pid, exe)
				}
				return fmt.Errorf("端口 %d 被进程 %d 占用（无法读取占用者路径）", port, pid)
			}
		}
	}

	return nil
}

// waitForReady 等待服务就绪
//
// 通过真实 TCP 拨测监听端口判断就绪，未监听即未就绪——
// MySQL 慢启动或进程秒退都不会被误判成功。
// 对确实没有端口的服务，等待进程存活确认后视为就绪。
func (e *Engine) waitForReady(svc *Service, pid int, timeout time.Duration) error {
	ports := extractPorts(svc.Command, svc.Args)
	if svc.Port > 0 {
		ports = append(ports, svc.Port)
	}

	if len(ports) == 0 {
		// 无端口服务：确认进程存活即可，不再固定 sleep 500ms 谎报就绪
		deadline := time.After(timeout)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline:
				return fmt.Errorf("等待 %s 就绪超时", svc.Name)
			case <-ticker.C:
				if pid <= 0 || isPIDRunning(pid) {
					return nil
				}
				return fmt.Errorf("%s 进程已退出（PID=%d）", svc.Name, pid)
			}
		}
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// 超时前最后确认：进程是否还活着（秒退的进程永远等不到端口）
			if pid > 0 && !isPIDRunning(pid) {
				return fmt.Errorf("%s 启动失败: 进程在就绪前退出（PID=%d）", svc.Name, pid)
			}
			return fmt.Errorf("等待 %s 就绪超时", svc.Name)
		case <-ticker.C:
			// 进程秒退：直接失败，不必干等超时
			if pid > 0 && !isPIDRunning(pid) {
				return fmt.Errorf("%s 启动失败: 进程在就绪前退出（PID=%d）", svc.Name, pid)
			}
			allReady := true
			for _, port := range ports {
				if !isPortReady(port) {
					allReady = false
					break
				}
			}
			if allReady {
				return nil
			}
		}
	}
}

// buildEnv 构建环境变量列表
func (svc *Service) buildEnv() []string {
	env := make([]string, 0, len(svc.Env))
	for k, v := range svc.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// extractPorts 从命令和参数中提取端口号
func extractPorts(command string, args []string) []int {
	var ports []int

	// 检查命令本身
	if port := extractPortFromArg(command); port > 0 {
		ports = append(ports, port)
	}

	// 检查参数
	for _, arg := range args {
		if port := extractPortFromArg(arg); port > 0 {
			ports = append(ports, port)
		}
	}

	return ports
}

// extractPortFromArg 从参数中提取端口号
func extractPortFromArg(arg string) int {
	// 查找常见的端口格式
	// -b 127.0.0.1:9000
	// --port=3000
	// -p 8080
	// :3000

	// 检查 -b 或 --bind 参数
	if arg == "-b" || arg == "--bind" {
		return 0 // 需要下一个参数
	}

	// 检查 --port=N 格式
	if len(arg) > 7 && arg[:7] == "--port=" {
		portStr := arg[7:]
		if port, err := parsePort(portStr); err == nil {
			return port
		}
	}

	// 检查 -p N 格式
	if arg == "-p" || arg == "--port" {
		return 0 // 需要下一个参数
	}

	// 检查 :N 格式（如 127.0.0.1:9000）
	if len(arg) > 0 && arg[0] == ':' {
		if port, err := parsePort(arg[1:]); err == nil {
			return port
		}
	}

	// 检查 IP:PORT 格式
	if idx := lastIndexByte(arg, ':'); idx > 0 {
		portStr := arg[idx+1:]
		if port, err := parsePort(portStr); err == nil {
			return port
		}
	}

	return 0
}

func parsePort(s string) (int, error) {
	port := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid port: %s", s)
		}
		port = port*10 + int(ch-'0')
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// isPIDRunning 检查进程是否在运行
func isPIDRunning(pid int) bool {
	return process.IsProcessRunning(pid)
}

// isPortInUse 检查端口是否被占用
func isPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// isPortReady 检查端口是否可连接（服务就绪）
func isPortReady(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// findPIDByPort 通过端口查找 PID
func findPIDByPort(port int) int {
	// 内核 TCP 表（GetExtendedTcpTable，netstat 兜底）；同端口多监听者时取首个
	return process.FindPIDByPort(port)
}

// sortStrings 排序字符串切片
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
