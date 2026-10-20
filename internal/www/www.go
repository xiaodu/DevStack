// Package www 负责 DevStack 默认站点：生成默认首页与供首页读取的运行状态快照。
package www

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/atomicfile"
)

const (
	statusFileName  = "devstack-status.json"
	indexFileName   = "index.html"
	indexMarker     = "<!-- devstack-default-page -->"
	legacySignature = "DevStack &mdash; 默认站点根目录"
)

type Component struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	Installed bool   `json:"installed"`
}

type Project struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	URL     string `json:"url"`
	Root    string `json:"root"`
	Enabled bool   `json:"enabled"`
}

type Status struct {
	App          string      `json:"app"`
	UpdatedAt    string      `json:"updatedAt"`
	Environment  string      `json:"environment"`
	NginxVersion string      `json:"nginxVersion"`
	NginxStatus  string      `json:"nginxStatus"`
	Services     []Component `json:"services"`
	Runtimes     []Component `json:"runtimes"`
	Projects     []Project   `json:"projects"`
	WWWRoot      string      `json:"wwwRoot"`
}

func WriteStatus(installDir string, st Status) error {
	if installDir == "" {
		return nil
	}
	if st.App == "" {
		st.App = "DevStack"
	}
	if st.UpdatedAt == "" {
		st.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if st.Services == nil {
		st.Services = make([]Component, 0)
	}
	if st.Runtimes == nil {
		st.Runtimes = make([]Component, 0)
	}
	if st.Projects == nil {
		st.Projects = make([]Project, 0)
	}
	if st.WWWRoot == "" {
		st.WWWRoot = filepath.Join(installDir, "www")
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化站点状态失败: %w", err)
	}

	// 心跳、操作钩子与读接口可能并发触发写入；两个 goroutine 同时 replace
	// 同一目标在 Windows 上会偶发 Access denied，必须串行化。
	writeStatusMu.Lock()
	defer writeStatusMu.Unlock()
	return writeStatusFile(filepath.Join(installDir, "www", statusFileName), data)
}

var writeStatusMu sync.Mutex

// writeStatusFile 先走原子替换；nginx/杀毒软件可能短暂持有文件的读句柄，
// Windows 下 rename 会报 Access denied，重试几次后仍未成功则退回原地覆盖。
// 默认首页对 JSON 解析失败有兜底，退化写入不会让页面崩溃。
func writeStatusFile(path string, data []byte) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := atomicfile.WriteFile(path, data, 0644); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入站点状态失败: %w（原子替换最后错误: %v）", err, lastErr)
	}
	return nil
}

// EnsureIndex 写入默认首页。用户自定义的 index.html 不会被覆盖；
// 仅当文件缺失、带 DevStack 标记（旧版生成的页面）时才重写。
func EnsureIndex(installDir string) error {
	if installDir == "" {
		return nil
	}
	dir := filepath.Join(installDir, "www")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, indexFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		content := string(data)
		if !strings.Contains(content, indexMarker) && !strings.Contains(content, legacySignature) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.WriteFile(path, []byte(defaultIndexHTML), 0644)
}

const defaultIndexHTML = `<!DOCTYPE html>
<!-- devstack-default-page -->
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>DevStack · 默认站点</title>
<style>
:root {
  --bg: #f4f5fb;
  --card: #ffffff;
  --line: #e5e7f0;
  --text: #1f2430;
  --muted: #6b7280;
  --primary: #6366f1;
  --success: #16a34a;
  --warning: #d97706;
  --error: #dc2626;
}
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }
.header { background: linear-gradient(135deg, #4f46e5, #7c3aed); color: #fff; padding: 36px 24px 32px; }
.header-inner { max-width: 960px; margin: 0 auto; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.brand h1 { font-size: 28px; font-weight: 700; letter-spacing: -0.5px; }
.brand p { margin-top: 6px; font-size: 14px; opacity: .85; }
.badge { display: inline-flex; align-items: center; gap: 7px; background: rgba(255,255,255,.16); border: 1px solid rgba(255,255,255,.28); padding: 7px 13px; border-radius: 999px; font-size: 13px; font-weight: 600; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #4ade80; box-shadow: 0 0 0 3px rgba(74,222,128,.25); }
.container { max-width: 960px; margin: 0 auto; padding: 24px; display: flex; flex-direction: column; gap: 18px; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: 14px; overflow: hidden; box-shadow: 0 1px 2px rgba(16,24,40,.04); }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 15px 20px; border-bottom: 1px solid var(--line); }
.card-head h2 { font-size: 15px; font-weight: 600; }
.card-head .hint { font-size: 12px; color: var(--muted); }
.card-body { padding: 18px 20px; }
.info-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 14px; }
.info-item { background: #fafafe; border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; }
.info-label { font-size: 12px; color: var(--muted); }
.info-value { margin-top: 5px; font-size: 14px; font-weight: 600; font-family: ui-monospace, Consolas, monospace; word-break: break-all; }
.components { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 12px; }
.component { display: flex; align-items: center; gap: 11px; border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; background: #fafafe; }
.component-dot { width: 9px; height: 9px; border-radius: 50%; flex-shrink: 0; }
.component-main { min-width: 0; }
.component-name { font-size: 14px; font-weight: 600; }
.component-meta { margin-top: 3px; font-size: 12px; color: var(--muted); font-family: ui-monospace, Consolas, monospace; }
.status-running { background: var(--success); }
.status-starting, .status-stopping { background: var(--warning); }
.status-error { background: var(--error); }
.status-stopped { background: #9ca3af; }
.status-text-running { color: var(--success); }
.status-text-starting, .status-text-stopping { color: var(--warning); }
.status-text-error { color: var(--error); }
.status-text-stopped { color: var(--muted); }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 11px 20px; font-size: 13px; border-bottom: 1px solid var(--line); }
th { font-size: 12px; color: var(--muted); font-weight: 500; background: #fafafe; }
tr:last-child td { border-bottom: none; }
td.mono { font-family: ui-monospace, Consolas, monospace; font-size: 12px; color: #4b5563; word-break: break-all; }
td a { color: var(--primary); text-decoration: none; font-weight: 600; }
td a:hover { text-decoration: underline; }
.pill { display: inline-block; padding: 2px 9px; border-radius: 999px; font-size: 11px; font-weight: 600; }
.pill-on { background: #dcfce7; color: #166534; }
.pill-off { background: #f3f4f6; color: #6b7280; }
.empty { padding: 26px 12px; text-align: center; color: var(--muted); font-size: 13px; line-height: 1.9; }
.footer { text-align: center; padding: 8px 24px 32px; font-size: 12px; color: #9ca3af; line-height: 1.9; }
@media (max-width: 640px) {
  .header { padding: 26px 18px; }
  .container { padding: 16px; }
  th, td { padding: 9px 12px; }
}
</style>
</head>
<body>
<div class="header">
  <div class="header-inner">
    <div class="brand">
      <h1>DevStack</h1>
      <p>本地 Web 开发环境管理器 · 默认站点</p>
    </div>
    <span class="badge"><span class="dot"></span>Nginx 正在提供服务</span>
  </div>
</div>

<div class="container">
  <div class="card">
    <div class="card-head">
      <h2>当前环境</h2>
      <span class="hint" id="updated-at">正在读取状态...</span>
    </div>
    <div class="card-body">
      <div class="info-grid">
        <div class="info-item"><div class="info-label">激活环境</div><div class="info-value" id="env-name">-</div></div>
        <div class="info-item"><div class="info-label">Nginx</div><div class="info-value" id="nginx-info">-</div></div>
        <div class="info-item"><div class="info-label">默认站点根目录</div><div class="info-value" id="www-root">-</div></div>
        <div class="info-item"><div class="info-label">状态数据</div><div class="info-value" id="status-source">等待同步</div></div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-head">
      <h2>组件运行状态</h2>
      <span class="hint">数据由 DevStack 写入，本页每 5 秒刷新</span>
    </div>
    <div class="card-body">
      <div class="components" id="components">
        <div class="empty">正在加载组件状态...</div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-head">
      <h2>项目站点</h2>
      <span class="hint" id="project-count"></span>
    </div>
    <div id="projects">
      <div class="empty">正在加载项目列表...</div>
    </div>
  </div>
</div>

<div class="footer">
  DevStack · 默认站点由 Nginx 提供<br>
  要替换此页面，请修改 www 目录下的 index.html
</div>

<script>
(function () {
  var STATUS_URL = 'devstack-status.json';

  function esc(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  function statusLabel(s) {
    return ({ running: '运行中', stopped: '已停止', starting: '启动中', stopping: '停止中', error: '异常' })[s] || (s || '未知');
  }

  function statusClass(s) {
    return ['running', 'stopped', 'starting', 'stopping', 'error'].indexOf(s) >= 0 ? s : 'stopped';
  }

  function renderComponent(c) {
    var cls = statusClass(c.status);
    return '<div class="component">' +
      '<span class="component-dot status-' + cls + '"></span>' +
      '<div class="component-main">' +
        '<div class="component-name">' + esc(c.name || c.id) + '</div>' +
        '<div class="component-meta">v' + esc(c.version || '-') + ' · <span class="status-text-' + cls + '">' + statusLabel(c.status) + '</span></div>' +
      '</div>' +
    '</div>';
  }

  function renderComponents(data) {
    var target = document.getElementById('components');
    var list = (data.services || []).concat(data.runtimes || []);
    if (!list.length) {
      target.innerHTML = '<div class="empty">当前环境中没有已注册的组件<br>请在 DevStack 中激活一个环境模板</div>';
      return;
    }
    target.innerHTML = list.map(renderComponent).join('');
  }

  function renderProjects(data) {
    var target = document.getElementById('projects');
    var list = data.projects || [];
    document.getElementById('project-count').textContent = list.length ? list.length + ' 个站点' : '';
    if (!list.length) {
      target.innerHTML = '<div class="empty">还没有项目站点<br>在 DevStack 的「项目与网站」中创建后，会显示在这里</div>';
      return;
    }
    var rows = list.map(function (p) {
      return '<tr>' +
        '<td><strong>' + esc(p.name) + '</strong></td>' +
        '<td>' + (p.url ? '<a href="' + esc(p.url) + '" target="_blank" rel="noopener">' + esc(p.domain || p.url) + '</a>' : '<span style="color:#9ca3af">未配置域名</span>') + '</td>' +
        '<td class="mono">' + esc(p.type || '-') + '</td>' +
        '<td class="mono">' + esc(p.root || '-') + '</td>' +
        '<td><span class="pill ' + (p.enabled ? 'pill-on' : 'pill-off') + '">' + (p.enabled ? '已启用' : '已禁用') + '</span></td>' +
      '</tr>';
    }).join('');
    target.innerHTML = '<table><thead><tr><th>名称</th><th>访问地址</th><th>类型</th><th>根目录</th><th>状态</th></tr></thead><tbody>' + rows + '</tbody></table>';
  }

  function render(data) {
    document.getElementById('updated-at').textContent = data.updatedAt ? '状态更新于 ' + data.updatedAt : '状态时间未知';
    document.getElementById('env-name').textContent = data.environment || '未激活环境';
    var nginx = data.nginxVersion ? 'v' + data.nginxVersion : '-';
    if (data.nginxStatus) nginx += ' · ' + statusLabel(data.nginxStatus);
    document.getElementById('nginx-info').textContent = nginx;
    document.getElementById('www-root').textContent = data.wwwRoot || '-';
    document.getElementById('status-source').textContent = 'DevStack 实时同步';
    renderComponents(data);
    renderProjects(data);
  }

  function markUnavailable() {
    document.getElementById('updated-at').textContent = '未获取到状态数据';
    document.getElementById('env-name').textContent = 'DevStack 尚未启动或未激活环境';
    document.getElementById('nginx-info').textContent = 'Nginx 正在运行（当前页面由它提供）';
    document.getElementById('status-source').textContent = '不可用';
    document.getElementById('status-source').className = 'info-value status-text-error';
    document.getElementById('components').innerHTML = '<div class="empty">暂无状态数据<br>启动 DevStack 并激活环境后，这里会显示各组件的真实运行状态</div>';
    document.getElementById('projects').innerHTML = '<div class="empty">暂无项目数据</div>';
  }

  function refresh() {
    fetch(STATUS_URL + '?t=' + Date.now(), { cache: 'no-store' })
      .then(function (r) { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
      .then(function (data) {
        render(data);
        document.getElementById('status-source').className = 'info-value status-text-running';
      })
      .catch(markUnavailable);
  }

  refresh();
  setInterval(refresh, 5000);
})();
</script>
</body>
</html>`
