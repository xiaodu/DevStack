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

type RedisProvider struct {
	*BaseProvider
	cfg      *config.Config
	pm       *process.Manager
	instance *RedisInstance
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
		cfg: cfg,
		pm:  pm,
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

	instance := &RedisInstance{
		id:       "redis-" + version,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		redisDir: redisDir,
		redisExe: filepath.Join(redisDir, "redis-server.exe"),
		pm:       p.pm,
	}
	p.instance = instance
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

	i.status = core.StatusStarting

	confPath := filepath.Join(i.redisDir, "redis.conf")
	if !fileExists(confPath) {
		if err := os.MkdirAll(i.redisDir, 0755); err != nil {
			i.status = core.StatusError
			return err
		}
		conf := fmt.Sprintf("port %d\nbind 127.0.0.1\ndaemonize no\nloglevel notice\nlogfile \"redis.log\"\ndir \"%s\"\n", i.port, i.redisDir)
		if err := os.WriteFile(confPath, []byte(conf), 0644); err != nil {
			i.status = core.StatusError
			return err
		}
	}

	// 端口被同 kind 其它版本残留实例占用则自动清理；被外部程序占用则报错
	if err := ensurePortFree(i.port, kindDirOf(i.redisDir), 10*time.Second); err != nil {
		i.status = core.StatusError
		return err
	}

	pid, err := i.pm.Start("redis", i.redisExe, []string{confPath}, nil, i.redisDir)
	if err != nil {
		i.status = core.StatusError
		return err
	}

	for j := 0; j < 10; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := ownedPIDByPort(i.port, i.redisDir); p > 0 {
			i.pid = p
			break
		}
	}

	if i.pid <= 0 {
		// 只有本版本真正监听端口才算启动成功。
		i.pm.Stop(pid)
		i.status = core.StatusError
		if occ := process.FindPIDByPort(i.port); occ > 0 {
			return fmt.Errorf("Redis 启动失败: 端口 %d 被 %s (PID %d) 占用", i.port, process.GetExecutablePath(occ), occ)
		}
		return fmt.Errorf("Redis 启动失败: 进程未监听端口 %d，请查看 %s\\redis.log", i.port, i.redisDir)
	}

	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

func (i *RedisInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.status = core.StatusStopping

	// 收集属于本实例的进程（只收集不杀）：自管 PID ∪ 端口上属于本版本目录的全部 PID。
	// 只按自管 PID 停止会漏掉 DevStack 重启前遗留、或历史停止失败残留的同目录孤儿进程。
	targets := collectOwnedTargets(i.port, i.redisDir, i.pid)

	if len(targets) == 0 {
		// 本实例未运行，视为已停止
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 1. 优雅停止：redis-cli -p <port> shutdown。
	//    Redis 会触发 RDB save（若配置了 save 规则）后退出，避免强杀丢未持久化数据。
	//    忽略错误：redis-cli 不存在或网络异常时失败，由兜底强杀覆盖。
	redisCliExe := filepath.Join(i.redisDir, "redis-cli.exe")
	if fileExists(redisCliExe) {
		_ = runStopCommand(redisCliExe, []string{
			"-p", fmt.Sprintf("%d", i.port),
			"shutdown",
		}, i.redisDir)
	}

	// 2. 等待本实例进程退出（8s）。判据=进程退出，不用端口是否释放。
	remaining := waitProcessesExited(targets, 8*time.Second)
	if len(remaining) == 0 {
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 3. 兜底强杀：pm.Stop 已是 taskkill /F /T。
	var lastErr error
	for pid := range remaining {
		if err := i.pm.Stop(pid); err != nil {
			lastErr = err
		}
	}
	if rem2 := waitProcessesExited(remaining, 3*time.Second); len(rem2) > 0 {
		return fmt.Errorf("Redis 停止失败: 进程 %s 未退出（优雅停止超时后强制终止仍失败，可能权限不足）: %v", pidList(rem2), lastErr)
	}
	if rest := verifyStoppedByPort(i.port, i.redisDir, 3*time.Second); len(rest) > 0 {
		return fmt.Errorf("Redis 停止失败: 端口 %d 上仍有本实例进程 (PID %s) 未退出: %v", i.port, pidList(pidSet(rest)), lastErr)
	}

	i.pid = 0
	i.status = core.StatusStopped
	return nil
}

func (i *RedisInstance) Status() (string, error) {
	return string(i.status), nil
}

func (i *RedisInstance) HealthCheck(ctx context.Context) error {
	if i.pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(i.pid) {
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
