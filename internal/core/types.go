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
	TypeNginx    ServiceType = "nginx"
	TypeApache   ServiceType = "apache"
	TypeMySQL    ServiceType = "mysql"
	TypePostgreSQL ServiceType = "postgresql"
	TypeMongoDB  ServiceType = "mongodb"
	TypeRedis    ServiceType = "redis"
	TypeMemcached ServiceType = "memcached"
	TypePHP      ServiceType = "php"
	TypeNode     ServiceType = "node"
	TypePython   ServiceType = "python"
	TypeComposer ServiceType = "composer"
	TypePhpMyAdmin ServiceType = "phpmyadmin"
)

const (
	CategoryWebServer = "webserver"
	CategoryDatabase  = "database"
	CategoryService   = "service"
	CategoryTool      = "tools"
)

type ServiceInfo struct {
	ID        string        `json:"id"`        // "nginx@1.24.0"
	Kind      string        `json:"kind"`      // "nginx"
	Name      string        `json:"name"`      // "Nginx"
	Type      ServiceType   `json:"type"`
	Category  string        `json:"category"`
	Version   string        `json:"version"`   // "1.24.0"
	Status    ServiceStatus `json:"status"`
	Port      int           `json:"port"`
	PID       int           `json:"pid"`
	Installed bool          `json:"installed"`
	Error     string        `json:"error,omitempty"`
	Uptime    string        `json:"uptime,omitempty"`
}

// ==================== Runtime ====================

type RuntimeType string

const (
	RuntimePHP   RuntimeType = "php"
	RuntimeNode  RuntimeType = "node"
	RuntimePython RuntimeType = "python"
)

type RuntimeInfo struct {
	ID        string        `json:"id"`        // "php@7.4.33"
	Kind      string        `json:"kind"`      // "php"
	Type      RuntimeType   `json:"type"`
	Version   string        `json:"version"`
	Path      string        `json:"path"`
	Active    bool          `json:"active"`     // 命令行默认版本
	Running   bool          `json:"running"`    // php-cgi 是否运行 (PHP 特有)
	Port      int           `json:"port"`       // PHP 特有：FastCGI 端口
	Installed bool          `json:"installed"`
	Status    ServiceStatus `json:"status"`
	Name      string        `json:"name"`
	PID       int           `json:"pid"`
	Uptime    string        `json:"uptime,omitempty"`
}

// ==================== Project ====================

type ProjectType string

const (
	ProjectPHP    ProjectType = "php"
	ProjectNode   ProjectType = "node"
	ProjectPython ProjectType = "python"
	ProjectStatic ProjectType = "static"
	ProjectProxy  ProjectType = "proxy"
)

type Project struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Type       ProjectType `json:"type"`

	Root       string      `json:"root"`       // 项目根目录

	Domain     string      `json:"domain"`
	Port       int         `json:"port"`        // HTTP 监听端口 (默认 80)
	HTTPSPort  int         `json:"httpsPort"`   // HTTPS 端口 (默认 443)
	SSL        bool        `json:"ssl"`

	// EnvironmentID 引用的环境模板（EnvironmentProfile.ID），同一环境可被多个项目共享。
	// 非空时，以下五个 *Version 字段可通过 profile 自动填充，但也允许项目单独覆盖。
	EnvironmentID string `json:"environmentID,omitempty"`

	// 以下字段保持向后兼容，保留；在 EnvironmentID 存在但字段为空时，作为 profile 版本的"项目级覆盖"
	PHPVersion    string `json:"phpVersion,omitempty"`    // PHP 版本
	MySQLVersion  string `json:"mysqlVersion,omitempty"`  // MySQL 版本
	NginxVersion  string `json:"nginxVersion,omitempty"`  // Nginx 版本
	NodeVersion   string `json:"nodeVersion,omitempty"`   // Node.js 版本
	PythonVersion string `json:"pythonVersion,omitempty"` // Python 版本

	ProxyPass  string      `json:"proxyPass,omitempty"`  // Proxy 反代目标
	TargetPort int         `json:"targetPort,omitempty"`  // Node/Python 进程端口

	Rewrite      string            `json:"rewrite,omitempty"`
	Notes        string            `json:"notes,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"` // key: capability, value: providerID

	Enabled    bool        `json:"enabled"`
	CreatedAt  string      `json:"createdAt"`
}

// ==================== Legacy (keep for backward compat) ====================

type Site struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	DocumentRoot string `json:"documentRoot"`
	PHPEnabled   bool   `json:"phpEnabled"`
	PHPVersion   string `json:"phpVersion,omitempty"`
	Port         int    `json:"port"`
	HTTPSPort    int    `json:"httpsPort,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	Type         string `json:"type,omitempty"`
	Rewrite      string `json:"rewrite,omitempty"`
	SSL          bool   `json:"ssl"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"createdAt"`
	Notes        string `json:"notes,omitempty"`
}

type AppSettings struct {
	InstallDir     string `json:"installDir"`
	StartOnBoot    bool   `json:"startOnBoot"`
	MinimizeToTray bool   `json:"minimizeToTray"`
	Theme          string `json:"theme"`
	Language       string `json:"language"`

	NginxConfig  NginxSettings  `json:"nginxConfig"`
	PHPConfig    PHPSettings    `json:"phpConfig"`
	MySQLConfig  MySQLSettings  `json:"mysqlConfig"`
}

type NginxSettings struct {
	WorkerConnections   int    `json:"workerConnections"`
	ClientMaxBodySize   string `json:"clientMaxBodySize"`
	GzipEnabled         bool   `json:"gzipEnabled"`
	StaticCacheDays     int    `json:"staticCacheDays"`
	KeepaliveTimeout    int    `json:"keepaliveTimeout"`
	ServerTokens        bool   `json:"serverTokens"`
}

type PHPSettings struct {
	MemoryLimit         string `json:"memoryLimit"`
	MaxExecutionTime    int    `json:"maxExecutionTime"`
	UploadMaxFilesize   string `json:"uploadMaxFilesize"`
	PostMaxSize         string `json:"postMaxSize"`
	DisplayErrors       bool   `json:"displayErrors"`
	OPcacheEnabled      bool   `json:"opcacheEnabled"`
	RealpathCacheSize   string `json:"realpathCacheSize"`

	MaxInputTime         int    `json:"maxInputTime"`
	MaxInputVars         int    `json:"maxInputVars"`
	MaxFileUploads       int    `json:"maxFileUploads"`
	Timezone             string `json:"timezone"`
	DefaultCharset       string `json:"defaultCharset"`
	FileUploads          bool   `json:"fileUploads"`
	SessionGCMaxLifetime int    `json:"sessionGCMaxLifetime"`
	LogErrors            bool   `json:"logErrors"`
	ErrorLog             string `json:"errorLog"`
	FixPathinfo          bool   `json:"fixPathinfo"`
}

type MySQLSettings struct {
	InnoDBBufferPoolSize     string `json:"innodbBufferPoolSize"`
	InnoDBLogFileSize        string `json:"innodbLogFileSize"`
	InnoDBFlushLogAtTrxCommit int  `json:"innodbFlushLogAtTrxCommit"`
	MaxConnections           int    `json:"maxConnections"`
	TmpTableSize             string `json:"tmpTableSize"`
	MaxHeapTableSize         string `json:"maxHeapTableSize"`
	ThreadCacheSize          int    `json:"threadCacheSize"`
	TableOpenCache           int    `json:"tableOpenCache"`
	BindAddress              string `json:"bindAddress"`
	Port                     int    `json:"port"`

	CharacterSet      string `json:"characterSet"`
	Collation         string `json:"collation"`
	SQLMode           string `json:"sqlMode"`
	SkipNameResolve   bool   `json:"skipNameResolve"`
	GeneralLog        bool   `json:"generalLog"`
	SlowQueryLog      bool   `json:"slowQueryLog"`
	LongQueryTime     int    `json:"longQueryTime"`
	MaxAllowedPacket  string `json:"maxAllowedPacket"`
	KeyBufferSize     string `json:"keyBufferSize"`
	WaitTimeout       int    `json:"waitTimeout"`
	SortBufferSize    string `json:"sortBufferSize"`
	JoinBufferSize    string `json:"joinBufferSize"`
}

type SystemInfo struct {
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
	AppVersion string `json:"appVersion"`
	InstallDir string `json:"installDir"`
}
