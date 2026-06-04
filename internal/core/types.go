package core

type ServiceStatus string

const (
	StatusRunning    ServiceStatus = "running"
	StatusStopped    ServiceStatus = "stopped"
	StatusInstalling ServiceStatus = "installing"
	StatusError      ServiceStatus = "error"
	StatusStarting   ServiceStatus = "starting"
	StatusStopping   ServiceStatus = "stopping"
)

type ServiceType string

const (
	TypeNginx  ServiceType = "nginx"
	TypeMySQL  ServiceType = "mysql"
	TypeRedis  ServiceType = "redis"
	TypePHP    ServiceType = "php"
	TypeNode   ServiceType = "node"
	TypePython ServiceType = "python"
)

type ServiceInfo struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      ServiceType   `json:"type"`
	Version   string        `json:"version"`
	Status    ServiceStatus `json:"status"`
	Port      int           `json:"port"`
	PID       int           `json:"pid"`
	Installed bool          `json:"installed"`
	AutoStart bool          `json:"autoStart"`
	Error     string        `json:"error,omitempty"`
	Uptime    string        `json:"uptime,omitempty"`
}

type Site struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Domain     string `json:"domain"`
	Root       string `json:"root"`
	PHPEnabled bool   `json:"phpEnabled"`
	PHPVersion string `json:"phpVersion,omitempty"`
	Port       int    `json:"port"`
	SSL        bool   `json:"ssl"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"createdAt"`
	Notes      string `json:"notes,omitempty"`
}

type AppSettings struct {
	InstallDir     string `json:"installDir"`
	DataDir        string `json:"dataDir"`
	StartOnBoot    bool   `json:"startOnBoot"`
	MinimizeToTray bool   `json:"minimizeToTray"`
	Theme          string `json:"theme"`
	Language       string `json:"language"`
}

type SystemInfo struct {
	GoVersion    string `json:"goVersion"`
	Platform     string `json:"platform"`
	Arch         string `json:"arch"`
	CPU          string `json:"cpu"`
	Memory       string `json:"memory"`
	AppVersion   string `json:"appVersion"`
	InstallDir   string `json:"installDir"`
	DataDir      string `json:"dataDir"`
}
