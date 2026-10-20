package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type RedisProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	mu        sync.Mutex // 保护 instances 映射
	instances map[string]*RedisInstance
	instance  *RedisInstance // 最近一次访问的实例，供 Configure 等裸 API 使用
}

type RedisInstance struct {
	instanceGuard
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	port      int
	redisDir  string
	redisExe  string
	pm        *process.Manager
	startedAt time.Time
}

func NewRedisProvider(cfg *config.Config, pm *process.Manager) *RedisProvider {
	p := &RedisProvider{
		BaseProvider: NewBaseProvider(
			"redis",
			"",
			"高性能键值存储数据库",
			[]Capability{CapabilityCache},
		),
		cfg:       cfg,
		pm:        pm,
		instances: make(map[string]*RedisInstance),
	}
	MustRegister("redis", p, CapabilityCache)
	return p
}

func (p *RedisProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	version := "latest"
	if v, ok := config["version"].(string); ok {
		version = v
	}
	port := 6379
	if pt, ok := config["port"].(int); ok {
		port = pt
	}

	installDir := p.cfg.Get().InstallDir
	redisDir := filepath.Join(installDir, "programs", "redis", version)

	// 按版本复用活实例，避免 NewInstance 覆盖丢 PID（根因②）
	instanceID := "redis-" + version
	var instance *RedisInstance
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
	instance = &RedisInstance{
		id:       instanceID,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		redisDir: redisDir,
		redisExe: filepath.Join(redisDir, "redis-server.exe"),
		pm:       p.pm,
	}
	p.instances[instanceID] = instance
	p.instance = instance
	p.mu.Unlock()
	return instance, nil
}

func (p *RedisProvider) Install(ctx context.Context, version string) error {
	return nil
}

func (p *RedisProvider) Uninstall(ctx context.Context, version string) error {
	return nil
}

func (p *RedisProvider) IsInstalled(version string) bool {
	installDir := p.cfg.Get().InstallDir
	redisExe := filepath.Join(installDir, "programs", "redis", version, "redis-server.exe")
	_, err := os.Stat(redisExe)
	return err == nil
}

func (p *RedisProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	if p.instance == nil {
		return fmt.Errorf("Redis instance not initialized")
	}
	return p.applyConfig(p.instance)
}

func (p *RedisProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "port", Type: "int", Default: 6379, Description: "监听端口", Required: true},
			{Name: "maxmemory", Type: "string", Default: "", Description: "最大内存限制，如 256mb"},
		},
	}
}

func (p *RedisProvider) applyConfig(instance *RedisInstance) error {
	redisDir := instance.redisDir
	if err := os.MkdirAll(redisDir, 0755); err != nil {
		return err
	}
	confPath := filepath.Join(redisDir, "redis.conf")
	if fileExists(confPath) {
		return nil
	}
	conf := fmt.Sprintf("port %d\nbind 127.0.0.1\ndaemonize no\nloglevel notice\nlogfile \"redis.log\"\ndir \"%s\"\n", instance.port, redisDir)
	return os.WriteFile(confPath, []byte(conf), 0644)
}

func (i *RedisInstance) ID() string {
	return i.id
}

func (i *RedisInstance) Type() string {
	return "cache"
}

func (i *RedisInstance) Start(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	if !fileExists(i.redisExe) {
		return fmt.Errorf("Redis executable not found: %s", i.redisExe)
	}

	i.Lock()
	port := i.port
	i.Unlock()
	setStatus := func(s core.ServiceStatus) { i.withState(func() { i.status = s }) }
	setStatus(core.StatusStarting)

	confPath := filepath.Join(i.redisDir, "redis.conf")
	if !fileExists(confPath) {
		if err := os.MkdirAll(i.redisDir, 0755); err != nil {
			setStatus(core.StatusError)
			return err
		}
		conf := fmt.Sprintf("port %d\nbind 127.0.0.1\ndaemonize no\nloglevel notice\nlogfile \"redis.log\"\ndir \"%s\"\n", port, i.redisDir)
		if err := os.WriteFile(confPath, []byte(conf), 0644); err != nil {
			setStatus(core.StatusError)
			return err
		}
	}

	// 端口被同 kind 其它版本残留实例占用则自动清理；被外部程序占用则报错。
	// 传 graceful：redis-server 强杀会丢掉未持久化写入，能走 SHUTDOWN 就先走。
	graceful := func(ctx context.Context, p int) (stopOutcome, error) {
		return i.gracefulShutdown(ctx, p)
	}
	if err := ensurePortFree(ctx, port, i.redisDir, GraceRedis+10*time.Second, graceful); err != nil {
		setStatus(core.StatusError)
		return err
	}

	pid, err := i.pm.Start("redis", i.redisExe, []string{confPath}, nil, i.redisDir)
	if err != nil {
		setStatus(core.StatusError)
		return err
	}

	// 最多等 5s 监听就绪，但轮询可被 ctx 取消（旧实现用 time.Sleep，
	// 应用退出时只能干等完整个循环）。
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			i.pm.Stop(pid)
			setStatus(core.StatusError)
			return fmt.Errorf("Redis 启动被取消: %w", ctx.Err())
		case <-deadline:
			// 只有本版本真正监听端口才算启动成功。
			i.pm.Stop(pid)
			setStatus(core.StatusError)
			if occ := process.FindPIDByPort(port); occ > 0 {
				return fmt.Errorf("Redis 启动失败: 端口 %d 被 %s (PID %d) 占用", port, process.GetExecutablePath(occ), occ)
			}
			return fmt.Errorf("Redis 启动失败: 进程未监听端口 %d，请查看 %s\\redis.log", port, i.redisDir)
		case <-time.After(stopPollInterval):
			if p := ownedPIDByPort(port, i.redisDir); p > 0 {
				i.withState(func() { i.pid = p })
				setStatus(core.StatusRunning)
				i.withState(func() { i.startedAt = time.Now() })
				return nil
			}
		}
	}
}

// gracefulShutdown 用 redis-cli 触发 SHUTDOWN。
//
// 不带 SAVE/NOSAVE 参数是故意的：按 Redis 官方语义，裸 SHUTDOWN =
// “配了 save 规则就先落盘再退，没配则直接退”，这恰好尊重用户自己的配置；
// 而强制 SAVE 会在磁盘只读/满盘时把一次正常停止变成失败。
//
// 凭据用 REDISCLI_AUTH 环境变量传（redis-cli 官方推荐做法），而不是 -a：
// 后者会把密码敲进命令行，在进程列表与日志里裸奔。
//
// 不传 --connect-timeout 等新版选项：旧版 redis-cli 不认识会直接报 usage，
// 反而把一次能成功的关闭误判成失败；连接挂死由 runStopCommandEnv 自带的
// stopCmdTimeout 兜底。
func (i *RedisInstance) gracefulShutdown(ctx context.Context, port int) (stopOutcome, error) {
	cli := filepath.Join(i.redisDir, "redis-cli.exe")
	if !fileExists(cli) {
		return stopPermanent, fmt.Errorf("redis-cli.exe 不存在: %s", cli)
	}
	args := []string{"-h", "127.0.0.1", "-p", fmt.Sprintf("%d", port)}
	var env []string
	if pw := i.requirePass(); pw != "" {
		env = []string{"REDISCLI_AUTH=" + pw}
	}
	args = append(args, "shutdown")
	return runStopCommandEnv(ctx, cli, args, i.redisDir, env)
}

// requirePass 从 redis.conf 里读 requirepass（未设置则空串）。
// 旧实现从不传认证，一旦用户设了密码，优雅关闭永远失败而强杀成了默认路径，
// 且错误被 `_ =` 吞掉 —— 用户完全看不到。
func (i *RedisInstance) requirePass() string {
	data, err := os.ReadFile(filepath.Join(i.redisDir, "redis.conf"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "requirepass"); ok {
			return strings.Trim(strings.TrimSpace(rest), "\"'")
		}
	}
	return ""
}

func (i *RedisInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.clearStopNote()
	port, selfPID := i.snapshot()
	i.withState(func() { i.status = core.StatusStopping })

	// 收集属于本实例的进程（只收集不杀）：自管 PID ∪ 端口上属于本版本目录的全部 PID。
	// 只按自管 PID 停止会漏掉 DevStack 重启前遗留、或历史停止失败残留的同目录孤儿进程。
	targets := collectOwnedTargets(port, i.redisDir, selfPID)
	if len(targets) == 0 {
		i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
		return nil
	}

	res := runStopLadder(ctx, targets, []stopRung{
		// commandHaltRung：Redis 回复"拒绝关闭（保存失败）"时阶梯到此为止，
		// 绝不能升级到强杀 —— 那等于把用户数据打死。
		commandHaltRung("redis-cli shutdown", GraceRedis, func(ctx context.Context) (stopOutcome, error) {
			return i.gracefulShutdown(ctx, port)
		}),
		closeRung(graceAfterClose),
		forceKillRung(graceAfterKill),
	}, rescanOwned(port, i.redisDir))

	if res.Halted {
		i.withState(func() { i.status = core.StatusError })
		return fmt.Errorf("Redis 拒绝关闭（为保护数据，未强制终止）: %v。\n"+
			"常见原因：磁盘写入失败/空间不足，或 RDB 保存时被只读副本限制。\n"+
			"请查看 %s\\redis.log；若确认无需保留未持久化数据，可手动执行 redis-cli shutdown nosave",
			res.FirstErr, i.redisDir)
	}

	cause := "已尝试 SHUTDOWN、进程级关闭与整树强杀"
	if errors.Is(ctx.Err(), context.Canceled) {
		cause = "停止被取消（可能已退出应用）"
	}
	if len(res.Remaining) > 0 {
		i.withState(func() { i.status = core.StatusError })
		return formatLadderFailure("Redis", port, res, cause)
	}
	if rest := verifyStoppedByPort(ctx, port, i.redisDir, graceAfterKill); len(rest) > 0 {
		res.Remaining = pidSet(rest)
		i.withState(func() { i.status = core.StatusError })
		return formatLadderFailure("Redis", port, res, "端口上重新出现本实例进程")
	}

	// 状态按 Stopped 落账，异常靠 stopNote 上浮（见 types.go 里的契约说明）。
	if res.FirstErr != nil {
		i.setStopNote(fmt.Sprintf("Redis 未能优雅关闭（%v），已强制终止收尾；本次未落盘的写入可能丢失", res.FirstErr))
	}
	i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
	return nil
}

// snapshot 在状态锁保护下读取 port/pid（instanceGuard 不再长期持锁后必须）。
func (i *RedisInstance) snapshot() (port, pid int) {
	i.Lock()
	defer i.Unlock()
	return i.port, i.pid
}

func (i *RedisInstance) Status() (string, error) {
	i.Lock()
	defer i.Unlock()
	return string(i.status), nil
}

func (i *RedisInstance) HealthCheck(ctx context.Context) error {
	_, pid := i.snapshot()
	if pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(pid) {
		return fmt.Errorf("process not running")
	}
	return nil
}

func (i *RedisInstance) Configure(values map[string]interface{}) error {
	return nil
}

func (i *RedisInstance) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (i *RedisInstance) LogPaths() []string {
	return []string{
		filepath.Join(i.redisDir, "redis.log"),
	}
}

func (i *RedisInstance) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{}, nil
}
