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
		// 端口被其它程序占用且不属于本实例：不接管，避免误杀外部进程
		if process.FindPIDByPort(i.port) > 0 {
			break
		}
	}

	if i.pid <= 0 {
		if !i.pm.IsRunning(pid) {
			i.pm.Stop(pid)
			i.status = core.StatusError
			return fmt.Errorf("Redis start failed: port %d not responding or occupied by another process", i.port)
		}
		// 进程存活但尚未监听端口：跟踪自身进程
		i.pid = pid
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

	// 只停止属于本实例的进程：自管 pid，以及端口上可执行路径位于本版本目录内的进程。
	targets := make(map[int]struct{})
	if i.pid > 0 {
		targets[i.pid] = struct{}{}
	}
	if i.pid <= 0 {
		if pid := stopOwnedProcessByPort(i.port, i.redisDir); pid > 0 {
			targets[pid] = struct{}{}
		}
	}

	if len(targets) == 0 {
		// 本实例未运行，视为已停止
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	for pid := range targets {
		i.pm.Stop(pid)
	}

	// 停止成功的判定以本实例进程是否退出为准，而非端口是否释放
	if remaining := waitProcessesExited(targets, 5*time.Second); len(remaining) > 0 {
		return fmt.Errorf("Redis stop failed: process still running (pid %s)", pidList(remaining))
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
