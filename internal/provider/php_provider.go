package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
)

type PHPProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	resMgr    *resource.Manager
	mu        sync.Mutex // 保护 instances 映射
	instances map[string]*PHPInstance
}

type PHPInstance struct {
	instanceGuard
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	port      int
	phpDir    string
	phpExe    string
	pm        *process.Manager
	startedAt time.Time
}

func NewPHPProvider(cfg *config.Config, pm *process.Manager, resMgr *resource.Manager) *PHPProvider {
	p := &PHPProvider{
		BaseProvider: NewBaseProvider(
			"php",
			"",
			"PHP 脚本语言运行时",
			[]Capability{CapabilityFPMRuntime},
		),
		cfg:       cfg,
		pm:        pm,
		resMgr:    resMgr,
		instances: make(map[string]*PHPInstance),
	}
	MustRegister("php", p, CapabilityFPMRuntime)
	return p
}

func (p *PHPProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	version := "8.3"
	if v, ok := config["version"].(string); ok {
		version = v
	}

	installDir := p.cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "programs", "php", version)

	instanceID := "php@" + version
	p.mu.Lock()
	if existing, ok := p.instances[instanceID]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	p.mu.Unlock()

	port := p.getPHPPort(version)

	instance := &PHPInstance{
		id:      instanceID,
		version: version,
		status:  core.StatusStopped,
		port:    port,
		phpDir:  phpDir,
		phpExe:  filepath.Join(phpDir, "php-cgi.exe"),
		pm:      p.pm,
	}
	p.mu.Lock()
	p.instances[instanceID] = instance
	p.mu.Unlock()
	return instance, nil
}

func (p *PHPProvider) getPHPPort(version string) int {
	if p.resMgr != nil {
		port, err := p.resMgr.Ports().Allocate("php:"+version, 9000)
		if err == nil {
			return port
		}
	}
	return 9000 + p.versionToIndex(version)
}

func (p *PHPProvider) versionToIndex(version string) int {
	switch version {
	case "8.0":
		return 0
	case "8.1":
		return 1
	case "8.2":
		return 2
	case "8.3":
		return 3
	case "8.4":
		return 4
	default:
		return 5
	}
}

func (p *PHPProvider) Install(ctx context.Context, version string) error {
	return nil
}

func (p *PHPProvider) Uninstall(ctx context.Context, version string) error {
	instanceID := "php@" + version
	p.mu.Lock()
	instance, ok := p.instances[instanceID]
	if ok {
		delete(p.instances, instanceID)
	}
	p.mu.Unlock()
	if !ok {
		return nil
	}

	instance.Stop(ctx)
	if p.resMgr != nil {
		p.resMgr.Ports().Release(instance.port, "php:"+version)
	}
	return nil
}

func (p *PHPProvider) IsInstalled(version string) bool {
	installDir := p.cfg.Get().InstallDir
	phpExe := filepath.Join(installDir, "programs", "php", version, "php-cgi.exe")
	_, err := os.Stat(phpExe)
	return err == nil
}

func (p *PHPProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	return nil
}

func (p *PHPProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "port", Type: "int", Default: 9000, Description: "FastCGI 监听端口"},
			{Name: "maxChildren", Type: "int", Default: 4, Description: "最大子进程数"},
			{Name: "memoryLimit", Type: "string", Default: "128M", Description: "内存限制"},
			{Name: "uploadMaxFilesize", Type: "string", Default: "2M", Description: "最大上传文件大小"},
		},
	}
}

func (p *PHPProvider) GetListenPort() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, inst := range p.instances {
		if inst.status == core.StatusRunning {
			return inst.port
		}
	}
	return 9000
}

func (p *PHPProvider) SetListenPort(port int) error {
	if p.resMgr != nil {
		_, err := p.resMgr.Ports().Allocate("php", port)
		return err
	}
	return nil
}

func (p *PHPProvider) Reload(ctx context.Context) error {
	p.mu.Lock()
	running := make([]*PHPInstance, 0, len(p.instances))
	for _, inst := range p.instances {
		if inst.status == core.StatusRunning {
			running = append(running, inst)
		}
	}
	p.mu.Unlock()

	for _, inst := range running {
		if err := inst.Stop(ctx); err != nil {
			return err
		}
		if err := inst.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *PHPProvider) CheckHealth(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, inst := range p.instances {
		if inst.status == core.StatusRunning {
			return inst.HealthCheck(ctx)
		}
	}
	return fmt.Errorf("no PHP instance running")
}

func (i *PHPInstance) ID() string {
	return i.id
}

func (i *PHPInstance) Type() string {
	return "fpm_runtime"
}

func (i *PHPInstance) Start(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	if !fileExists(i.phpExe) {
		return fmt.Errorf("PHP executable not found: %s", i.phpExe)
	}

	port, _ := i.snapshot()
	setStatus := func(s core.ServiceStatus) { i.withState(func() { i.status = s }) }
	setStatus(core.StatusStarting)

	// 端口被同 kind 其它版本残留实例占用则自动清理；被外部程序占用则报错。
	// php-cgi 无自带优雅停止命令（-b 模式下无任何控制接口），不传 graceful。
	if err := ensurePortFree(ctx, port, i.phpDir, graceAfterKill+graceAfterClose); err != nil {
		setStatus(core.StatusError)
		return err
	}

	args := []string{
		"-b", fmt.Sprintf("127.0.0.1:%d", port),
		"-c", filepath.Join(i.phpDir, "php.ini"),
	}

	pid, err := i.pm.Start(i.id, i.phpExe, args, nil, i.phpDir)
	if err != nil {
		setStatus(core.StatusError)
		return err
	}

	// 最多等 3s 监听就绪，轮询可被 ctx 取消（旧实现 6×500ms time.Sleep）。
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-ctx.Done():
			i.pm.Stop(pid)
			setStatus(core.StatusError)
			return fmt.Errorf("PHP 启动被取消: %w", ctx.Err())
		case <-deadline:
			// 只有本版本真正监听端口才算启动成功。
			i.pm.Stop(pid)
			setStatus(core.StatusError)
			if occ := process.FindPIDByPort(port); occ > 0 {
				return fmt.Errorf("PHP 启动失败: 端口 %d 被 %s (PID %d) 占用", port, process.GetExecutablePath(occ), occ)
			}
			return fmt.Errorf("PHP 启动失败: 进程未监听端口 %d，请检查 %s\\php.ini", port, i.phpDir)
		case <-time.After(stopPollInterval):
			if p := ownedPIDByPort(port, i.phpDir); p > 0 {
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

// snapshot 在状态锁保护下读取 port/pid。
func (i *PHPInstance) snapshot() (port, pid int) {
	i.Lock()
	defer i.Unlock()
	return i.port, i.pid
}

func (i *PHPInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.clearStopNote()
	port, selfPID := i.snapshot()
	i.withState(func() { i.status = core.StatusStopping })

	// 收集属于本实例的进程（只收集不杀）：自管 PID ∪ 端口上属于本版本目录的全部 PID。
	// php-cgi 常被手工/脚本多起几个实例，只按自管 PID 停止会在端口上留下同目录孤儿。
	targets := collectOwnedTargets(port, i.phpDir, selfPID)
	if len(targets) == 0 {
		i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
		return nil
	}

	// php-cgi 没有应用层停止命令，阶梯从进程级关闭起：
	// taskkill 不带 /F 先试（php-cgi 会收到 WM_CLOSE 并走正常退出），
	// 不行再整树强杀。旧实现只有一步 taskkill /F /T，请求处理中直接被打断。
	res := runStopLadder(ctx, targets, []stopRung{
		closeRung(graceAfterClose),
		forceKillRung(graceAfterKill),
	}, rescanOwned(port, i.phpDir))

	cause := "已尝试进程级关闭与整树强杀"
	if errors.Is(ctx.Err(), context.Canceled) {
		cause = "停止被取消（可能已退出应用）"
	}
	if len(res.Remaining) > 0 {
		i.withState(func() { i.pid = 0; i.status = core.StatusError })
		return formatLadderFailure("PHP", port, res, cause)
	}
	if rest := verifyStoppedByPort(ctx, port, i.phpDir, graceAfterKill); len(rest) > 0 {
		res.Remaining = pidSet(rest)
		i.withState(func() { i.pid = 0; i.status = core.StatusError })
		return formatLadderFailure("PHP", port, res, "端口上重新出现本实例进程")
	}

	i.withState(func() { i.pid = 0; i.status = core.StatusStopped })
	return nil
}

func (i *PHPInstance) Status() (string, error) {
	i.Lock()
	defer i.Unlock()
	return string(i.status), nil
}

func (i *PHPInstance) HealthCheck(ctx context.Context) error {
	_, pid := i.snapshot()
	if pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(pid) {
		return fmt.Errorf("process not running")
	}
	return nil
}

func (i *PHPInstance) Configure(values map[string]interface{}) error {
	return nil
}

func (i *PHPInstance) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (i *PHPInstance) LogPaths() []string {
	return []string{}
}

func (i *PHPInstance) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{}, nil
}
