package provider

import (
	"context"
	"fmt"
	"sync"
)

type Provider interface {
	Name() string
	Version() string
	Capabilities() []Capability
	Description() string

	NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error)
	
	Install(ctx context.Context, version string) error
	Uninstall(ctx context.Context, version string) error
	IsInstalled(version string) bool
	
	Configure(ctx context.Context, values map[string]interface{}) error
	ConfigSchema() ConfigSchema
}

type BaseProvider struct {
	name        string
	version     string
	capabilities []Capability
	description string
}

func NewBaseProvider(name, version, description string, capabilities []Capability) *BaseProvider {
	return &BaseProvider{
		name:         name,
		version:      version,
		capabilities: capabilities,
		description:  description,
	}
}

func (p *BaseProvider) Name() string {
	return p.name
}

func (p *BaseProvider) Version() string {
	return p.version
}

func (p *BaseProvider) Capabilities() []Capability {
	return p.capabilities
}

func (p *BaseProvider) Description() string {
	return p.description
}

func (p *BaseProvider) Install(ctx context.Context, version string) error {
	return nil
}

func (p *BaseProvider) Uninstall(ctx context.Context, version string) error {
	return nil
}

func (p *BaseProvider) IsInstalled(version string) bool {
	return false
}

func (p *BaseProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	return nil
}

func (p *BaseProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (p *BaseProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	return nil, nil
}

// instanceGuard 提供实例级单飞与状态互斥。
//
// 与旧实现的关键差别 —— 旧 begin() 里 `g.mu.Lock()` 之后直到 end() 才 Unlock，
// 于是 **整个 Stop 过被锁持着**（旧 10s+3s+3s，新阶梯最长 45s+），
// 任何走 Lock() 的只读调用（Status / Configure / Reload / 日志路径）都会被
// 同样长的时间阻塞。现在 busy 只是标志位，stateMu 在函数返回时立即释放，
// 互斥仅用于保护字段读写（微秒级）。
type instanceGuard struct {
	stateMu sync.Mutex
	busy    bool
	// stopNote 记录"最终确实停了，但过程有异常"的告警：
	// 如优雅关闭被认证拒收、实际靠强杀收尾、pid 文件未清理等。
	//
	// 为何不用 error 返回值携带：service.Manager 的回退路径会把任何非 nil
	// error 直接写成 StatusError（manager.go 末尾），会把"已停但走过强杀"
	// 误报成"没停"。因此状态按 Stopped 落账，风险靠这条告警上浮。
	stopNote string
}

// begin 获取单飞令牌。已忙碌时 fail-fast 返回错误（调用方直接返回）。
//
// 这里不需要像 vitess mysqlctl 那样用 capacity-one channel 让等待方响应 ctx
// 取消 —— 因为本实现根本不排队，拿不到就立即报错，不存在"阻塞中无法取消"。
func (g *instanceGuard) begin() error {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	if g.busy {
		return fmt.Errorf("操作进行中，请稍候")
	}
	g.busy = true
	return nil
}

// end 释放单飞令牌。
func (g *instanceGuard) end() {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.busy = false
}

// Lock/Unlock 保护实例共享字段（status/pid/port 等）的读写。
// 绝不在长时间操作（等待进程退出）期间持有。
func (g *instanceGuard) Lock() { g.stateMu.Lock() }

func (g *instanceGuard) Unlock() { g.stateMu.Unlock() }

// withState 在 stateMu 保护下修改实例字段，供 Start/Stop 内的状态写入使用。
func (g *instanceGuard) withState(fn func()) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	fn()
}

// StopNote 返回上一次停止留下的告警（空串表示干净停止）。
// 上层（service.Manager）据此在状态仍为 Stopped 的情况下向用户展示风险。
func (g *instanceGuard) StopNote() string {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.stopNote
}

// clearStopNote 在开始一次新的停止前清除旧告警，避免陈旧信息漂到下次结果上。
func (g *instanceGuard) clearStopNote() {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.stopNote = ""
}

// setStopNote 记录一次停止过程中的异常告警（状态仍按实际结果落账）。
func (g *instanceGuard) setStopNote(note string) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	g.stopNote = note
}

// isBusy 查询单飞令牌是否已被占用（供测试与只读判定位使用）。
func (g *instanceGuard) isBusy() bool {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.busy
}