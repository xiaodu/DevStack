package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/mysqlutil"
	"github.com/xiaodu/DevStack/internal/process"
)

type MySQLProvider struct {
	*BaseProvider
	cfg      *config.Config
	pm       *process.Manager
	instance *MySQLInstance
}

type MySQLInstance struct {
	instanceGuard
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	port      int
	mysqlDir  string
	mysqlExe  string
	dataDir   string
	pm        *process.Manager
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
		dataDir:  filepath.Join(installDir, "data", "mysql", version),
		pm:       p.pm,
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

	// 使用与实例一致的版本化数据目录，避免多版本共享同一 datadir 导致数据串写
	dataDir := instance.dataDir
	os.MkdirAll(dataDir, 0755)

	// log_error/slow_query_log_file 指向 logs 目录，必须提前创建，否则首次启动 mysqld 打不开日志直接退出
	os.MkdirAll(filepath.Join(mysqlDir, "logs"), 0755)

	confPath := filepath.Join(confDir, "my.ini")

	// 仅在配置不存在时生成引导默认值；已存在说明用户或配置页已定制，不得覆盖
	if fileExists(confPath) {
		return nil
	}

	content := fmt.Sprintf(`[mysqld]
port=%d
datadir=%s
socket=%s\\mysql.sock
default-time-zone='+08:00'
character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci
init_connect='SET NAMES utf8mb4'
skip-name-resolve=1
explicit_defaults_for_timestamp=1

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
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	if !fileExists(i.mysqlExe) {
		return fmt.Errorf("MySQL executable not found: %s", i.mysqlExe)
	}

	i.status = core.StatusStarting

	// 数据目录不存在或为空时，先执行初始化，否则 mysqld 会立即退出
	if !dirExists(i.dataDir) || isEmptyDir(i.dataDir) {
		initCmd := &initCommand{mysqlExe: i.mysqlExe, dataDir: i.dataDir}
		if err := initCmd.run(); err != nil {
			i.status = core.StatusError
			return err
		}
	}

	// 确保日志目录存在（my.ini 中 log_error/slow_query_log_file 指向此处）
	os.MkdirAll(filepath.Join(i.mysqlDir, "logs"), 0755)

	if err := i.startServer(); err != nil {
		i.status = core.StatusError
		return err
	}

	// 确保 root@127.0.0.1 存在：修复 "Host '127.0.0.1' is not allowed to connect"，
	// 同时让 mysqladmin -h127.0.0.1 -uroot shutdown 的优雅关闭真正生效。
	// 引导期间会短暂停掉刚启动的实例，healed=true 时需要重新拉起。
	healed, err := mysqlutil.EnsureRootIPAccount(mysqlutil.Config{
		MySQLDir: i.mysqlDir,
		DataDir:  i.dataDir,
		Port:     i.port,
		Version:  i.version,
		MyIni:    i.configPath(),
	})
	if err != nil {
		i.status = core.StatusError
		return err
	}
	if healed {
		if err := i.startServer(); err != nil {
			i.status = core.StatusError
			return err
		}
	}

	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

// configPath 返回本实例实际使用的服务器配置文件（conf/my.ini 优先，其次 my.ini），均不存在时返回空串。
func (i *MySQLInstance) configPath() string {
	myiniPath := filepath.Join(i.mysqlDir, "conf", "my.ini")
	if !fileExists(myiniPath) {
		myiniPath = filepath.Join(i.mysqlDir, "my.ini")
	}
	if !fileExists(myiniPath) {
		return ""
	}
	return myiniPath
}

// startServer 启动正常 mysqld 实例并等待端口就绪，同步更新 i.pid。
func (i *MySQLInstance) startServer() error {
	// 端口被同 kind 其它版本的残留实例占用（如切换 MySQL 版本后旧实例仍在跑）：
	// 先尝试 mysqladmin 优雅关闭（强杀 mysqld 有 InnoDB 数据损坏风险），失败再强制清除；
	// 被外部程序占用则明确报错。必须在启动前处理，
	// 否则 mysqld 绑定失败退出，而旧代码会误把残留进程认领为本实例。
	graceful := func(port int) error {
		mysqladminExe := filepath.Join(i.mysqlDir, "bin", "mysqladmin.exe")
		if !fileExists(mysqladminExe) {
			return fmt.Errorf("mysqladmin 不存在: %s", mysqladminExe)
		}
		return runStopCommand(mysqladminExe, []string{
			"--host=127.0.0.1",
			fmt.Sprintf("--port=%d", port),
			"-u", "root",
			"shutdown",
		}, i.mysqlDir)
	}
	if err := ensurePortFree(i.port, kindDirOf(i.mysqlDir), 10*time.Second, graceful); err != nil {
		return err
	}

	args := []string{
		"--datadir=" + i.dataDir,
		"--port=" + fmt.Sprintf("%d", i.port),
	}
	if cp := i.configPath(); cp != "" {
		args = append([]string{"--defaults-file=" + cp}, args...)
	}

	pid, err := i.pm.Start("mysql", i.mysqlExe, args, nil, filepath.Dir(filepath.Dir(i.mysqlDir)))
	if err != nil {
		return err
	}

	// 冷启动/InnoDB 崩溃恢复可能较慢，最多等待 15 秒
	for j := 0; j < 30; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := ownedPIDByPort(i.port, i.mysqlDir); p > 0 {
			i.pid = p
			return nil
		}
	}

	// 只有本版本真正监听端口才算启动成功。
	i.pm.Stop(pid)
	if occ := process.FindPIDByPort(i.port); occ > 0 {
		return fmt.Errorf("MySQL 启动失败: 端口 %d 被 %s (PID %d) 占用", i.port, process.GetExecutablePath(occ), occ)
	}
	return fmt.Errorf("MySQL 启动失败: 进程未监听端口 %d，请查看 %s", i.port, filepath.Join(filepath.Dir(i.dataDir), "..", "logs"))
}

func (i *MySQLInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.status = core.StatusStopping

	// 收集属于本实例的进程：自管 pid，以及端口上属于本版本目录内的进程（只收集不杀）。
	targets := make(map[int]struct{})
	if i.pid > 0 {
		targets[i.pid] = struct{}{}
	}
	if i.pid <= 0 {
		if pid := ownedPIDByPort(i.port, i.mysqlDir); pid > 0 {
			targets[pid] = struct{}{}
		}
	}

	if len(targets) == 0 {
		// 本实例未运行，视为已停止
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 1. 优雅停止：mysqladmin --host=127.0.0.1 --port=<p> -u root shutdown。
	//    走 mysqld 正常关闭流程（flush buffer pool / 写 redo log / 关表），
	//    避免强杀跳过持久化导致数据损坏。root 为 --initialize-insecure 创建的无密码账户。
	//    忽略错误：用户改了 root 密码或网络异常时失败，由兜底强杀覆盖。
	mysqladminExe := filepath.Join(i.mysqlDir, "bin", "mysqladmin.exe")
	if fileExists(mysqladminExe) {
		_ = runStopCommand(mysqladminExe, []string{
			"--host=127.0.0.1",
			fmt.Sprintf("--port=%d", i.port),
			"-u", "root",
			"shutdown",
		}, i.mysqlDir)
	}

	// 2. 等待本实例进程退出（10s）。MySQL 正常关闭要 flush 脏页，比其它服务慢。
	//    判据=进程退出，不用端口是否释放。
	remaining := waitProcessesExited(targets, 10*time.Second)
	if len(remaining) == 0 {
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 3. 兜底强杀：pm.Stop 已是 taskkill /F /T。
	//    注意：到这一步说明优雅停止失败，数据可能有损坏风险，但服务必须停止。
	var lastErr error
	for pid := range remaining {
		if err := i.pm.Stop(pid); err != nil {
			lastErr = err
		}
	}
	if rem2 := waitProcessesExited(remaining, 3*time.Second); len(rem2) > 0 {
		return fmt.Errorf("MySQL 停止失败: 进程 %s 未退出（优雅停止超时后强制终止仍失败，可能权限不足）: %v", pidList(rem2), lastErr)
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
	if !i.pm.IsRunning(i.pid) {
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
	if err := os.MkdirAll(c.dataDir, 0755); err != nil {
		return fmt.Errorf("创建 MySQL 数据目录失败: %w", err)
	}
	args := []string{
		"--initialize-insecure",
		"--datadir=" + c.dataDir,
	}
	cmd := exec.Command(c.mysqlExe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("MySQL 数据目录初始化失败: %v\n%s", err, string(out))
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	return len(entries) == 0
}
