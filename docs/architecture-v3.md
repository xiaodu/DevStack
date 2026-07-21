# DevStack 架构 v3

参考 ServBay 设计，统一概念。

## 1. 概念模型

DevStack 管理三类对象：

```
服务 (Service)       — 基础设施
  Nginx (Web 服务器)
  MySQL, MariaDB (数据库)
  Redis, Memcached (缓存)

运行时 (Runtime)     — 开发语言运行时
  PHP 8.3, PHP 8.2, PHP 7.4 … (每个版本独立 php-cgi 进程)
  Node 20, Node 18 …
  Python 3.12, 3.11 …

项目 (Project)       — 你的应用（统一概念）
  PHP 项目    → Nginx vhost + PHP-FPM (指定 PHP 版本)
  Node 项目   → Nginx 反代到用户自行启动的进程
  Python 项目  → Nginx 反代到用户自行启动的进程
  Static 项目  → Nginx vhost 托管静态文件
  Proxy 项目   → Nginx 反代到任意地址
```

### 关键原则

- **PHP 多版本**：每个 PHP 版本运行独立的 php-cgi 进程，各自一个端口
- **Node/Python 不管理进程**：和 ServBay 一致，用户在自己终端启动 dev server，DevStack 只配 Nginx 反代
- **统一"项目"**：所有类型都是项目，没有"站点"和"项目"的区分

---

## 2. 软件包清单

所有可安装软件包（含版本、下载地址、默认端口）统一维护在 `internal/registry/manifest.go`，
不在代码中散落硬编码。

```go
registry.Builtin().Get("nginx")    → *PackageDef（版本、URL、端口等）
registry.Builtin().List("runtime") → []PackageDef（只返回 PHP/Node/Python）
```

可通过 `{dataDir}/packages.json` 覆盖内置清单，实现自定义软件源。

## 3. 目录结构

```
{dataDir}/              (AppData/DevStack 或 ~/.devstack)
├── settings.json       用户设置
├── projects.json       用户项目
├── php_registry.json   PHP 版本端口映射
├── logs/
│   └── devstack.log    应用日志
└── certs/
    ├── server.crt      本地 SSL 证书
    └── server.key

{installDir}/           用户选择的安装目录
├── nginx/{version}/
├── mysql/{version}/
├── redis/{version}/
├── php/{version}/
├── node/{version}/
├── python/{version}/
└── www/                默认 Web 根目录
```

代码组织：

```
internal/
├── config/       保留
├── core/         扩展 types
├── download/     保留
├── process/      保留
├── log/          保留
│
├── service/      Nginx / MySQL / Redis 管理
│   ├── manager.go  (去掉 PHP/Node/Python)
│   ├── nginx.go
│   ├── mysql.go
│   └── redis.go
│
├── runtime/      新增 — 语言运行时多版本管理
│   ├── manager.go  — 统一 CRUD + 生命周期
│   ├── php.go      — PHP 多版本：启停、端口分配
│   ├── node.go     — Node 版本管理（仅检测状态）
│   ├── python.go   — Python 版本管理（仅检测状态）
│   └── types.go    — RuntimeInfo
│
└── project/      重构（从 site/ 扩展）
    ├── manager.go  — 项目 CRUD + Nginx 同步
    ├── nginx.go    — 各类型 Nginx vhost 配置生成
    ├── types.go    — Project 数据结构
    └── hosts.go    — hosts 管理
```

安装目录：

```
{installDir}/
├── nginx/
│   └── 1.26.0/
├── mysql/
│   └── 8.4.0/
├── redis/
│   └── 7.2.0/
├── php/             多版本
│   ├── 8.3.32/
│   ├── 8.2.20/
│   └── 7.4.33/
├── node/
│   └── 20.11.0/
├── python/
│   └── 3.12.0/
└── www/             项目目录
    ├── my-php-app/
    └── my-node-app/
```

---

## 3. 数据结构

### `internal/core/types.go` — 扩展

```go
type ProjectType string

const (
    ProjectPHP    ProjectType = "php"
    ProjectNode   ProjectType = "node"
    ProjectPython ProjectType = "python"
    ProjectStatic ProjectType = "static"
    ProjectProxy  ProjectType = "proxy"
)

type RuntimeType string

const (
    RuntimePHP   RuntimeType = "php"
    RuntimeNode  RuntimeType = "node"
    RuntimePython RuntimeType = "python"
)

type RuntimeInfo struct {
    ID        string       `json:"id"`        // "php-8.3"
    Type      RuntimeType  `json:"type"`
    Version   string       `json:"version"`
    Path      string       `json:"path"`
    Active    bool         `json:"active"`     // 命令行默认版本
    Running   bool         `json:"running"`    // php-cgi 是否运行 (PHP 特有)
    Port      int          `json:"port"`       // PHP 特有：FastCGI 端口
    Installed bool         `json:"installed"`
}

type Project struct {
    ID        string      `json:"id"`
    Name      string      `json:"name"`
    Type      ProjectType `json:"type"`

    Root      string      `json:"root"`      // 项目根目录（文档根）

    Domain    string      `json:"domain"`
    Port      int         `json:"port"`       // HTTP 监听端口 (默认 80)
    HTTPSPort int         `json:"httpsPort"`  // HTTPS 端口 (默认 443)
    SSL       bool        `json:"ssl"`

    // PHP 专用
    PHPVersion string     `json:"phpVersion,omitempty"` // 选用的 PHP 版本

    // Proxy 专用
    ProxyPass string      `json:"proxyPass,omitempty"`  // 反代目标地址
    // Node/Python 通用
    TargetPort int        `json:"targetPort,omitempty"` // Node/Python 进程端口

    Rewrite   string      `json:"rewrite,omitempty"`    // URL 重写规则
    Notes     string      `json:"notes,omitempty"`

    Enabled   bool        `json:"enabled"`
    CreatedAt string      `json:"createdAt"`
}
```

---

## 4. PHP 多版本管理

### 4.1 安装

每个版本独立目录：

```
{installDir}/php/
├── 8.3.32/       ← php.exe, php-cgi.exe, ext/, php.ini
├── 8.2.20/
└── 7.4.33/
```

### 4.2 启动

每个版本独立 php-cgi 进程，自动分配端口：

```
PHP 8.3 → php-cgi -b 127.0.0.1:9000     (端口 9000)
PHP 8.2 → php-cgi -b 127.0.0.1:9001     (端口 9001)
PHP 7.4 → php-cgi -b 127.0.0.1:9002     (端口 9002)
```

端口分配：
- Manager 维护 `map[string]int`：版本号 → 端口
- 端口范围 9000-9099
- 各版本 Worker 数量独立配置（默认 4）
- 停止时释放端口

### 4.3 生命周期 API

```go
runtime.Start("php-8.3")   → 启动 PHP 8.3 的 php-cgi (4 workers)
runtime.Stop("php-8.3")    → 停止 PHP 8.3 的所有 php-cgi 进程
runtime.Status("php-8.3")  → 检查 9000 端口是否监听 + 进程存活性
```

### 4.4 项目选择版本

```go
PHP 项目配置:
  PHPVersion = "8.3"
  → Nginx vhost 中:
    fastcgi_pass 127.0.0.1:9000;   // PHP 8.3 的端口
```

---

## 5. 项目类型与 Nginx 配置

### PHP 项目

```nginx
server {
    listen 80;
    server_name my-app.local;
    root E:/devstack/www/my-app;
    index index.php index.html;

    location / {
        try_files $uri $uri/ /index.php?$query_string;  # 或 Laravel/ThinkPHP 规则
    }

    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:9000;   # PHPVersion 对应的端口
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
}
```

### Node 项目（用户自行启动 node 进程）

用户终端：`node server.js`（监听 3001 端口）
DevStack 配 Nginx 反代：

```nginx
server {
    listen 80;
    server_name node-app.local;

    location / {
        proxy_pass http://127.0.0.1:3001;  # targetPort
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

### Python 项目（用户自行启动 python 进程）

用户终端：`python app.py`（监听 5000 端口）
DevStack 配 Nginx 反代：同上，proxy_pass 改为 `127.0.0.1:5000`

### Static 项目

```nginx
server {
    listen 80;
    server_name static-site.local;
    root E:/devstack/www/static-site;
    index index.html;
    location / { try_files $uri $uri/ =404; }
}
```

### Proxy 项目

```nginx
server {
    listen 80;
    server_name proxy-app.local;
    location / {
        proxy_pass http://127.0.0.1:3000;  # proxyPass
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

---

## 6. 向后兼容

### 现有 sites.json 迁移

当前 `sites.json` 中的每条记录（`core.Site`）：
- 全部转为 `core.Project`
- `Type` 字段已有，保留
- `PHPVersion` 字段已有，保留
- 新增 `TargetPort`：Node/Python 项目从 `Port + 1000` 公式推算

### 现有 service 迁移

- `service["php"]` → `runtime["php-8.3"]`（假设当前版本是 8.3）
- `service["node"]` → `runtime["node-20"]`
- `service["python"]` → `runtime["python-3.12"]`
- `service["nginx"]` → 保留
- `service["mysql"]` → 保留
- `service["redis"]` → 保留

---

## 7. 实现顺序

### Phase 1 — 数据结构 + 重命名

1. `internal/core/types.go` — 扩展：ProjectType、RuntimeInfo、Project 结构
2. 创建 `internal/runtime/` 目录，实现 RuntimeInfo 类型
3. 创建 `internal/project/` 目录，将 `internal/site/` 的代码搬过来，改为 Project 结构
4. `internal/service/` — 去掉 PHP/Node/Python 的 handler（或标记废弃）

### Phase 2 — PHP 多版本 + runtime

5. `internal/runtime/manager.go` — 统一管理所有语言版本
6. `internal/runtime/php.go` — PHP 多版本启停、端口分配、Worker 配置
7. `internal/runtime/node.go` — Node 版本检测状态
8. `internal/runtime/python.go` — Python 版本检测状态

### Phase 3 — Project 类型扩展

9. `internal/project/types.go` — 统一 Project 数据结构
10. `internal/project/nginx.go` — 各类型 Nginx 配置生成
11. `internal/project/manager.go` — CRUD + Nginx 同步

### Phase 4 — API 绑定

12. `app.go` — 绑定 Runtime API、Project API
13. 前端适配新接口

---

## 8. 仪表盘示意

```
┌──────────────────────────────────────────────────────────┐
│  运行概况                                                  │
│  ┌──────┬──────┬──────┬──────┐                           │
│  │ 项目  │ Nginx│ MySQL│ Redis│                           │
│  │ 3/5  │ ●    │ ●    │ ○    │                           │
│  └──────┴──────┴──────┴──────┘                           │
│                                                           │
│  项目（你的应用）                                            │
│  ┌─────────────────┬─────────────────┬─────────────────┐  │
│  │ ERP              │ API             │ Blog            │  │
│  │ ● Nginx vhost    │ ● Nginx 反代    │ ○ 已禁用         │  │
│  │ PHP 8.3          │ Node → :3001    │ PHP 7.4         │  │
│  │ erp.local        │ api.local       │ blog.local      │  │
│  │ [禁用]  [打开]    │ [禁用]  [打开]  │ [启用]           │  │
│  ├─────────────────┼─────────────────┼─────────────────┤  │
│  │ phpMy            │ + 新建项目       │                  │  │
│  │ ● Nginx vhost    │                  │                  │  │
│  │ PHP 8.3          │                  │                  │  │
│  │ pma.local        │                  │                  │  │
│  └─────────────────┴─────────────────┴─────────────────┘  │
│                                                           │
│  PHP 运行时                     服务                       │
│  PHP 8.3  ● 9000  [停止]         Nginx  ● 80             │
│  PHP 7.4  ○ 9001  [启动]         MySQL  ● 3306            │
│  PHP 8.2  ○ 9002  [启动]         Redis  ○ 6379  [启动]    │
│                                                           │
│  最近操作                                                  │
│  12:00  │ 启用  │ ERP      │ 成功                         │
│  11:55  │ 停止  │ PHP 7.4  │ 成功                         │
└──────────────────────────────────────────────────────────┘
```
