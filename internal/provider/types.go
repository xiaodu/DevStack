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

// instanceGuard 提供实例级互斥与单飞：Start/Stop 等会修改实例状态的操作
// 必须通过 begin/end 包裹，防止并发 Start+Stop 互杀/互拉；busy 标志阻止
// 同一实例被并发重复操作（如 Stop 期间再次 Stop）。
type instanceGuard struct {
	mu   sync.Mutex
	busy bool
}

// begin 获取实例互斥并标记忙碌。已忙碌时返回错误（调用方应直接返回）。
func (g *instanceGuard) begin() error {
	g.mu.Lock()
	if g.busy {
		g.mu.Unlock()
		return fmt.Errorf("操作进行中，请稍候")
	}
	g.busy = true
	return nil
}

// end 释放单飞标志与互斥。
func (g *instanceGuard) end() {
	g.busy = false
	g.mu.Unlock()
}

// Lock/Unlock 提供不改变 busy 状态的轻量锁，供只读实例状态的调用方使用。
func (g *instanceGuard) Lock() {
	g.mu.Lock()
}

func (g *instanceGuard) Unlock() {
	g.mu.Unlock()
}