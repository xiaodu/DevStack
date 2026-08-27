package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
)

type NginxProvider struct {
	*BaseProvider
	cfg      *config.Config
	pm       *process.Manager
	resMgr   *resource.Manager
	instance *NginxInstance
	vhosts   []VhostConfig
}

type NginxInstance struct {
	instanceGuard
	id        string
	version   string
	status    core.ServiceStatus
	pid       int
	mpid      int
	port      int
	nginxDir  string
	nginxExe  string
	pm        *process.Manager
	startedAt time.Time
}

func NewNginxProvider(cfg *config.Config, pm *process.Manager, resMgr *resource.Manager) *NginxProvider {
	p := &NginxProvider{
		BaseProvider: NewBaseProvider(
			"nginx",
			"",
			"高性能 Web 服务器和反向代理",
			[]Capability{CapabilityWebServer},
		),
		cfg:    cfg,
		pm:     pm,
		resMgr: resMgr,
		vhosts: make([]VhostConfig, 0),
	}
	MustRegister("nginx", p, CapabilityWebServer)
	return p
}

func (p *NginxProvider) NewInstance(ctx context.Context, config map[string]interface{}) (ManagedEntity, error) {
	version := "latest"
	if v, ok := config["version"].(string); ok {
		version = v
	}
	port := 80
	if pt, ok := config["port"].(int); ok {
		port = pt
	}

	installDir := p.cfg.Get().InstallDir
	nginxDir := filepath.Join(installDir, "programs", "nginx", version)

	instance := &NginxInstance{
		id:       "nginx-" + version,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		nginxDir: nginxDir,
		nginxExe: filepath.Join(nginxDir, "nginx.exe"),
		pm:       p.pm,
	}
	p.instance = instance
	return instance, nil
}

func (p *NginxProvider) Install(ctx context.Context, version string) error {
	return nil
}

func (p *NginxProvider) Uninstall(ctx context.Context, version string) error {
	return nil
}

func (p *NginxProvider) IsInstalled(version string) bool {
	installDir := p.cfg.Get().InstallDir
	nginxExe := filepath.Join(installDir, "programs", "nginx", version, "nginx.exe")
	_, err := os.Stat(nginxExe)
	return err == nil
}

func (p *NginxProvider) Configure(ctx context.Context, values map[string]interface{}) error {
	if p.instance == nil {
		return fmt.Errorf("Nginx instance not initialized")
	}
	return p.applyConfig(p.instance)
}

func (p *NginxProvider) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "port", Type: "int", Default: 80, Description: "HTTP 监听端口", Required: true},
			{Name: "workerConnections", Type: "int", Default: 1024, Description: "工作连接数"},
			{Name: "clientMaxBodySize", Type: "string", Default: "20M", Description: "最大请求体大小"},
			{Name: "gzipEnabled", Type: "bool", Default: true, Description: "启用 Gzip 压缩"},
			{Name: "staticCacheDays", Type: "int", Default: 30, Description: "静态文件缓存天数"},
			{Name: "keepaliveTimeout", Type: "int", Default: 65, Description: "Keepalive 超时时间"},
			{Name: "serverTokens", Type: "bool", Default: false, Description: "显示服务器版本"},
		},
	}
}

func (p *NginxProvider) applyConfig(instance *NginxInstance) error {
	installDir := p.cfg.Get().InstallDir
	nginxDir := instance.nginxDir

	confDir := filepath.Join(nginxDir, "conf")
	os.MkdirAll(confDir, 0755)

	confPath := filepath.Join(confDir, "nginx.conf")

	wwwRoot := filepath.ToSlash(filepath.Join(installDir, "www"))
	os.MkdirAll(filepath.Join(installDir, "www"), 0755)

	indexPath := filepath.Join(installDir, "www", "index.html")
	if !fileExists(indexPath) {
		os.WriteFile(indexPath, []byte(defaultIndexHTML), 0644)
	}

	settings := p.cfg.Get().NginxConfig
	gzipOn := "on"
	if !settings.GzipEnabled {
		gzipOn = "off"
	}
	serverTokensOn := "on"
	if !settings.ServerTokens {
		serverTokensOn = "off"
	}

	phpPorts := allocatedPHPBasePorts(p.resMgr)
	phpUpstream := generatePHPUpstreamConfig(phpPorts)

	content := fmt.Sprintf(`worker_processes auto;
error_log logs/error.log;
pid logs/nginx.pid;

events {
	worker_connections %d;
	multi_accept on;
	use select;
}

http {
	include mime.types;
	default_type application/octet-stream;
	charset utf-8;

	sendfile on;
	tcp_nopush on;
	tcp_nodelay on;
	keepalive_timeout %d;
	keepalive_requests 1000;
	reset_timedout_connection on;

	access_log logs/access.log;

	server_tokens %s;
	client_max_body_size %s;
	client_body_buffer_size 128k;
	client_header_buffer_size 1k;
	large_client_header_buffers 4 16k;

	gzip %s;
	gzip_min_length 1k;
	gzip_buffers 4 16k;
	gzip_comp_level 5;
	gzip_types text/plain text/css text/xml text/javascript application/javascript application/json application/xml application/rss+xml application/atom+xml image/svg+xml;
	gzip_vary on;
	gzip_proxied any;
	gzip_disable "msie6";

	fastcgi_buffer_size 128k;
	fastcgi_buffers 8 256k;
	fastcgi_busy_buffers_size 256k;
	fastcgi_temp_file_write_size 256k;
	fastcgi_intercept_errors on;

	fastcgi_cache_path cache levels=1:2 keys_zone=phpcache:100m inactive=60m max_size=512m use_temp_path=off;
	fastcgi_cache_key "$scheme$request_method$host$request_uri";
	fastcgi_cache_use_stale error timeout invalid_header http_500;
	fastcgi_ignore_client_abort on;

	limit_req_zone $binary_remote_addr zone=php:10m rate=10r/s;
	limit_conn_zone $binary_remote_addr zone=conn:10m;

	%s

	server {
		listen %d default_server;
		server_name localhost _;

		add_header X-Frame-Options "SAMEORIGIN" always;
		add_header X-Content-Type-Options "nosniff" always;
		add_header X-XSS-Protection "1; mode=block" always;
		add_header Referrer-Policy "strict-origin-when-cross-origin" always;

		location / {
			root %s;
			index index.php index.html index.htm;
			try_files $uri $uri/ =404;
		}

		location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot|webp)$ {
			root %s;
			expires %dd;
			add_header Cache-Control "public, no-transform, immutable";
			access_log off;
			add_header X-Content-Type-Options "nosniff";
		}

		error_page 500 502 503 504 /50x.html;
		location = /50x.html {
			root %s;
		}

		location ~ \.php$ {
			root %s;
			fastcgi_pass php_backend;
			fastcgi_index index.php;
			fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
			include fastcgi_params;
			fastcgi_read_timeout 300s;

			limit_req zone=php burst=20 nodelay;
			limit_conn conn 10;
		}

		location ~ /\. {
			deny all;
			access_log off;
			log_not_found off;
		}
	}

	include sites-enabled/*.conf;
}
`, settings.WorkerConnections, settings.KeepaliveTimeout, serverTokensOn,
		strings.ToLower(settings.ClientMaxBodySize), gzipOn, phpUpstream,
		instance.port, wwwRoot, wwwRoot, settings.StaticCacheDays, wwwRoot, wwwRoot)

	return os.WriteFile(confPath, []byte(content), 0644)
}

func (p *NginxProvider) ServeHTTP(root string, upstream Upstream) error {
	vhost := VhostConfig{
		ID:        "auto-" + upstream.Name,
		Domain:    upstream.Name + ".local",
		Root:      root,
		Upstreams: []Upstream{upstream},
	}
	return p.AddVhost(vhost)
}

func (p *NginxProvider) Reload(ctx context.Context) error {
	if p.instance == nil {
		return fmt.Errorf("Nginx instance not initialized")
	}
	inst := p.instance
	inst.Lock()
	defer inst.Unlock()
	if inst.pid <= 0 {
		return fmt.Errorf("Nginx instance not running")
	}
	reloadCmd := &reloadCommand{nginxExe: inst.nginxExe, nginxDir: inst.nginxDir}
	return reloadCmd.run()
}

func (p *NginxProvider) AddVhost(config VhostConfig) error {
	for _, v := range p.vhosts {
		if v.ID == config.ID {
			return fmt.Errorf("vhost %s already exists", config.ID)
		}
	}
	p.vhosts = append(p.vhosts, config)
	return nil
}

func (p *NginxProvider) RemoveVhost(id string) error {
	for i, v := range p.vhosts {
		if v.ID == id {
			p.vhosts = append(p.vhosts[:i], p.vhosts[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("vhost %s not found", id)
}

func (p *NginxProvider) GetVhosts() []VhostConfig {
	return p.vhosts
}

func (i *NginxInstance) ID() string {
	return i.id
}

func (i *NginxInstance) Type() string {
	return "web_server"
}

func (i *NginxInstance) Start(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	if !fileExists(i.nginxExe) {
		return fmt.Errorf("Nginx executable not found: %s", i.nginxExe)
	}

	// 幂等：本实例已在运行则直接返回，避免重复拉起新实例
	if i.pid > 0 && i.pm.IsRunning(i.pid) {
		i.status = core.StatusRunning
		return nil
	}

	// 端口已被本实例版本目录内的 nginx 占用（如上次激活残留的实例）：接管而非再起一份
	if pid := ownedPIDByPort(i.port, i.nginxDir); pid > 0 {
		i.mpid = pid
		i.pid = pid
		i.status = core.StatusRunning
		i.startedAt = time.Now()
		return nil
	}

	i.status = core.StatusStarting

	// 端口被同 kind 其它版本的残留实例占用（如环境切换版本后旧版本孤儿仍在跑）：
	// 自动停掉它再启动；被外部程序占用则报错。否则新起的 nginx 会因绑定失败退出，
	// 而旧代码在进程存活窗口内误判成功，导致"激活成功但实际未运行"的假状态。
	if err := ensurePortFree(i.port, kindDirOf(i.nginxDir), 10*time.Second); err != nil {
		i.status = core.StatusError
		return err
	}

	pid, err := i.pm.Start("nginx", i.nginxExe, nil, nil, i.nginxDir)
	if err != nil {
		i.status = core.StatusError
		return err
	}

	for j := 0; j < 20; j++ {
		time.Sleep(500 * time.Millisecond)
		if p := ownedPIDByPort(i.port, i.nginxDir); p > 0 {
			i.pid = p
			break
		}
	}

	if i.pid <= 0 {
		// 只有本版本真正监听端口才算启动成功；其余一律明确失败。
		i.pm.Stop(pid)
		i.status = core.StatusError
		if occ := process.FindPIDByPort(i.port); occ > 0 {
			return fmt.Errorf("Nginx 启动失败: 端口 %d 被 %s (PID %d) 占用", i.port, process.GetExecutablePath(occ), occ)
		}
		return fmt.Errorf("Nginx 启动失败: 进程未监听端口 %d，请查看 %s\\logs\\error.log", i.port, filepath.Join(i.nginxDir, "logs"))
	}

	i.mpid = pid
	i.status = core.StatusRunning
	i.startedAt = time.Now()
	return nil
}

func (i *NginxInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.status = core.StatusStopping

	// 收集属于本实例的进程：自管 pid、主进程 mpid，以及端口上属于本版本目录内的进程。
	// 用 ownedPIDByPort（只收集 PID 不杀），避免旧的 stopOwnedProcessByPort 边收集边用新 Manager
	// 实例杀进程（与 i.pm 不是同一注册表，且重复杀）的设计缺陷。
	targets := make(map[int]struct{})
	if i.pid > 0 {
		targets[i.pid] = struct{}{}
	}
	if i.mpid > 0 && i.mpid != i.pid {
		targets[i.mpid] = struct{}{}
	}
	if i.pid <= 0 {
		if pid := ownedPIDByPort(i.port, i.nginxDir); pid > 0 {
			targets[pid] = struct{}{}
		}
	}

	if len(targets) == 0 {
		// 本实例未运行：端口可能被其它程序占用，与本实例无关，视为已停止
		i.pid = 0
		i.mpid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 1. 优雅停止：nginx -s stop（master 读 logs/nginx.pid 后通知 workers 优雅退出）。
	//    忽略错误：PID 文件丢失/指向旧 master 时会失败，由后续兜底强杀覆盖。
	//    工作目录设为 nginxDir，与 reloadCommand 一致，依赖 conf/nginx.conf 里的 pid 指令。
	if i.nginxExe != "" && fileExists(i.nginxExe) {
		_ = runStopCommand(i.nginxExe, []string{"-s", "stop"}, i.nginxDir)
	}

	// 2. 等待本实例进程退出（8s）。判据=进程退出，不用端口是否释放。
	remaining := waitProcessesExited(targets, 8*time.Second)
	if len(remaining) == 0 {
		i.pid = 0
		i.mpid = 0
		i.status = core.StatusStopped
		return nil
	}

	// 3. 兜底强杀：pm.Stop 已是 taskkill /F /T，递归杀子进程覆盖 worker 残留。
	var lastErr error
	for pid := range remaining {
		if err := i.pm.Stop(pid); err != nil {
			lastErr = err
		}
	}
	// 再等 3s 确认退出
	if rem2 := waitProcessesExited(remaining, 3*time.Second); len(rem2) > 0 {
		return fmt.Errorf("Nginx 停止失败: 进程 %s 未退出（优雅停止超时后强制终止仍失败，可能权限不足）: %v", pidList(rem2), lastErr)
	}

	i.pid = 0
	i.mpid = 0
	i.status = core.StatusStopped
	return nil
}

func (i *NginxInstance) Status() (string, error) {
	return string(i.status), nil
}

func (i *NginxInstance) HealthCheck(ctx context.Context) error {
	if i.pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(i.pid) {
		return fmt.Errorf("process not running")
	}
	return nil
}

func (i *NginxInstance) Configure(values map[string]interface{}) error {
	return nil
}

func (i *NginxInstance) ConfigSchema() ConfigSchema {
	return ConfigSchema{}
}

func (i *NginxInstance) LogPaths() []string {
	return []string{
		filepath.Join(i.nginxDir, "logs", "error.log"),
		filepath.Join(i.nginxDir, "logs", "access.log"),
	}
}

func (i *NginxInstance) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{}, nil
}

type reloadCommand struct {
	nginxExe string
	nginxDir string
}

func (c *reloadCommand) run() error {
	if c.nginxExe == "" {
		return fmt.Errorf("nginx executable path not set")
	}
	cmd := exec.Command(c.nginxExe, "-s", "reload")
	// -s reload 依赖工作目录下的 logs/nginx.pid，必须与 Start 的 workdir 保持一致
	cmd.Dir = c.nginxDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload failed: %w, output: %s", err, string(out))
	}
	return nil
}

// allocatedPHPBasePorts 返回资源池中所有已分配的 PHP 基端口（每个 PHP 版本一个基端口）。
// 排序保证生成的上游配置确定且稳定。
func allocatedPHPBasePorts(resMgr *resource.Manager) []int {
	if resMgr == nil {
		return nil
	}
	var ports []int
	for port, owner := range resMgr.Ports().ListAllocated() {
		if strings.HasPrefix(owner, "php:") {
			ports = append(ports, port)
		}
	}
	sort.Ints(ports)
	return ports
}

// generatePHPUpstreamConfig 为每个 PHP 基端口展开 php-cgi 工作进程端口（基端口起 4 个）。
// 若资源池无 PHP 端口则回退 9000，保证配置始终合法。
func generatePHPUpstreamConfig(basePorts []int) string {
	if len(basePorts) == 0 {
		basePorts = []int{9000}
	}
	var buf strings.Builder
	buf.WriteString("upstream php_backend {\n")
	for _, base := range basePorts {
		for i := 0; i < 4; i++ {
			fmt.Fprintf(&buf, "\tserver 127.0.0.1:%d;\n", base+i)
		}
	}
	buf.WriteString("\tkeepalive 32;\n}")
	return buf.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const defaultIndexHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>DevStack</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif; background: #f0f2f5; color: #333; min-height: 100vh; display: flex; flex-direction: column; }
.header { background: linear-gradient(135deg,#6366f1,#8b5cf6); color: #fff; padding: 40px 24px; text-align: center; }
.header h1 { font-size: 32px; font-weight: 700; letter-spacing: -0.5px; }
.header p { margin-top: 8px; opacity: .85; font-size: 15px; }
.container { max-width: 800px; margin: 0 auto; padding: 32px 24px; flex: 1; }
.card { background: #fff; border-radius: 10px; padding: 24px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,.08); }
.card h2 { font-size: 18px; font-weight: 600; margin-bottom: 12px; color: #111; }
.info-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #f0f0f0; font-size: 14px; }
.info-row:last-child { border-bottom: none; }
.info-label { color: #666; }
.info-value { font-family: monospace; color: #333; }
.actions { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 4px; }
.actions a { display: inline-flex; align-items: center; gap: 6px; padding: 10px 20px; border-radius: 8px; text-decoration: none; font-size: 14px; font-weight: 500; transition: all .15s; }
.actions a.primary { background: #6366f1; color: #fff; }
.actions a.primary:hover { background: #4f46e5; }
.actions a.secondary { background: #f0f0f0; color: #333; }
.actions a.secondary:hover { background: #e5e5e5; }
.footer { text-align: center; padding: 24px; font-size: 13px; color: #999; }
</style>
</head>
<body>
<div class="header">
<h1>DevStack</h1>
<p>本地 Web 开发环境管理器</p>
</div>
<div class="container">
<div class="card">
<h2>站点信息</h2>
<div class="info-row"><span class="info-label">服务器</span><span class="info-value">DevStack</span></div>
<div class="info-row"><span class="info-label">根目录</span><span class="info-value">www/</span></div>
<div class="info-row"><span class="info-label">主机</span><span class="info-value">localhost</span></div>
<div class="info-row"><span class="info-label">状态</span><span class="info-value" style="color:#22c55e">运行中</span></div>
</div>
<div class="card">
<h2>快速开始</h2>
<p style="font-size:14px;color:#666;margin-bottom:16px">将你的项目文件放入此目录，或通过 DevStack 面板创建站点。</p>
<div class="actions">
<a href="/" class="primary">打开站点</a>
<a href="/index.html" class="secondary">此页面</a>
</div>
</div>
</div>
<div class="footer">DevStack &mdash; 默认站点根目录</div>
</body>
</html>`
