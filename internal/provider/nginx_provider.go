package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/www"
)

type NginxProvider struct {
	*BaseProvider
	cfg       *config.Config
	pm        *process.Manager
	resMgr    *resource.Manager
	mu        sync.Mutex // 保护 instances 映射
	instances map[string]*NginxInstance
	instance  *NginxInstance // 最近一次访问的实例，供 Configure/Reload 等裸 API 使用
	vhosts    []VhostConfig
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
		cfg:       cfg,
		pm:        pm,
		resMgr:    resMgr,
		vhosts:    make([]VhostConfig, 0),
		instances: make(map[string]*NginxInstance),
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

	// 按版本复用活实例：旧实现每次 NewInstance 都新建并覆盖 p.instance，
	// 导致 stopServiceByName 再查时拿到的是无 PID 的新对象，活进程丢失（根因②）。
	instanceID := "nginx-" + version
	var instance *NginxInstance
	p.mu.Lock()
	if existing, ok := p.instances[instanceID]; ok {
		instance = existing
		p.mu.Unlock()
		// 复用旧实例，但端口配置可能已改，同步到最新值
		instance.Lock()
		if instance.port != port {
			instance.port = port
		}
		instance.Unlock()
		p.instance = instance
		return instance, nil
	}
	instance = &NginxInstance{
		id:       instanceID,
		version:  version,
		status:   core.StatusStopped,
		port:     port,
		nginxDir: nginxDir,
		nginxExe: filepath.Join(nginxDir, "nginx.exe"),
		pm:       p.pm,
	}
	p.instances[instanceID] = instance
	p.instance = instance
	p.mu.Unlock()
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

	_ = www.EnsureIndex(installDir)

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

	port, pid, _ := i.snapshot()
	setStatus := func(s core.ServiceStatus) { i.withState(func() { i.status = s }) }

	// 幂等：本实例已在运行则直接返回，避免重复拉起新实例
	if pid > 0 && i.pm.IsRunning(pid) {
		setStatus(core.StatusRunning)
		return nil
	}

	// 端口已被本实例版本目录内的 nginx 占用（如上次激活残留的实例）：接管而非再起一份
	if p := ownedPIDByPort(port, i.nginxDir); p > 0 {
		i.withState(func() {
			i.mpid = p
			i.pid = p
			i.status = core.StatusRunning
			i.startedAt = time.Now()
		})
		return nil
	}

	setStatus(core.StatusStarting)

	// 端口被同 kind 其它版本的残留实例占用（如环境切换版本后旧版本孤儿仍在跑）：
	// 自动停掉它再启动；被外部程序占用则报错。否则新起的 nginx 会因绑定失败退出，
	// 而旧代码在进程存活窗口内误判成功，导致"激活成功但实际未运行"的假状态。
	// graceful 用 `-s stop`（而非 quit）：这里要清理的是上一会话的孤儿，
	// 目标是尽快拿回端口，不需要等它把手里的请求跑完。
	graceful := func(ctx context.Context, p int) (stopOutcome, error) {
		return i.signalShutdown(ctx, "stop")
	}
	if err := ensurePortFree(ctx, port, i.nginxDir, GraceNginx+10*time.Second, graceful); err != nil {
		setStatus(core.StatusError)
		return err
	}

	newPID, err := i.pm.Start("nginx", i.nginxExe, nil, nil, i.nginxDir)
	if err != nil {
		setStatus(core.StatusError)
		return err
	}

	// 最多等 10s 监听就绪，轮询可被 ctx 取消（旧实现 20×500ms time.Sleep，
	// 关闭应用时只能干等满 10s）。
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			i.pm.Stop(newPID)
			setStatus(core.StatusError)
			return fmt.Errorf("Nginx 启动被取消: %w", ctx.Err())
		case <-deadline:
			// 只有本版本真正监听端口才算启动成功；其余一律明确失败。
			i.pm.Stop(newPID)
			setStatus(core.StatusError)
			if occ := process.FindPIDByPort(port); occ > 0 {
				return fmt.Errorf("Nginx 启动失败: 端口 %d 被 %s (PID %d) 占用", port, process.GetExecutablePath(occ), occ)
			}
			return fmt.Errorf("Nginx 启动失败: 进程未监听端口 %d，请查看 %s\\logs\\error.log", port, filepath.Join(i.nginxDir, "logs"))
		case <-time.After(stopPollInterval):
			if p := ownedPIDByPort(port, i.nginxDir); p > 0 {
				i.withState(func() {
					i.pid = p
					i.mpid = newPID
					i.status = core.StatusRunning
					i.startedAt = time.Now()
				})
				return nil
			}
		}
	}
}

// signalShutdown 用 nginx.exe 自带的信号机制通知 master。
// level 取 "quit"（优雅：处理完当前请求再退）或 "stop"（立即关闭）。
//
// 工作目录必须是 nginxDir：pid 路径来自 conf/nginx.conf 的 `pid` 指令，
// 相对路径只在正确的前缀目录下才能解析（与 reloadCommand 一致）。
func (i *NginxInstance) signalShutdown(ctx context.Context, level string) (stopOutcome, error) {
	if i.nginxExe == "" || !fileExists(i.nginxExe) {
		return stopPermanent, fmt.Errorf("nginx.exe 不存在: %s", i.nginxExe)
	}
	return runStopCommandCtx(ctx, i.nginxExe, []string{"-s", level}, i.nginxDir)
}

// snapshot 在状态锁保护下读取 port/pid/mpid。
func (i *NginxInstance) snapshot() (port, pid, mpid int) {
	i.Lock()
	defer i.Unlock()
	return i.port, i.pid, i.mpid
}

func (i *NginxInstance) Stop(ctx context.Context) error {
	if err := i.begin(); err != nil {
		return err
	}
	defer i.end()

	i.clearStopNote()
	port, pid, mpid := i.snapshot()
	i.withState(func() { i.status = core.StatusStopping })

	// 收集属于本实例的进程：自管 pid、主进程 mpid，以及端口上属于本版本目录内的**全部**进程。
	// 必须无条件并入端口归属集合，而不是只在 i.pid<=0 时才查：
	// nginx 是 master/worker 结构，master 与每个 worker 都监听同一端口；且历史停止失败
	// 会在同版本目录上累积出多个孤儿 master（logs/nginx.pid 被最后一次启动覆盖，
	// `nginx -s stop` 只对最新那个 master 生效）。只按自管 pid 停止 = 永远停不干净。
	targets := collectOwnedTargets(port, i.nginxDir, pid, mpid)
	if len(targets) == 0 {
		// 本实例未运行：端口可能被其它程序占用，与本实例无关，视为已停止
		i.withState(func() { i.pid = 0; i.mpid = 0; i.status = core.StatusStopped })
		return nil
	}

	// 阶梯参照 nginx 自身的信号语义：quit（优雅等请求跑完）→ stop（立即）→
	// 进程级关闭 → 整树强杀。旧实现只有一步 `-s stop`，把"正在处理长请求"
	// 的 worker 直接打断（nginx 官方文档明确区分 quit/stop 两种语义）。
	// 预算取 GraceNginx=5s：桌面开发环境下请求秒级能完，超过即视为挂死请求。
	rungs := []stopRung{
		commandRung("nginx -s quit", GraceNginx, func(ctx context.Context) (stopOutcome, error) {
			return i.signalShutdown(ctx, "quit")
		}),
		commandRung("nginx -s stop", graceAfterClose, func(ctx context.Context) (stopOutcome, error) {
			return i.signalShutdown(ctx, "stop")
		}),
		closeRung(graceAfterClose),
		forceKillRung(graceAfterKill),
	}
	res := runStopLadder(ctx, targets, rungs, rescanOwned(port, i.nginxDir))

	cause := "已尝试 quit、stop、进程级关闭与整树强杀"
	if errors.Is(ctx.Err(), context.Canceled) {
		cause = "停止被取消（可能已退出应用）"
	}
	if len(res.Remaining) > 0 {
		i.withState(func() { i.pid = 0; i.mpid = 0; i.status = core.StatusError })
		return formatLadderFailure("Nginx", port, res, cause)
	}

	// 复核：端口上不再出现本版本目录的进程才算停止成功。
	// 自管 PID 全退出不足以判定停止 —— master 会重生 worker。
	if rest := verifyStoppedByPort(ctx, port, i.nginxDir, graceAfterKill); len(rest) > 0 {
		res.Remaining = pidSet(rest)
		i.withState(func() { i.pid = 0; i.mpid = 0; i.status = core.StatusError })
		return formatLadderFailure("Nginx", port, res, "端口上重新出现本实例进程（master 重生 worker）")
	}

	// 确实停了，但靠的是强杀：状态仍按 Stopped 落账，风险用 stopNote 上浮。
	if res.FirstErr != nil {
		i.setStopNote(fmt.Sprintf("Nginx 未能优雅退出（%v），已强制终止收尾；可能有请求被中断", res.FirstErr))
	}
	i.withState(func() { i.pid = 0; i.mpid = 0; i.status = core.StatusStopped })
	return nil
}

func (i *NginxInstance) Status() (string, error) {
	i.Lock()
	defer i.Unlock()
	return string(i.status), nil
}

func (i *NginxInstance) HealthCheck(ctx context.Context) error {
	_, pid, _ := i.snapshot()
	if pid <= 0 {
		return fmt.Errorf("not running")
	}
	if !i.pm.IsRunning(pid) {
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
