package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type MemcachedProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	mu        sync.Mutex
	instances map[string]*MemcachedInstance
	instance  *MemcachedInstance
}

type MemcachedInstance struct {
	instanceGuard
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	port      int
	dir       string
	exe       string
	pm        *process.Manager
	startedAt time.Time
}

func NewMemcachedProvider(cfg *config.Config, pm *process.Manager) *MemcachedProvider {
	p := &MemcachedProvider{
		BaseProvider: NewBaseProvider(
			"memcached",
			"",
			"高性能分布式内存对象缓存系统",
			[]Capability{CapabilityCache},
		),
		cfg:       cfg,
		pm:        pm,
		instances: make(map[string]*MemcachedInstance),
	}
	MustRegister("memcached", p, CapabilityCache)
	return p
}

func (p *MemcachedProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	version := "latest"
	if v, ok := config["version"].(string); ok && v != "" {
		version = v
	}
	port := 11211
	if pt, ok := config["port"].(int); ok && pt > 0 {
		port = pt
	}

	installDir := p.cfg.Get().InstallDir
	dir := filepath.Join(installDir, "programs", "memcached", version)

	// 按版本复用活实例，避免 NewInstance 覆盖已记录的 PID
	instanceID := "memcached-" + version
	var instance *MemcachedInstance
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
	instance = &MemcachedInstance{
		id:      instanceID,
		version: version,
		status:  core.StatusStopped,
		port:    port,
		dir:     dir,
		exe:     filepath.Join(dir, "memcached.exe"),
		pm:      p.pm,
	}
	p.instances[instanceID] = instance
	p.instance = instance
	p.mu.Unlock()
	return instance, nil
}

func (p *MemcachedProvider) IsInstalled(version string) bool {
	installDir := p.cfg.Get().InstallDir
	exe := filepath.Join(installDir, "programs", "memcached", version, "memcached.exe")
	_, err := os.Stat(exe)
	return err == nil
}

func (p *MemcachedProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	if p.instance == nil {
		return fmt.Errorf("Memcached instance not initialized")
	}
	return nil
}

func (p *MemcachedProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "port", Type: "int", Default: 11211, Description: "监听端口", Required: true},
		},
	}
}

func (i *MemcachedInstance) ID() string {
	return i.id
}

func (i *MemcachedInstance) Type() string {
	return "cache"
}

func (i *MemcachedInstance) Start(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	if !fileExists(i.exe) {
		return fmt.Errorf("Memcached executable not found: %s", i.exe)
	}

	port, pid := i.snapshot()
	setStatus := func(s core.ServiceStatus) { i.withState(func() { i.status = s }) }

	// 幂等：本实例已在运行则直接返回
	if pid > 0 && i.pm.IsRunning(pid) {
		setStatus(core.StatusRunning)
		return nil
	}

	// 端口已被本版本目录内的残留实例占用：接管而非再起一份
	if p := ownedPIDByPort(port, i.dir); p > 0 {
		i.withState(func() {
			i.pid = p
			i.status = core.StatusRunning
			i.startedAt = time.Now()
		})
		return nil
	}

	setStatus(core.StatusStarting)

	// memcached 没有应用层停止命令，端口残留只能进程级关闭/整树强杀
	if err := ensurePortFree(ctx, port, i.dir, graceAfterClose+graceAfterKill); err != nil {
		setStatus(core.StatusError)
		return err
	}

	args := []string{
		"-p", strconv.Itoa(port),
		"-l", "127.0.0.1",
		"-U", "0", // 关闭 UDP，避免同端口再开一个 UDP 监听造成归属判定混乱
	}
	newPID, err := i.pm.Start("memcached", i.exe, args, nil, i.dir)
	if err != nil {
		setStatus(core.StatusError)
		return err
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			i.pm.Stop(newPID)
			setStatus(core.StatusError)
			return fmt.Errorf("Memcached 启动被取消: %w", ctx.Err())
		case <-deadline:
			i.pm.Stop(newPID)
			setStatus(core.StatusError)
			if occ := process.FindPIDByPort(port); occ > 0 {
				return fmt.Errorf("Memcached 启动失败: 端口 %d 被 %s (PID %d) 占用", port, process.GetExecutablePath(occ), occ)
			}
			return fmt.Errorf("Memcached 启动失败: 进程未监听端口 %d", port)
		case <-time.After(stopPollInterval):
			if p := ownedPIDByPort(port, i.dir); p > 0 {
				i.withState(func() {
					i.pid = p
					i.status = core.StatusRunning
					i.startedAt = time.Now()
				})
				return nil
			}
		}
	}
}

func (i *MemcachedInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.clearStopNote()
	port, selfPID := i.snapshot()
	i.withState(func() { i.status = core.StatusStopping })

	targets := collectOwnedTargets(port, i.dir, selfPID)
	if len(targets) == 0 {
		i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
		return nil
	}

	res := runStopLadder(ctx, targets, []stopRung{
		closeRung(graceAfterClose),
		forceKillRung(graceAfterKill),
	}, rescanOwned(port, i.dir))

	if res.FirstErr != nil && len(res.Remaining) == 0 {
		i.setStopNote(fmt.Sprintf("Memcached 未能优雅关闭（%v），已强制终止收尾", res.FirstErr))
	}

	cause := "已尝试进程级关闭与整树强杀"
	if errors.Is(ctx.Err(), context.Canceled) {
		cause = "停止被取消（可能已退出应用）"
	}
	if len(res.Remaining) > 0 {
		i.withState(func() { i.pid = 0; i.status = core.StatusError })
		return formatLadderFailure("Memcached", port, res, cause)
	}
	if rest := verifyStoppedByPort(ctx, port, i.dir, graceAfterKill); len(rest) > 0 {
		res.Remaining = pidSet(rest)
		i.withState(func() { i.pid = 0; i.status = core.StatusError })
		return formatLadderFailure("Memcached", port, res, "端口上重新出现本实例进程")
	}

	i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
	return nil
}

func (i *MemcachedInstance) snapshot() (port, pid int) {
	i.Lock()
	defer i.Unlock()
	return i.port, i.pid
}

func (i *MemcachedInstance) Status() (string, error) {
	i.Lock()
	defer i.Unlock()
	return string(i.status), nil
}

func (i *MemcachedInstance) HealthCheck(ctx context.Context) error {
	_, pid := i.snapshot()
	if pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(pid) {
		return fmt.Errorf("process not running")
	}
	return nil
}

func (i *MemcachedInstance) Configure(values map[string]interface{}) error {
	return nil
}

func (i *MemcachedInstance) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (i *MemcachedInstance) LogPaths() []string {
	return nil
}

func (i *MemcachedInstance) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{}, nil
}
