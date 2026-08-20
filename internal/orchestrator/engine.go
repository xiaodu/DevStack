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

	// 运行时状态
	PID   int          // 进程 PID
	State ServiceState // 当前状态
	Error error        // 最后一次错误
	Started time.Time  // 启动时间

	// 依赖关系
	DependsOn []string // 依赖的服务名列表
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

	// 状态变更回调
	onStateChange func(name string, oldState, newState ServiceState, err error)

	// 日志回调
	onLog func(msg string)
}

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

	// 清空现有服务
	e.services = make(map[string]*Service)

	// 转换为 Service 定义
	for _, entry := range procfile.Entries {
		svc := &Service{
			Name:      entry.Name,
			Command:   entry.Command,
			Args:      entry.Args,
			Env:       entry.Env,
			WorkDir:   entry.WorkDir,
			AutoRun:   entry.AutoRun,
			GraceSec:  entry.GraceSec,
			State:     StateStopped,
			DependsOn: e.inferDependencies(entry.Name),
		}
		e.services[svc.Name] = svc
	}

	e.procfile = procfile

	e.log(fmt.Sprintf("加载 Procfile 成功，共 %d 个服务", len(procfile.Entries)))

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
func (e *Engine) StartAll(ctx context.Context) error {
	order := e.getStartupOrder()

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

	// 3. 启动进程
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

	// 4. 等待服务就绪
	if err := e.waitForReady(svc, 10*time.Second); err != nil {
		// 启动超时或失败，停止进程
		e.group.Stop(proc.PID)
		e.setError(name, err)
		return err
	}

	// 5. 更新状态
	e.mu.Lock()
	svc.PID = proc.PID
	svc.State = StateRunning
	svc.Started = time.Now()
	e.mu.Unlock()

	e.notifyStateChange(name, StateStarting, StateRunning, nil)
	e.log(fmt.Sprintf("%s 启动成功 (PID=%d)", name, proc.PID))

	return nil
}

// Stop 停止单个服务
func (e *Engine) Stop(ctx context.Context, name string) error {
	svc := e.getService(name)
	if svc == nil {
		return fmt.Errorf("服务 %s 不存在", name)
	}

	e.mu.Lock()
	if svc.State == StateStopped || svc.State == StateStopping {
		e.mu.Unlock()
		return nil
	}
	oldState := svc.State
	svc.State = StateStopping
	e.mu.Unlock()

	e.notifyStateChange(name, oldState, StateStopping, nil)
	e.log(fmt.Sprintf("正在停止 %s ...", name))

	// 1. 检查是否有服务依赖此服务
	if deps := e.getDependents(name); len(deps) > 0 {
		e.log(fmt.Sprintf("警告: %s 被以下服务依赖: %v", name, deps))
	}

	// 2. 优雅停止
	if err := e.group.Stop(svc.PID); err != nil {
		// 优雅停止失败，强制停止
		e.log(fmt.Sprintf("优雅停止 %s 失败，尝试强制停止: %v", name, err))
		e.group.ForceStop(svc.PID)
	}

	// 3. 等待进程退出
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			svc.State = StateStopped
			svc.PID = 0
			e.mu.Unlock()
			return ctx.Err()
		case <-deadline:
			// 超时，强制停止
			e.group.ForceStop(svc.PID)
			e.mu.Lock()
			svc.State = StateStopped
			svc.PID = 0
			e.mu.Unlock()
			e.notifyStateChange(name, StateStopping, StateStopped, nil)
			e.log(fmt.Sprintf("%s 已停止", name))
			return nil
		case <-ticker.C:
			if !isPIDRunning(svc.PID) {
				e.mu.Lock()
				svc.State = StateStopped
				svc.PID = 0
				e.mu.Unlock()
				e.notifyStateChange(name, StateStopping, StateStopped, nil)
				e.log(fmt.Sprintf("%s 已停止", name))
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

// Status 获取服务状态
func (e *Engine) Status(name string) (*Service, error) {
	svc := e.getService(name)
	if svc == nil {
		return nil, fmt.Errorf("服务 %s 不存在", name)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// 检查进程是否真的在运行
	if svc.State == StateRunning && !isPIDRunning(svc.PID) {
		svc.State = StateStopped
		svc.PID = 0
	}

	return svc, nil
}

// List 获取所有服务状态
func (e *Engine) List() []*Service {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*Service, 0, len(e.services))
	for _, svc := range e.services {
		// 检查进程是否真的在运行
		if svc.State == StateRunning && !isPIDRunning(svc.PID) {
			svc.State = StateStopped
			svc.PID = 0
		}
		result = append(result, svc)
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
func (e *Engine) getStartupOrder() []string {
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

	return order
}

// getShutdownOrder 获取停止顺序（反向拓扑排序）
func (e *Engine) getShutdownOrder() []string {
	order := e.getStartupOrder()

	// 反转顺序
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	return order
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
	// 从命令行参数中提取端口
	ports := extractPorts(svc.Command, svc.Args)

	for _, port := range ports {
		if isPortInUse(port) {
			// 检查是否是自己的进程
			pid := findPIDByPort(port)
			if pid != svc.PID {
				return fmt.Errorf("端口 %d 被进程 %d 占用", port, pid)
			}
		}
	}

	return nil
}

// waitForReady 等待服务就绪
//
// 通过检查端口是否可连接来判断服务是否就绪。
// 对于没有端口的服务，等待一小段时间后认为就绪。
func (e *Engine) waitForReady(svc *Service, timeout time.Duration) error {
	ports := extractPorts(svc.Command, svc.Args)

	if len(ports) == 0 {
		// 没有端口，等待一小段时间
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("等待 %s 就绪超时", svc.Name)
		case <-ticker.C:
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
	// 使用 netstat 查找占用端口的进程
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
