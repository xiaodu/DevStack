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
	// ActiveEnvironmentID 与 SaveActive 写入的 activeEnvironmentId 对称（v4：系统仅持久化此一条运行时信息）
	ActiveEnvironmentID string `json:"activeEnvironmentId,omitempty"`
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


