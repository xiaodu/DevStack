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
	"github.com/xiaodu/DevStack/internal/resource"
)

type PHPProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	resMgr    *resource.Manager
	instances map[string]*PHPInstance
}

type PHPInstance struct {
	id         string
	version    string
	status     core.ServiceStatus
	pid        int
	port       int
	phpDir     string
	phpExe     string
	startedAt  time.Time
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
	if existing, ok := p.instances[instanceID]; ok {
		return existing, nil
	}

	port := p.getPHPPort(version)

	instance := &PHPInstance{
		id:        instanceID,
		version:   version,
		status:    core.StatusStopped,
		port:      port,
		phpDir:    phpDir,
		phpExe:    filepath.Join(phpDir, "php-cgi.exe"),
	}
	p.instances[instanceID] = instance
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
	if instance, ok := p.instances[instanceID]; ok {
		instance.Stop(ctx)
		if p.resMgr != nil {
			p.resMgr.Ports().Release(instance.port)
		}
		delete(p.instances, instanceID)
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
	for _, inst := range p.instances {
		if inst.status == core.StatusRunning {
			inst.Stop(ctx)
			inst.Start(ctx)
		}
	}
	return nil
}

func (p *PHPProvider) CheckHealth(ctx context.Context) error {
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
	if !fileExists(i.phpExe) {
		return fmt.Errorf("PHP executable not found: %s", i.phpExe)
	}

	i.status = core.StatusStarting

	args := []string{
		"-b", fmt.Sprintf("127.0.0.1:%d", i.port),
		"-c", filepath.Join(i.phpDir, "php.ini"),
	}

	pid, err := process.NewManager().Start(i.id, i.phpExe, args, nil, i.phpDir)
	if err != nil {
		i.status = core.StatusError
		return err
	}

	for j := 0; j < 6; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := process.FindPIDByPort(i.port); p > 0 {
			i.pid = p
			break
		}
	}

	if i.pid <= 0 && !process.NewManager().IsRunning(pid) {
		process.NewManager().Stop(pid)
		i.status = core.StatusError
		return fmt.Errorf("PHP start failed: port %d not responding", i.port)
	}

	if i.pid <= 0 {
		i.pid = pid
	}

	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

func (i *PHPInstance) Stop(ctx context.Context) error {
	if i.pid <= 0 && process.FindPIDByPort(i.port) == 0 {
		i.status = core.StatusStopped
		return nil
	}
	i.status = core.StatusStopping

	pid := i.pid
	if pid <= 0 {
		pid = stopProcessByPort(i.port, "php-cgi.exe")
	}
	if pid > 0 {
		process.NewManager().Stop(pid)
	}

	killAllByName("php-cgi.exe")

	if !waitPortReleased(i.port, 3*time.Second) {
		return fmt.Errorf("PHP stop failed: port %d still listening", i.port)
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
	if !process.NewManager().IsRunning(i.pid) {
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