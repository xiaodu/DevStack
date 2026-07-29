package engine

import "github.com/xiaodu/DevStack/internal/core"

// EnvironmentState 表示所有服务/项目/运行时的期望状态快照
// 与 environment.EnvironmentProfile（用户定义的"环境"模板）区分
type EnvironmentState struct {
	Services map[string]ServiceDesiredState `json:"services"`
	Projects map[string]ProjectDesiredState `json:"projects"`
	Runtimes map[string]RuntimeDesiredState `json:"runtimes"`
	// ActiveProfileID 记录当前激活的环境模板 ID（空表示基于项目推导）
	ActiveProfileID string `json:"activeProfileID,omitempty"`
}

type ServiceDesiredState struct {
	Version string               `json:"version"`
	State   core.ServiceStatus   `json:"state"`
}

type ProjectDesiredState struct {
	Name          string            `json:"name"`
	Type          core.ProjectType  `json:"type"`
	Root          string            `json:"root"`
	Domain        string            `json:"domain"`
	Port          int               `json:"port"`
	HTTPSPort     int               `json:"httpsPort"`
	SSL           bool              `json:"ssl"`
	EnvironmentID string            `json:"environmentID,omitempty"`
	PHPVersion    string            `json:"phpVersion,omitempty"`
	MySQLVersion  string            `json:"mysqlVersion,omitempty"`
	NginxVersion  string            `json:"nginxVersion,omitempty"`
	NodeVersion   string            `json:"nodeVersion,omitempty"`
	PythonVersion string            `json:"pythonVersion,omitempty"`
	ProxyPass     string            `json:"proxyPass,omitempty"`
	TargetPort    int               `json:"targetPort,omitempty"`
	Rewrite       string            `json:"rewrite,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	Enabled       bool              `json:"enabled"`
	Dependencies  map[string]string `json:"dependencies,omitempty"`
}

type RuntimeDesiredState struct {
	Version string               `json:"version"`
	State   core.ServiceStatus   `json:"state"`
	Active  bool                 `json:"active"`
}

type ChangeType string

const (
	ChangeStart     ChangeType = "start"
	ChangeStop      ChangeType = "stop"
	ChangeConfigure ChangeType = "configure"
	ChangeCreate    ChangeType = "create"
	ChangeDelete    ChangeType = "delete"
	ChangeUpdate    ChangeType = "update"
)

const (
	EntityService  = "service"
	EntityProject  = "project"
	EntityRuntime  = "runtime"
)

type Change struct {
	Type      ChangeType        `json:"type"`
	EntityID  string            `json:"entityId"`
	EntityType string           `json:"entityType"`
	From      interface{}       `json:"from,omitempty"`
	To        interface{}       `json:"to,omitempty"`
}

type Reconciler interface {
	Apply(env EnvironmentState) error
	Diff(env EnvironmentState) ([]Change, error)
	GetCurrentState() (EnvironmentState, error)
}
