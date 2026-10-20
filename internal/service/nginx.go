package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/www"
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

	_ = www.EnsureIndex(installDir)

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
