package provider

import (
	"context"
)

type Upstream struct {
	Name     string
	Protocol string
	Host     string
	Port     int
}

type VhostConfig struct {
	ID        string
	Domain    string
	Root      string
	Upstreams []Upstream
	SSL       bool
	SSLPort   int
	HTTPPort  int
	ProxyPass string
	Rewrite   string
}

type ConfigSchema struct {
	Fields []ConfigField
}

type ConfigField struct {
	Name        string
	Type        string
	Default     interface{}
	Description string
	Required    bool
}

type Metrics struct {
	CPUUsage    float64
	MemoryUsage float64
	Connections int
	Requests    int64
}

type ManagedEntity interface {
	ID() string
	Type() string

	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() (string, error)
	HealthCheck(ctx context.Context) error

	Configure(values map[string]interface{}) error
	ConfigSchema() ConfigSchema

	LogPaths() []string
	Metrics(ctx context.Context) (Metrics, error)
}

type WebServerCapability interface {
	ServeHTTP(root string, upstream Upstream) error
	Reload(ctx context.Context) error
	AddVhost(config VhostConfig) error
	RemoveVhost(id string) error
	GetVhosts() []VhostConfig
}

type DatabaseCapability interface {
	CreateDatabase(name string) error
	DropDatabase(name string) error
	CreateUser(username, password string) error
	GrantPrivileges(username, database string) error
	ExecuteSQL(sql string) error
	CheckConnection(ctx context.Context) error
}

type FPMRuntimeCapability interface {
	GetListenPort() int
	SetListenPort(port int) error
	Reload(ctx context.Context) error
	CheckHealth(ctx context.Context) error
}