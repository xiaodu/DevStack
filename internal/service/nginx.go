package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
)

type NginxHandler struct{}

func (h *NginxHandler) Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error {
	installDir := cfg.Get().InstallDir
	nginxDir := filepath.Join(installDir, "programs", "nginx", s.Info.Version)

	confDir := filepath.Join(nginxDir, "conf")
	os.MkdirAll(confDir, 0755)

	confPath := filepath.Join(confDir, "nginx.conf")

	wwwRoot := filepath.ToSlash(filepath.Join(installDir, "www"))
	os.MkdirAll(filepath.Join(installDir, "www"), 0755)

	indexPath := filepath.Join(installDir, "www", "index.html")
	if !fileExists(indexPath) {
		os.WriteFile(indexPath, []byte(defaultIndexHTML), 0644)
	}

	settings := cfg.Get().NginxConfig
	gzipOn := "on"
	if !settings.GzipEnabled {
		gzipOn = "off"
	}
	serverTokensOn := "on"
	if !settings.ServerTokens {
		serverTokensOn = "off"
	}

	phpPorts := allocatedPHPBasePorts(resMgr)
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
		s.Info.Port, wwwRoot, wwwRoot, settings.StaticCacheDays, wwwRoot, wwwRoot)
	return os.WriteFile(confPath, []byte(content), 0644)
}

func (h *NginxHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	settings := cfg.Get()
	nc := settings.NginxConfig
	changed := false

	if v, ok := values["workerConnections"]; ok {
		if n, err := fmt.Sscanf(v, "%d", new(int)); err == nil && n == 1 {
			var wc int
			fmt.Sscanf(v, "%d", &wc)
			nc.WorkerConnections = wc
			changed = true
		}
	}
	if v, ok := values["clientMaxBodySize"]; ok {
		nc.ClientMaxBodySize = v
		changed = true
	}
	if v, ok := values["gzipEnabled"]; ok {
		nc.GzipEnabled = v == "true"
		changed = true
	}
	if v, ok := values["staticCacheDays"]; ok {
		var d int
		fmt.Sscanf(v, "%d", &d)
		nc.StaticCacheDays = d
		changed = true
	}
	if v, ok := values["keepaliveTimeout"]; ok {
		var t int
		fmt.Sscanf(v, "%d", &t)
		nc.KeepaliveTimeout = t
		changed = true
	}
	if v, ok := values["serverTokens"]; ok {
		nc.ServerTokens = v == "true"
		changed = true
	}

	if changed {
		settings.NginxConfig = nc
		if err := cfg.Update(settings); err != nil {
			return err
		}
	}

	return h.Configure(s, cfg, nil)
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
