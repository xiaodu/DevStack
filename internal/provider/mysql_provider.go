package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/mysqlutil"
	"github.com/xiaodu/DevStack/internal/process"
)

type MySQLProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	mu        sync.Mutex // 保护 instances 映射
	instances map[string]*MySQLInstance
	instance  *MySQLInstance // 最近一次访问的实例，供 Configure 等裸 API 使用
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
		cfg:       cfg,
		pm:        pm,
		instances: make(map[string]*MySQLInstance),
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

	// 按版本复用活实例，避免 NewInstance 覆盖丢 PID（根因②），与 PHPProvider 同一模式
	instanceID := "mysql-" + version
	var instance *MySQLInstance
	p.mu.Lock()
	if existing, ok := p.instances[instanceID]; ok {
		instance = existing
		p.mu.Unlock()
		instance.Lock()
		if instance.port != port {
			instance.port = port
		}
		instance.Unlock()
		p.instance = instance
		return instance, nil
	}
	instance = &MySQLInstance{
		id:       instanceID,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		mysqlDir: mysqlDir,
		mysqlExe: filepath.Join(mysqlDir, "bin", "mysqld.exe"),
		dataDir:  filepath.Join(installDir, "data", "mysql", version),
		pm:       p.pm,
	}
	p.instances[instanceID] = instance
	p.instance = instance
	p.mu.Unlock()
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

	port := i.snapshotPort()
	setStatus := func(s core.ServiceStatus) {
		i.withState(func() { i.status = s })
	}
	setStatus(core.StatusStarting)

	// 数据目录未初始化时先建系统表，否则 mysqld 会立即退出（版本策略见 mysqlutil）
	if err := mysqlutil.InitializeDataDir(mysqlutil.Config{
		MySQLDir: i.mysqlDir,
		DataDir:  i.dataDir,
		Version:  i.version,
		MyIni:    i.configPath(),
	}); err != nil {
		setStatus(core.StatusError)
		return err
	}

	// 确保日志目录存在（my.ini 中 log_error/slow_query_log_file 指向此处）
	os.MkdirAll(filepath.Join(i.mysqlDir, "logs"), 0755)

	if err := i.startServer(ctx, port); err != nil {
		setStatus(core.StatusError)
		return err
	}

	// 确保 root@127.0.0.1 存在：修复 "Host '127.0.0.1' is not allowed to connect"，
	// 同时让 mysqladmin --port=<p> -uroot shutdown 的优雅关闭真正生效。
	// 引导期间会短暂停掉刚启动的实例，healed=true 时需要重新拉起。
	healed, err := mysqlutil.EnsureRootIPAccount(mysqlutil.Config{
		MySQLDir: i.mysqlDir,
		DataDir:  i.dataDir,
		Port:     port,
		Version:  i.version,
		MyIni:    i.configPath(),
	})
	if err != nil {
		setStatus(core.StatusError)
		return err
	}
	if healed {
		if err := i.startServer(ctx, port); err != nil {
			setStatus(core.StatusError)
			return err
		}
	}

	i.withState(func() {
		i.status = core.StatusRunning
		i.startedAt = time.Now()
	})
	return nil
}

// snapshotPort 在状态锁保护下读取 port/pid。
// instanceGuard 不再长期持锁后，这两个字段会在 NewInstance 里被并发修改，
// 无锁读会产生数据竞争（且可能读到刷新中的旧值）。
func (i *MySQLInstance) snapshotPort() int {
	i.Lock()
	defer i.Unlock()
	return i.port
}

func (i *MySQLInstance) snapshot() (port, pid int) {
	i.Lock()
	defer i.Unlock()
	return i.port, i.pid
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
func (i *MySQLInstance) startServer(ctx context.Context, port int) error {
	// 端口被同 kind 其它版本的残留实例占用（如切换 MySQL 版本后旧实例仍在跑）：
	// 先尝试 mysqladmin 优雅关闭（强杀 mysqld 有 InnoDB 数据损坏风险），失败再强制清除；
	// 被外部程序占用则明确报错。必须在启动前处理，
	// 否则 mysqld 绑定失败退出，而旧代码会误把残留进程认领为本实例。
	graceful := func(ctx context.Context, p int) (stopOutcome, error) {
		return i.gracefulShutdown(ctx, p)
	}
	// 预算 = GraceMySQL + 15s 缓冲。旧实现写死 10s，而残留 mysqld 可能正在刷脏页，
	// 于是在它“正常关闭的中途”报“等待端口释放超时”，把本来会成功的启动打回去。
	if err := ensurePortFree(ctx, port, i.mysqlDir, GraceMySQL+15*time.Second, graceful); err != nil {
		return err
	}

	args := []string{
		"--datadir=" + i.dataDir,
		"--port=" + fmt.Sprintf("%d", port),
	}
	if cp := i.configPath(); cp != "" {
		args = append([]string{"--defaults-file=" + cp}, args...)
	}

	pid, err := i.pm.Start("mysql", i.mysqlExe, args, nil, filepath.Dir(filepath.Dir(i.mysqlDir)))
	if err != nil {
		return err
	}

	// 冷启动/InnoDB 崩溃恢复可能较慢，最多等待 15 秒；
	// 但必须可被 ctx 取消 —— 旧实现用 time.Sleep 写死轮询，应用退出时只能干等 15s。
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-ctx.Done():
			i.pm.Stop(pid)
			return fmt.Errorf("MySQL 启动被取消: %w", ctx.Err())
		case <-deadline:
			// 只有本版本真正监听端口才算启动成功。
			i.pm.Stop(pid)
			if occ := process.FindPIDByPort(port); occ > 0 {
				return fmt.Errorf("MySQL 启动失败: 端口 %d 被 %s (PID %d) 占用", port, process.GetExecutablePath(occ), occ)
			}
			return fmt.Errorf("MySQL 启动失败: 进程未监听端口 %d，请查看 %s", port, filepath.Join(filepath.Dir(i.dataDir), "..", "logs"))
		case <-time.After(100 * time.Millisecond):
			if p := ownedPIDByPort(port, i.mysqlDir); p > 0 {
				i.withState(func() { i.pid = p })
				return nil
			}
		}
	}
}

// stopRungs 组装 MySQL 的分级停止阶梯。
//
// 每级独立预算，依据见 stop_ladder.go 顶部的设计出处：
//
//	第 1 级 mysqladmin shutdown —— 唯一能让 InnoDB 走完 flush 的路径，
//	预算 GraceMySQL(45s)；认证被拒时不白等，立刻降级（SkipWaitWhenPermanent）。
//	第 2 级 进程级关闭 —— taskkill 不带 /F，对 mysqld 这类控制台进程通常直接
//	报错，据此马上降级；但对带窗口的包装进程（如某些启动器）是真关闭。
//	第 3 级 整树强杀 —— 跳过持久化，靠 pid-file 残留判定崩溃恢复风险。
func (i *MySQLInstance) stopRungs(port int) []stopRung {
	return []stopRung{
		commandRung("mysqladmin shutdown", GraceMySQL, func(ctx context.Context) (stopOutcome, error) {
			return i.gracefulShutdown(ctx, port)
		}),
		closeRung(graceAfterClose),
		forceKillRung(graceAfterKill),
	}
}

func (i *MySQLInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.clearStopNote()
	port, selfPID := i.snapshot()
	i.withState(func() { i.status = core.StatusStopping })

	// 收集属于本实例的进程：自管 pid ∪ 端口上属于本版本目录内的全部进程（只收集不杀）。
	// 只按自管 pid 停止会漏掉 DevStack 重启前遗留的同目录孤儿 mysqld。
	targets := collectOwnedTargets(port, i.mysqlDir, selfPID)
	if len(targets) == 0 {
		// 本实例未运行，视为已停止
		i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
		return nil
	}

	res := runStopLadder(ctx, targets, i.stopRungs(port), rescanOwned(port, i.mysqlDir))

	cause := "优雅关闭、进程级关闭、整树强杀均已尝试"
	if errors.Is(ctx.Err(), context.Canceled) {
		cause = "停止被取消（可能已退出应用）"
	}
	if len(res.Remaining) > 0 {
		// 阶梯走完仍有进程存活：绝不能谎报已停止，状态如实置错并带回完整轨迹。
		i.withState(func() { i.status = core.StatusError })
		return formatLadderFailure("MySQL", port, res, cause)
	}

	// 端口复核：进程全退出 ≠ 实例已停（同目录残留 master 会重新绑上端口）。
	if rest := verifyStoppedByPort(ctx, port, i.mysqlDir, graceAfterKill); len(rest) > 0 {
		res.Remaining = pidSet(rest)
		i.withState(func() { i.status = core.StatusError })
		return formatLadderFailure("MySQL", port, res, "端口上重新出现本实例进程")
	}

	// 走到这里进程确实全退了。优雅失败靠 stopNote 上浮，而不是返回 error：
	// service.Manager 的回退路径会把任何非 nil error 写成 StatusError，
	// 于是"已停但走过强杀"会被误报成"没停"（见 types.go instanceGuard.stopNote）。
	switch {
	case res.FirstOutcome == stopPermanent && res.FirstErr != nil:
		i.setStopNote(fmt.Sprintf("MySQL 优雅关闭被拒（%s），已强制终止收尾；若 root 密码被改过，可在 %s 放置 [client] 凭据文件恢复优雅停止",
			res.FirstErr, filepath.Join(i.mysqlDir, "run", ".devstack-shutdown.cnf")))
	case res.FirstErr != nil:
		i.setStopNote(fmt.Sprintf("MySQL 未能在 %s 内优雅关闭（%s），已强制终止收尾；下次启动 InnoDB 可能需做崩溃恢复，耗时略长",
			GraceMySQL, res.FirstErr))
	}
	if stray := i.pidFileLivePID(); stray > 0 {
		// pid-file 指向的 mysqld 仍存活（本目录内）：端口上看不见它，
		// 但它确实还在跑（如 --skip-networking 启动、或正在关闭中途）。
		i.setStopNote(fmt.Sprintf("端口 %d 已释放，但 pid-file 指向的 mysqld (PID %d) 仍存活，请手动确认", port, stray))
	}

	i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
	return nil
}

// gracefulShutdown 通过 mysqladmin 触发 mysqld 的正常关闭流程
// （flush buffer pool、写 redo、关表），避免强杀跳过持久化导致数据损坏。
//
// 三个取自成熟实现的要点：
//
//  1. 凭据走 --defaults-extra-file，而不是命令行明文 -p：
//     docker-library/mysql 的 `_mysql_passfile` 正是如此 —— 密码不进命令行
//     （进程列表/日志都会泄露），也不会触发交互式提示（一旦等输入，
//     mysqladmin 永不返回，等价于 Stop 卡死）。MySQL 要求该选项**必须是
//     命令行第一个**，否则被静默忽略。
//  2. --connect-timeout=3：mysqld 正在关闭时端口可能半开，默认连接超时可达
//     数十秒，会把整级预算白烧在连不上的服务上；连不上就该尽快降级。
//  3. 返回值按 stopOutcome 分类，调用方据此决定「继续等退出」还是「立刻降级」。
func (i *MySQLInstance) gracefulShutdown(ctx context.Context, port int) (stopOutcome, error) {
	mysqladmin := filepath.Join(i.mysqlDir, "bin", "mysqladmin.exe")
	if !fileExists(mysqladmin) {
		return stopPermanent, fmt.Errorf("mysqladmin.exe 不存在: %s", mysqladmin)
	}
	args := []string{
		fmt.Sprintf("--port=%d", port),
		"--host=127.0.0.1",
		"--connect-timeout=3",
	}
	if cred := i.shutdownCredFile(); cred != "" {
		// defaults-extra-file 必须置于首位，MySQL 才会应用其中的 [client] 凭据
		args = append([]string{"--defaults-extra-file=" + cred}, args...)
	} else {
		// root 为 --initialize-insecure 创建的无密码账户（DevStack 引导路径）
		args = append(args, "-u", "root")
	}
	args = append(args, "shutdown")
	return runStopCommandCtx(ctx, mysqladmin, args, i.mysqlDir)
}

// shutdownCredFile 返回优雅关闭用的凭据文件路径（不存在则空串）。
//
// DevStack **不持久化 root 密码**（service/mysql.go 执行 ALTER USER 后并不落盘），
// 因此这里不主动生成含密码的 cnf，只支持用户/运维自行放置：
//
//	[client]
//	user=root
//	password=***
//
// 放置后即可恢复"优雅关闭真正生效"，解决旧实现里"改过 root 密码 → 每次停止
// 都必然 Access denied → 每次都靠强杀"的结构性问题（而错误被 `_ =` 吞掉，
// 用户永远看不到）。
func (i *MySQLInstance) shutdownCredFile() string {
	for _, p := range []string{
		filepath.Join(i.mysqlDir, "run", ".devstack-shutdown.cnf"),
		filepath.Join(i.mysqlDir, ".devstack-shutdown.cnf"),
	} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// pidFile 返回 mysqld 的 PID 文件路径：my.ini 的 pid-file 指令优先，
// 否则按 Windows 默认 <datadir>\<hostname>.pid。
func (i *MySQLInstance) pidFile() string {
	if cp := i.configPath(); cp != "" {
		data, err := os.ReadFile(cp)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(strings.ToLower(line), "pid-file") {
					continue
				}
				eq := strings.IndexByte(line, '=')
				if eq < 0 {
					continue
				}
				val := strings.Trim(strings.TrimSpace(line[eq+1:]), "\"")
				if val != "" && filepath.IsAbs(val) {
					return val
				}
			}
		}
	}
	host := "localhost"
	if h, err := os.Hostname(); err == nil && h != "" {
		host = h
	}
	return filepath.Join(i.dataDir, host+".pid")
}

// pidFileLivePID 返回 pid-file 指向、且可执行文件确实在本实例目录内的存活 PID（否则 0）。
//
// 这是停止判据的第三路信息源：自管 PID 会因会话重启丢失，端口扫描看不见
// 未监听端口的 mysqld，而 pid-file 是 mysqld 自己写的第一手证据
// （正常关闭会删除、被强杀则残留）。必须校验可执行路径 —— PID 会被系统复用，
// 只凭"pid 号还活着"会把无关进程当成 MySQL。
func (i *MySQLInstance) pidFileLivePID() int {
	data, err := os.ReadFile(i.pidFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(firstLine(strings.TrimSpace(string(data)))))
	if err != nil || pid <= 0 || !processAlive(pid) {
		return 0
	}
	if !pathWithinDir(process.GetExecutablePath(pid), i.mysqlDir) {
		return 0
	}
	return pid
}

func (i *MySQLInstance) Status() (string, error) {
	i.Lock()
	defer i.Unlock()
	return string(i.status), nil
}

func (i *MySQLInstance) HealthCheck(ctx context.Context) error {
	_, pid := i.snapshot()
	if pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(pid) {
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
