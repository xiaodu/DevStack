package provider

import (
	"context"
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