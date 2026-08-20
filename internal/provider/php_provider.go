package provider

import (
	"context"
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

	i.status = core.StatusStarting

	args := []string{
		"-b", fmt.Sprintf("127.0.0.1:%d", i.port),
		"-c", filepath.Join(i.phpDir, "php.ini"),
	}

	pid, err := i.pm.Start(i.id, i.phpExe, args, nil, i.phpDir)
	if err != nil {
		i.status = core.StatusError
		return err
	}

	for j := 0; j < 6; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := ownedPIDByPort(i.port, i.phpDir); p > 0 {
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
			return fmt.Errorf("PHP start failed: port %d not responding or occupied by another process", i.port)
		}
		// 进程存活但尚未监听端口：跟踪自身进程
		i.pid = pid
	}

	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

func (i *PHPInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.status = core.StatusStopping

	// 收集属于本实例的进程（只收集不杀）。
	targets := make(map[int]struct{})
	if i.pid > 0 {
		targets[i.pid] = struct{}{}
	}
	if i.pid <= 0 {
		if pid := ownedPIDByPort(i.port, i.phpDir); pid > 0 {
			targets[pid] = struct{}{}
		}
	}

	if len(targets) == 0 {
		// 本实例未运行，视为已停止
		i.pid = 0
		i.status = core.StatusStopped
		return nil
	}

	// php-cgi（-b FastCGI 模式）无自带优雅停止命令，直接终止进程。
	// pm.Stop 已是 taskkill /F /T，递归杀子进程（php-cgi -b 模式通常单进程，/T 无害）。
	var lastErr error
	for pid := range targets {
		if err := i.pm.Stop(pid); err != nil {
			lastErr = err
		}
	}

	// 等待本实例进程退出（5s）。判据=进程退出，不用端口是否释放。
	if remaining := waitProcessesExited(targets, 5*time.Second); len(remaining) > 0 {
		return fmt.Errorf("PHP 停止失败: 进程 %s 未退出（强制终止仍失败，可能权限不足）: %v", pidList(remaining), lastErr)
	}

	i.pid = 0
	i.status = core.StatusStopped
	return nil
}

func (i *PHPInstance) Status() (string, error) {
	return string(i.status), nil
}

func (i *PHPInstance) HealthCheck(ctx context.Context) error {
	if i.pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(i.pid) {
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
