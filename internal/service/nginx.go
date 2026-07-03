package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type NginxHandler struct{}

func (h *NginxHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	nginxDir := filepath.Join(installDir, "nginx", s.Info.Version)
	nginxExe := filepath.Join(nginxDir, "nginx.exe")

	if !fileExists(nginxExe) {
		return fmt.Errorf("Nginx 可执行文件未找到: %s", nginxExe)
	}

	s.Info.Status = core.StatusStarting

	pid, err := pm.Start("nginx", nginxExe, nil, nil, nginxDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	// Nginx master may exit after spawning workers on Windows.
	// Poll port until it's actually listening (up to ~3s) to confirm success.
	actualPid := pid
	for i := 0; i < 6; i++ {
		time.Sleep(500 * time.Millisecond)
		if p := process.FindPIDByPort(s.Info.Port); p > 0 {
			actualPid = p
			break
		}
	}

	// If neither master process nor worker port is alive, nginx failed to start.
	if !pm.IsRunning(pid) && process.FindPIDByPort(s.Info.Port) == 0 {
		pm.Stop(pid) // clean up zombie if any
		s.Info.Status = core.StatusError
		return fmt.Errorf("Nginx 启动失败：3 秒内端口 %d 无响应，请检查配置或端口是否被占用", s.Info.Port)
	}

	s.process = &execWrapper{
		pid:     actualPid,
		cmd:     nginxExe,
		started: now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = actualPid
	return nil
}

func (h *NginxHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	nginxDir := filepath.Join(installDir, "nginx", s.Info.Version)

	confDir := filepath.Join(nginxDir, "conf")
	os.MkdirAll(confDir, 0755)

	confPath := filepath.Join(confDir, "nginx.conf")

	wwwRoot := filepath.ToSlash(filepath.Join(installDir, "www"))
	os.MkdirAll(filepath.Join(installDir, "www"), 0755)

	indexPath := filepath.Join(installDir, "www", "index.html")
	if !fileExists(indexPath) {
		os.WriteFile(indexPath, []byte(defaultIndexHTML), 0644)
	}

	content := fmt.Sprintf(`worker_processes 1;
events {
	worker_connections 1024;
}
http {
	include mime.types;
	default_type application/octet-stream;
	sendfile on;
	keepalive_timeout 65;
	server {
		listen %d;
		server_name localhost;
		location / {
			root %s;
			index index.html index.htm index.php;
			try_files $uri $uri/ =404;
		}
		error_page 500 502 503 504 /50x.html;
		location = /50x.html {
			root %s;
		}
		location ~ \.php$ {
			root %s;
			fastcgi_pass 127.0.0.1:9000;
			fastcgi_index index.php;
			fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
			include fastcgi_params;
		}
	}
	include sites-enabled/*.conf;
}
`, s.Info.Port, wwwRoot, wwwRoot, wwwRoot)
	return os.WriteFile(confPath, []byte(content), 0644)
}

func (h *NginxHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	confPath := filepath.Join(installDir, "nginx", s.Info.Version, "conf", "nginx.conf")
	if !fileExists(confPath) {
		return h.Configure(s, cfg)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	content := string(data)
	for k, v := range values {
		content = updateIniValue(content, k, v)
	}
	return os.WriteFile(confPath, []byte(content), 0644)
}

func (h *NginxHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping
	if err := pm.Stop(s.process.pid); err != nil {
		s.Info.Status = core.StatusError
		return err
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
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
