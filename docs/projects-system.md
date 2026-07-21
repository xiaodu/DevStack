# 项目管理系统设计

## 1. 概述

将"系统服务"和"用户项目"分离。新增 `internal/project/` 模块，管理用户的应用项目（Node、Python、PHP、静态站点等），与现有的 `internal/service/`（基础设施服务）和 `internal/site/`（Nginx 虚拟主机）并行。

### 分层关系

```
Service 层 (internal/service/)     — Nginx, MySQL, Redis, PHP, Node.js, Python 运行时
  ↓ 依赖
Site 层 (internal/site/)          — Nginx 虚拟主机配置（保留，不改）
  ↓ 并行
Project 层 (internal/project/)    — 用户应用项目（新增）
```

### Project vs Site 对比

| 维度 | Site | Project |
|------|------|---------|
| 本质 | Nginx 虚拟主机配置 | 应用进程管理 |
| 依赖 | 必须 Nginx 运行 | Node/Python 独立；PHP/Static 可选 Nginx |
| 进程管理 | 无 | Start/Stop/Restart |
| 端口 | Nginx 监听端口 | 应用直接监听端口 |
| 类型 | static, php, proxy | node, python, php, static |

---

## 2. 数据结构

### 2.1 `internal/project/types.go`

```go
package project

type ProjectType string

const (
    ProjectNode   ProjectType = "node"
    ProjectPython ProjectType = "python"
    ProjectPHP    ProjectType = "php"
    ProjectStatic ProjectType = "static"
)

type Project struct {
    ID        string       `json:"id"`
    Name      string       `json:"name"`
    Type      ProjectType  `json:"type"`
    Path      string       `json:"path"`      // 项目绝对路径
    Entry     string       `json:"entry"`     // 入口文件，如 server.js, app.py, index.php
    Port      int          `json:"port"`      // 应用监听端口
    Domain    string       `json:"domain"`    // 域名（可选，Nginx 反代用）
    Status    string       `json:"status"`    // running/stopped/error
    PID       int          `json:"pid"`
    Uptime    string       `json:"uptime"`
    Version   string       `json:"version"`   // 运行时版本
    AutoStart bool         `json:"autoStart"` // 是否随服务自启
    CreatedAt time.Time    `json:"createdAt"`
}
```

### 2.2 状态流转

```
stopped → starting → running
running → stopping → stopped
任何状态 → error（进程意外退出）
```

---

## 3. 启动方式

### 3.1 Node 项目

```
项目：/path/project
入口：server.js
命令：node /path/project/server.js
端口：动态分配或用户指定
工作目录：/path/project
```

启动命令：`node "<entry>"`（entry 为完整路径）
进程管理：`process.Manager.Start(id, "node", [entry], env, path)`

### 3.2 Python 项目

```
项目：/path/project
入口：app.py
命令：python /path/project/app.py
端口：动态分配或用户指定
工作目录：/path/project
```

启动命令：`python "<entry>"`
如果入口为 `manage.py` 自动追加 `runserver 0.0.0.0:PORT`

### 3.3 PHP 项目

```
项目：/path/project
入口：index.php（入口提示用）
命令：php -S 127.0.0.1:PORT -t /path/project
端口：用户指定
工作目录：/path/project
```

使用 PHP 内置服务器，无需 Nginx。端口需用户指定（默认 8000+）。

### 3.4 Static 项目

```
项目：/path/project
入口：index.html
命令：无需进程，生成 Nginx vhost
端口：Nginx 监听端口
```

仅生成 Nginx 虚拟主机配置。若 Nginx 未运行则标记但可手动启用。
是否需要 Nginx 前端代理该目录。

---

## 4. Manager API

### 4.1 `internal/project/manager.go`

```go
type Manager struct {
    mu       sync.RWMutex
    cfg      *config.Config
    procMgr  *process.Manager
    projects map[string]*Project
    filePath string
}

func NewManager(cfg *config.Config, procMgr *process.Manager) *Manager
func (m *Manager) List() []Project
func (m *Manager) Get(id string) (Project, error)
func (m *Manager) Create(p Project) (*Project, error)
func (m *Manager) Update(p Project) error
func (m *Manager) Delete(id string) error
func (m *Manager) Start(id string) error
func (m *Manager) Stop(id string) error
func (m *Manager) Restart(id string) error
```

### 4.2 持久化

存储路径：`dataDir/projects.json`

格式：
```json
[
  {
    "id": "proj_1712345678000",
    "name": "ERP 系统",
    "type": "node",
    "path": "E:\\www\\erp\\dmp_new",
    "entry": "server.js",
    "port": 3001,
    "domain": "erp.local",
    "status": "running",
    "pid": 12345,
    "uptime": "1h23m",
    "version": "20.11.0",
    "autoStart": false,
    "createdAt": "2025-04-01T10:00:00+08:00"
  }
]
```

### 4.3 ID 生成

`fmt.Sprintf("proj_%d", time.Now().UnixMilli())`

---

## 5. 运行引擎 (`internal/project/run.go`)

### 5.1 Start 逻辑

```go
func (m *Manager) Start(id string) error {
    p := m.projects[id]
    p.Status = "starting"

    switch p.Type {
    case ProjectNode:
        return m.startNode(p)
    case ProjectPython:
        return m.startPython(p)
    case ProjectPHP:
        return m.startPHP(p)
    case ProjectStatic:
        return m.startStatic(p)
    }
}

func (m *Manager) startNode(p *Project) error {
    entry := filepath.Join(p.Path, p.Entry)
    pid, err := m.procMgr.Start(p.ID, "node", []string{entry}, nil, p.Path)
    p.PID = pid
    p.Status = "running"
    return nil
}
```

### 5.2 Stop 逻辑

所有类型统一的停止方式：
1. 设置 `Status = "stopping"`
2. 调用 `m.procMgr.Stop(pid)`
3. 轮询 `IsRunning` 最多 3 秒
4. 设置 `Status = "stopped"`, `PID = 0`

### 5.3 进程意外退出检测

启动时启动 goroutine watch：
```go
go func() {
    <-m.procMgr.WaitChan(pid)
    p.Status = "stopped"
    p.PID = 0
    // 如果 AutoStart 则自动重启
}()
```

---

## 6. 后端 API（`app.go` 新增）

```go
func (a *App) GetProjects() []project.Project
func (a *App) CreateProject(p project.Project) (*project.Project, error)
func (a *App) UpdateProject(p project.Project) error
func (a *App) DeleteProject(id string) error
func (a *App) StartProject(id string) error
func (a *App) StopProject(id string) error
func (a *App) RestartProject(id string) error
```

`app.go` 新增 `projMgr *project.Manager` 字段。

### 初始化

```go
func NewApp() *App {
    // ... 已有代码 ...
    projMgr := project.NewManager(cfg, procMgr)
    return &App{
        // ... 已有字段 ...
        projMgr: projMgr,
    }
}
```

---

## 7. 前端

### 7.1 新增页面

`frontend/src/components/Projects.svelte` — 项目管理页面

布局参考 Sites.svelte（左右分栏）：
- 左侧：项目列表（搜索 + 过滤）
- 右侧：编辑表单 / 详情

### 7.2 前端 API 扩展

`frontend/src/lib/api.js` 新增：

```js
import {GetProjects, CreateProject, UpdateProject, DeleteProject, StartProject, StopProject, RestartProject} from '../../wailsjs/go/main/App.js'

export const api = {
    // ... 已有 ...
    getProjects: () => GetProjects(),
    createProject: (p) => CreateProject(p),
    updateProject: (p) => UpdateProject(p),
    deleteProject: (id) => DeleteProject(id),
    startProject: (id) => StartProject(id),
    stopProject: (id) => StopProject(id),
    restartProject: (id) => RestartProject(id),
}
```

### 7.3 路由

`App.svelte` 新增 `projects` 视图：

```svelte
import Projects from './components/Projects.svelte'

// 在 view 切换中：
projects: '项目',
```

Sidebar 新增项目菜单项。

### 7.4 项目表单字段

| 字段 | 控件 | 说明 |
|------|------|------|
| 名称 | input text | |
| 类型 | select | node / python / php / static |
| 项目目录 | input + browse button | 目录选择器 |
| 入口文件 | input text | server.js, app.py |
| 端口 | input number | 应用监听端口 |
| 域名 | input text | 可选，Nginx 反代用 |
| 版本 | select | 已安装的运行时版本 |
| 自动启动 | checkbox | 随服务自启 |

---

## 8. Nginx 集成

### 8.1 Static 项目

生成虚拟主机配置（复用 `internal/site/` 的 `syncNginx` 机制）：

```nginx
server {
    listen 80;
    server_name my-project.local;
    root E:/www/my-project;
    index index.html index.htm;
}
```

### 8.2 Node/Python/PHP 项目（可选）

如需通过域名访问，反向代理到应用端口：

```nginx
server {
    listen 80;
    server_name my-project.local;

    location / {
        proxy_pass http://127.0.0.1:3001;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

当用户填写 Domain 字段且 Nginx 已安装时，自动生成此配置。

### 8.3 hosts 文件管理

复用 `internal/site/hosts.go` 的 `ensureHostsEntry` 功能。

---

## 9. 与 Sites 系统的关系

### 9.1 保留 Sites 不动

Sites 系统完全是 Nginx 虚拟主机配置，和 Projects 是平行关系。用户可以选择：
- 用 Sites 管理纯 Nginx 虚拟主机（已有方式）
- 用 Projects 管理应用项目（新方式）
- 两边同时使用

### 9.2 可能的未来合并方向

如果 Sites 和 Projects 功能重叠过多，可以将来合并：
- Site 的 `php` 类型 → 对应 `ProjectPHP` + Nginx vhost
- Site 的 `static` 类型 → 对应 `ProjectStatic` + Nginx vhost
- Site 的 `proxy` 类型 → 对应 `ProjectNode`/`ProjectPython` + Nginx vhost

但目前保持分离，降低风险。

---

## 10. 实现顺序

1. `internal/project/types.go` — 数据结构
2. `internal/project/manager.go` — CRUD + 持久化
3. `internal/project/run.go` — Start/Stop/Restart
4. `app.go` — 注册 ProjectManager + API 绑定
5. `frontend/src/lib/api.js` — 前端 API
6. `frontend/src/components/Projects.svelte` — 前端页面
7. `App.svelte` — 注册视图 + 导航菜单
8. 测试 + 集成验证
