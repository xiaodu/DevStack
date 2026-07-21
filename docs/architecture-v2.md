# DevStack 架构 v2

## 1. 概念模型

DevStack 管理三层对象：

```
运行时 (Runtime)     — 你用什么开发
  PHP 8.3, PHP 7.4, Node 20, Python 3.12 …
  安装、切换版本

服务 (Service)      — 支撑你应用的基础设施
  Nginx (Web 服务器)
  MySQL (数据库)
  Redis (缓存)

项目 (Project)      — 你开发的应用
  类型: php / node / python / static
  入口: server.js / app.py / index.php / index.html
  运行时: 绑定一个 Runtime 版本
  依赖: 选择需要的 Service (MySQL, Redis)
  网络: 端口 + 可选域名 (Nginx 自动反代)
  生命周期: start / stop / restart
```

### 关键变化

- **运行时从 Service 中拆分出来** — PHP/Node/Python 不再是"服务"，而是"运行时"。下载、安装、切换版本属于 Runtime 管理。
- **Service 只保留基础设施** — Nginx、MySQL、Redis。
- **Project 是核心** — 绑定运行时 + 依赖服务 + 进程管理 + Nginx 集成，一站式。

---

## 2. 模块结构

```
internal/
├── runtime/          ← 新增（从 service 拆分）
│   ├── manager.go    — 版本安装、列表、激活
│   ├── types.go      — RuntimeInfo
│   └── registry.go   — 持久化
│
├── service/          ← 保留，只保留基础设施
│   ├── nginx.go
│   ├── mysql.go
│   └── redis.go
│   └── manager.go    — 去掉 PHP/Node/Python
│
├── project/          ← 新增
│   ├── manager.go    — CRUD + 生命周期
│   ├── run.go        — 各类型启动/停止
│   ├── types.go      — Project 结构
│   ├── nginx.go      — Nginx 虚拟主机生成（替代现有 site/）
│   ├── log.go        — 日志捕获与查看
│   ├── env.go        — 环境变量管理
│   ├── task.go       — 自定义任务/脚本
│   └── template.go   — 项目模板
│
├── process/          ← 现有，保留
│   ├── process.go    — Windows 进程管理
│   └── monitor.go    — 资源监控 (CPU/内存)
│
├── cert/             ← 新增
│   └── mkcert.go     — 本地 SSL 证书
│
├── config/           ← 现有，保留
│
└── core/             ← 现有，types 扩展

删除: internal/site/  — 功能合并到 project/nginx.go
```

---

## 3. 数据结构

### 3.1 `internal/runtime/types.go`

```go
type RuntimeType string

const (
    RuntimePHP   RuntimeType = "php"
    RuntimeNode  RuntimeType = "node"
    RuntimePython RuntimeType = "python"
)

type RuntimeInfo struct {
    ID        string       `json:"id"`
    Type      RuntimeType  `json:"type"`
    Version   string       `json:"version"`
    Path      string       `json:"path"`     // 安装目录
    Active    bool         `json:"active"`   // 当前激活版本
    Port      int          `json:"port"`     // PHP 特有，FastCGI 端口
    Installed bool         `json:"installed"`
}
```

### 3.2 `internal/service/types.go`

```go
const (
    TypeNginx  ServiceType = "nginx"
    TypeMySQL  ServiceType = "mysql"
    TypeRedis  ServiceType = "redis"
)
```

### 3.3 `internal/project/types.go`

```go
type ProjectType string

const (
    ProjectPHP    ProjectType = "php"
    ProjectNode   ProjectType = "node"
    ProjectPython ProjectType = "python"
    ProjectStatic ProjectType = "static"
)

// EnvVar 环境变量
type EnvVar struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

// ProjectTask 自定义任务
type ProjectTask struct {
    Name    string `json:"name"`    // "启动开发服务器"
    Command string `json:"command"` // "npm run dev"
    Cwd     string `json:"cwd"`     // 工作目录（默认项目根）
}

type Project struct {
    ID        string         `json:"id"`
    Name      string         `json:"name"`
    Type      ProjectType    `json:"type"`
    Path      string         `json:"path"`      // 项目目录
    Entry     string         `json:"entry"`     // 入口文件
    Port      int            `json:"port"`      // 应用端口
    Domain    string         `json:"domain"`    // 域名
    Runtime   string         `json:"runtime"`   // 运行时版本
    Depends   []string       `json:"depends"`   // 依赖服务
    SSL       bool           `json:"ssl"`       // 启用 HTTPS
    Env       []EnvVar       `json:"env"`       // 环境变量
    Tasks     []ProjectTask  `json:"tasks"`     // 自定义任务
    Status    string         `json:"status"`
    PID       int            `json:"pid"`
    Uptime    string         `json:"uptime"`
    AutoStart bool           `json:"autoStart"`
    CreatedAt time.Time      `json:"createdAt"`
}
```

---

## 4. 项目生命周期

### 4.1 各类型行为

项目类型决定了三件事：是否需要 Nginx、是否需要进程管理、访问方式。

```
PHP 项目:
  - 需要 Nginx 在前（生成 vhost）
  - 需要 php-cgi 系统服务在后台
  - 访问方式: http://{domain}
  - 项目本身"不启动进程"——PHP 由系统服务处理
  - 点击"启动"=检查 Nginx+php-cgi 都在运行 + 启用 vhost
  - 点击"停止"=禁用 vhost

Static 项目:
  - 需要 Nginx 在前（生成 vhost）
  - 无需后台进程
  - 访问方式: http://{domain}

Node 项目:
  - 无需 Nginx（直接访问端口）
  - 可选域名: 通过 Nginx 反代到项目端口
  - 启动: node {path}/{entry} → 监听 127.0.0.1:{port}
  - 停止: kill 进程

Python 项目:
  - 无需 Nginx（直接访问端口）
  - 可选域名: 通过 Nginx 反代到项目端口
  - 启动: python {path}/{entry}
    - 如果 entry=manage.py，自动追加 runserver 0.0.0.0:{port}
  - 停止: kill 进程
```

总结：

| 类型 | 进程管理 | Nginx |
|------|---------|-------|
| PHP | 不需要（靠系统 php-cgi） | 必须 |
| Static | 不需要 | 必须 |
| Node | 需要（管理 node 进程） | 可选（反代） |
| Python | 需要（管理 python 进程） | 可选（反代） |

### 4.2 停止

```
所有类型统一流程:
  1. 设置 Status = stopping
  2. 调用 process.Manager.Stop(pid) 终止进程
  3. 轮询 IsRunning() 最多 3 秒确认死亡
  4. 设置 Status = stopped, PID = 0
```

### 4.3 意外退出检测 + 自动重启

```
启动时启动 goroutine:

go func() {
    <-procMgr.WaitChan(pid)
    p.Status = "stopped"
    p.PID = 0
    if p.AutoStart {
        time.Sleep(2 * time.Second)
        m.Start(p.ID)
        // 记录操作日志：进程崩溃自动重启
    }
}()
```

---

## 5. Nginx 集成（project/nginx.go）

当 Project 填写了 Domain 字段时，执行三件事：

```
1. 写入 hosts 文件
   → 127.0.0.1  {domain}

2. 生成虚拟主机配置
   → {nginxDir}/conf/sites-enabled/{project.id}.conf

3. 执行 nginx -s reload（如果 Nginx 正在运行）
```

### Nginx 配置模板

```
PHP 项目:
  server {
      listen 80;
      server_name {domain};
      root {path};
      index index.php index.html;
      location / { try_files $uri $uri/ =404; }
      location ~ \.php$ {
          fastcgi_pass 127.0.0.1:{phpPort};
          fastcgi_index index.php;
          fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
          include fastcgi_params;
      }
  }
  (PHP 走 Nginx + php-cgi，项目本身不启进程)

Node/Python 项目（有域名）:
  server {
      listen 80;
      server_name {domain};
      location / {
          proxy_pass http://127.0.0.1:{port};
          proxy_set_header Host $host;
          proxy_set_header X-Real-IP $remote_addr;
          proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      }
  }

Static 项目:
  server {
      listen 80;
      server_name {domain};
      root {path};
      index index.html index.htm;
      location / { try_files $uri $uri/ =404; }
  }
```

### SSL 支持（cert/mkcert.go）

当 Project.SSL = true 时：

```
1. 检查本地 CA 是否已安装，未安装则自动安装
   mkcert -install

2. 为域名生成证书
   mkcert {domain}

3. Nginx vhost 增加 ssl 配置
   listen 443 ssl;
   ssl_certificate {domain}.pem;
   ssl_certificate_key {domain}-key.pem;
```

---

## 6. 环境变量管理（project/env.go）

每个项目可配置环境变量（类比 .env 文件）：

```
功能:
  - 以表格形式管理 key=value
  - 导出为 .env 文件（写入项目目录）
  - 导入 .env 文件
  - 启动时注入为进程环境变量

存储:
  项目持久化中的 Env 字段，不在项目目录（不污染用户代码）
  但提供导出按钮写入 .env
```

默认变量：
```
PATH={runtimeDir}:$PATH           # 确保使用正确的运行时版本
PROJECT_ROOT={project.Path}       # 项目根目录
PROJECT_PORT={project.Port}       # 监听端口
```

---

## 7. 自定义任务（project/task.go）

每个项目可定义多个任务（类比 package.json scripts）：

```
项目内置默认任务:
  Node:   dev: "node {entry}"
          install: "npm install"
          build: "npm run build"
          test: "npm test"

  Python: dev: "python {entry}"
          install: "pip install -r requirements.txt"
          migrate: "python manage.py migrate"

  PHP:    dev: "php -S ..."
          install: "composer install"
          migrate: "php artisan migrate"

用户可自定义:
  名称: "启动开发服务器"
  命令: "npm run dev"
  工作目录: （默认项目根，可修改）
```

执行方式：
```
启动项目 = 执行 dev 任务
点击任务按钮 = 打开终端窗口执行命令
任务输出在日志面板显示
```

---

## 8. 依赖管理（project/deps.go）

```
启动前检查流程:
  1. 读取 Project.Depends (["mysql", "redis"])
  2. 查询 service.Manager 检查对应服务状态
  3. 所有依赖服务运行中 → 启动项目
  4. 有服务未运行 → 弹出对话框：
     "MySQL 未运行，是否自动启动？ [是] [否] [取消启动]"

数据库管理（扩展功能）:
  - 项目绑定 MySQL 后，一键创建数据库
  - 运行迁移命令
  - 打开 phpMyAdmin 并自动选中该数据库
```

---

## 9. 日志（project/log.go）

```
每启动一个进程，自动捕获 stdout/stderr:

type ProjectLogEntry struct {
    Time    time.Time `json:"time"`
    Line    string    `json:"line"`
    Stream  string    `json:"stream"` // stdout / stderr
}

功能:
  - 日志分项目存储
  - 实时推送到前端（WebSocket 或轮询）
  - 日志行号 + 时间戳
  - 搜索/过滤
  - 保留最近 N 条（内存），定期清理
```

---

## 10. 端口管理

### 自动分配

```
新建项目时:
  - 检查端口是否被占用
  - 被占用则提示 + 建议可用端口
  - 支持一键分配空闲端口

PHP 项目 port 分配:
  - builtin 模式: 8000-8999
  - nginx 模式: 使用运行时 PHP 端口 (9000+)
  - 自动从安装的 PHP 版本获取 port

Node/Python:
  3000-3999 / 5000-5999
```

### 冲突检测

```
每次 List() 时:
  - 检查各项目端口是否被外部进程占用
  - 被占用但项目显示 running → 标记异常
  - 前端显示 ⚠️ 端口冲突
```

---

## 11. 仪表盘

```
┌──────────────────────────────────────────────────────────────┐
│  运行概况                                                      │
│  ┌─────────┬─────────┬─────────┬─────────┬─────────┐        │
│  │ 项目运行 │ 服务运行 │ 站点数   │ 磁盘     │ 内存     │        │
│  │ 2 / 3   │ 3 / 3   │ 2       │ 45%     │ 62%     │        │
│  └─────────┴─────────┴─────────┴─────────┴─────────┘        │
│                                                               │
│  ┌─── 项目（你的应用）─────── ─── ── ─ ─  ┐  ┌─── 服务 ──────┐│
│  │ ┌──────────────┐ ┌──────────────┐    │  │ Nginx  ● 80   ││
│  │ │ ERP          │ │ API          │    │  │ MySQL  ● 3306 ││
│  │ │ ● 运行中 12:34│ │ ● 运行中 5:12│    │  │ Redis  ○ 6379 ││
│  │ │ node 20      │ │ python 3.12  │    │  │              ││
│  │ │ 3001         │ │ 5000         │    │  └──────────────┘│
│  │ │ [停止] [打开] │ │ [停止] [打开] │    │                 │
│  │ ├──────────────┤ ├──────────────┤    │                 │
│  │ │ Blog         │ │ + 新建项目    │    │                 │
│  │ │ ○ 已停止      │ │              │    │                 │
│  │ │ php 8.3      │ │              │    │                 │
│  │ │ 8000         │ │              │    │                 │
│  │ │ [启动] [配置]  │ │              │    │                 │
│  │ └──────────────┘ └──────────────┘    │                 │
│  └──────────────────────────────────────┘                 │
│                                                               │
│  最近操作                                                      │
│  12:00  │ 启动    │ ERP     │ 成功    │                       │
│  11:55  │ 停止    │ Blog    │ 成功    │                       │
│  11:50  │ 启动    │ Nginx   │ 成功    │                       │
└──────────────────────────────────────────────────────────────┘
```

---

## 12. 页面结构

```
DevStack UI:

├── 仪表盘 (Dashboard)        ← 主界面，显示项目和服务的运行概况
├── 项目 (Projects)           ← 项目管理 CRUD
│   ├── 列表
│   └── 编辑（配置、Env、任务、日志）
├── 服务 (Services)           ← 基础设施管理
│   ├── Nginx (配置、日志、重启)
│   ├── MySQL (配置、数据库管理)
│   └── Redis (配置)
├── 运行时 (Runtimes)         ← 版本管理
│   ├── PHP (多版本、切换、扩展)
│   ├── Node (多版本、切换)
│   └── Python (多版本、切换)
├── 软件包 (Packages)         ← 下载安装
├── 日志 (Logs)               ← 统一日志查看
└── 设置 (Settings)
```

---

## 13. 实现策略

### 阶段一：基础设施

```
1. internal/runtime/     — 从 service 拆分出运行时版本管理
2. internal/project/     — 项目 CRUD + 生命周期
3. internal/service/     — 只保留 Nginx/MySQL/Redis
4. project/nginx.go      — 替代 site/ 的 Nginx 虚拟主机功能
```

### 阶段二：功能补全

```
5. project/log.go        — 日志捕获
6. project/env.go        — 环境变量
7. project/task.go       — 自定义任务
8. cert/mkcert.go        — SSL 证书
```

### 阶段三：前端

```
9. 新版仪表盘（项目卡片 + 服务小面板）
10. 项目管理页面
11. 运行时管理页面
```

### 阶段四：迁移

```
12. sites.json → Projects 迁移工具
13. php_registry.json → Runtimes 迁移
14. 旧 API 标记弃用
```

---

## 14. 向后兼容

```
保留的 API（不改签名）:
  GetServices(), StartService(), StopService(), RestartService()
  GetSettings(), SaveSettings()
  GetSystemInfo(), GetLogs(), GetRecentOps()

新增的 API:
  Project: Get/List/Create/Update/Delete/Start/Stop/Restart
  Runtime: Get/List/SetActive
  Task:    RunTask(id, taskName)
  Env:     Get/SetEnv(id, env), ExportEnv(id), ImportEnv(id)

弃用的 API（标记为 deprecated，保留不动）:
  Site 相关（GetSites, CreateSite, UpdateSite, DeleteSite, ToggleSite）
  PHP API（GetPHPExtensions, SavePHPExtensions, GetPHPRegistry）
```
