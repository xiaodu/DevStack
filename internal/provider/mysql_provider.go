package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type MySQLProvider struct {
	*BaseProvider
	cfg      *config.Config
	pm       *process.Manager
	instance *MySQLInstance
}

type MySQLInstance struct {
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	port      int
	mysqlDir  string
	mysqlExe  string
	dataDir   string
	startedAt time.Time
}

func NewMySQLProvider(cfg *config.Config, pm *process.Manager) *MySQLProvider {
	p := &MySQLProvider{
		BaseProvider: NewBaseProvider(
			"mysql",
			"",
			"关系型数据库管理系统",
			[]Capability{CapabilityDatabase},
		),
		cfg: cfg,
		pm:  pm,
	}
	MustRegister("mysql", p, CapabilityDatabase)
	return p
}

func (p *MySQLProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	version := "latest"
	if v, ok := config["version"].(string); ok {
		version = v
	}
	port := 3306
	if pt, ok := config["port"].(int); ok {
		port = pt
	}

	installDir := p.cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "programs", "mysql", version)

	instance := &MySQLInstance{
		id:       "mysql-" + version,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		mysqlDir: mysqlDir,
		mysqlExe: filepath.Join(mysqlDir, "bin", "mysqld.exe"),
		dataDir:  filepath.Join(installDir, "data", "mysql"),
	}
	p.instance = instance
	return instance, nil
}

func (p *MySQLProvider) Install(ctx context.Context, version string) error {
	return nil
}

func (p *MySQLProvider) Uninstall(ctx context.Context, version string) error {
	return nil
}

func (p *MySQLProvider) IsInstalled(version string) bool {
	installDir := p.cfg.Get().InstallDir
	mysqlExe := filepath.Join(installDir, "programs", "mysql", version, "bin", "mysqld.exe")
	_, err := os.Stat(mysqlExe)
	return err == nil
}

func (p *MySQLProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	if p.instance == nil {
		return fmt.Errorf("MySQL instance not initialized")
	}
	return p.applyConfig(p.instance)
}

func (p *MySQLProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "port", Type: "int", Default: 3306, Description: "数据库端口", Required: true},
			{Name: "rootPassword", Type: "string", Default: "", Description: "root 密码"},
			{Name: "maxConnections", Type: "int", Default: 151, Description: "最大连接数"},
			{Name: "innodbBufferPoolSize", Type: "string", Default: "128M", Description: "InnoDB 缓冲池大小"},
		},
	}
}

func (p *MySQLProvider) applyConfig(instance *MySQLInstance) error {
	mysqlDir := instance.mysqlDir

	confDir := filepath.Join(mysqlDir, "conf")
	os.MkdirAll(confDir, 0755)

	installDir := p.cfg.Get().InstallDir
	dataDir := filepath.Join(installDir, "data", "mysql")
	os.MkdirAll(dataDir, 0755)

	confPath := filepath.Join(confDir, "my.ini")

	content := fmt.Sprintf(`[mysqld]
port=%d
datadir=%s
socket=%s\\mysql.sock
default-time-zone='+08:00'
character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci
init_connect='SET NAMES utf8mb4'

innodb_buffer_pool_size=%s
innodb_log_file_size=64M
innodb_flush_log_at_trx_commit=1
innodb_lock_wait_timeout=50

max_connections=%d
max_connect_errors=100
wait_timeout=600
interactive_timeout=600

query_cache_type=0
query_cache_size=0

log_error=%s\\logs\\error.log
slow_query_log=1
slow_query_log_file=%s\\logs\\slow.log
long_query_time=2
`,
		instance.port,
		filepath.ToSlash(dataDir),
		filepath.ToSlash(mysqlDir),
		"128M",
		151,
		filepath.ToSlash(mysqlDir),
		filepath.ToSlash(mysqlDir),
	)

	return os.WriteFile(confPath, []byte(content), 0644)
}

func (p *MySQLProvider) CreateDatabase(name string) error {
	return nil
}

func (p *MySQLProvider) DropDatabase(name string) error {
	return nil
}

func (p *MySQLProvider) CreateUser(username, password string) error {
	return nil
}

func (p *MySQLProvider) GrantPrivileges(username, database string) error {
	return nil
}

func (p *MySQLProvider) ExecuteSQL(sql string) error {
	return nil
}

func (p *MySQLProvider) CheckConnection(ctx context.Context) error {
	if p.instance == nil || p.instance.pid <= 0 {
		return fmt.Errorf("MySQL not running")
	}
	return nil
}

func (i *MySQLInstance) ID() string {
	return i.id
}

func (i *MySQLInstance) Type() string {
	return "database"
}

func (i *MySQLInstance) Start(ctx context.Context) error {
	if !fileExists(i.mysqlExe) {
		return fmt.Errorf("MySQL executable not found: %s", i.mysqlExe)
	}

	i.status = core.StatusStarting

	myiniPath := filepath.Join(i.mysqlDir, "conf", "my.ini")

	if !fileExists(i.dataDir) {
		initCmd := &initCommand{mysqlExe: i.mysqlExe, dataDir: i.dataDir}
		if err := initCmd.run(); err != nil {
			i.status = core.StatusError
			return err
		}
	}

	args := []string{
		fmt.Sprintf("--defaults-file=%s", myiniPath),
		"--datadir=" + i.dataDir,
		"--port=" + fmt.Sprintf("%d", i.port),
	}

	pid, err := process.NewManager().Start("mysql", i.mysqlExe, args, nil, filepath.Dir(filepath.Dir(i.mysqlDir)))
	if err != nil {
		i.status = core.StatusError
		return err
	}

	for j := 0; j < 10; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := process.FindPIDByPort(i.port); p > 0 {
			i.pid = p
			break
		}
	}

	if i.pid <= 0 && !process.NewManager().IsRunning(pid) {
		process.NewManager().Stop(pid)
		i.status = core.StatusError
		return fmt.Errorf("MySQL start failed: port %d not responding", i.port)
	}

	if i.pid <= 0 {
		i.pid = pid
	}

	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

func (i *MySQLInstance) Stop(ctx context.Context) error {
	if i.pid <= 0 {
		return nil
	}
	i.status = core.StatusStopping

	pid := i.pid
	process.NewManager().Stop(pid)

	killAllByName("mysqld.exe")

	for j := 0; j < 6; j++ {
		time.Sleep(500 * time.Millisecond)
		if !process.NewManager().IsRunning(pid) {
			break
		}
	}

	i.pid = 0
	i.status = core.StatusStopped
	return nil
}

func (i *MySQLInstance) Status() (string, error) {
	return string(i.status), nil
}

func (i *MySQLInstance) HealthCheck(ctx context.Context) error {
	if i.pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !process.NewManager().IsRunning(i.pid) {
		return fmt.Errorf("process not running")
	}
	return nil
}

func (i *MySQLInstance) Configure(values map[string]interface{}) error {
	return nil
}

func (i *MySQLInstance) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (i *MySQLInstance) LogPaths() []string {
	return []string{
		filepath.Join(i.mysqlDir, "logs", "error.log"),
		filepath.Join(i.mysqlDir, "logs", "slow.log"),
	}
}

func (i *MySQLInstance) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{}, nil
}

type initCommand struct {
	mysqlExe string
	dataDir  string
}

func (c *initCommand) run() error {
	return nil
}