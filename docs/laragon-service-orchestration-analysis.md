# Laragon 服务编排机制分析与 DevStack 实现方案

## 一、Laragon 服务编排机制核心原理

### 1.1 核心原则

**Laragon 不使用 Windows 服务**。它有自己的"service orchestration"（服务编排），直接管理进程的生命周期。

### 1.2 技术实现

```
┌─────────────────────────────────────────────────┐
│              Laragon.exe (主进程)                 │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │         Service Orchestrator            │   │
│  │  - 读取 Procfile 定义服务               │   │
│  │  - 管理子进程生命周期                   │   │
│  │  - 端口冲突检测                         │   │
│  │  - 依赖顺序管理                         │   │
│  └─────────────────────────────────────────┘   │
│                      │                          │
│                      ▼                          │
│  ┌─────────────────────────────────────────┐   │
│  │         Child Processes                 │   │
│  │  - nginx.exe (子进程)                   │   │
│  │  - mysqld.exe (子进程)                  │   │
│  │  - php-cgi.exe (子进程)                 │   │
│  └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

### 1.3 关键设计

| 设计点 | Laragon 实现 | 效果 |
|--------|-------------|------|
| **不使用 Windows 服务** | 自己管理进程 | 便携、隔离、快速 |
| **子进程模式** | nginx/mysql/php 作为子进程 | 退出时自动清理 |
| **Procfile 定义服务** | 声明式服务定义 | 灵活、可扩展 |
| **异步非阻塞** | 启动/停止不阻塞 UI | 响应快 |

### 1.4 Procfile 格式

```ini
; 服务名: autorun 命令 参数 环境变量 工作目录

; 基本格式
; Program Name: COMMANDS

; 带 autorun（自动启动）
; Program Name: autorun COMMANDS

; 带环境变量
; Program Name: COMMANDS ENV_VAR1=VALUE ENV_VAR2="VALUE WITH SPACE"

; 带工作目录
; Program Name: COMMANDS PWD=path\to\workdir

; 带环境文件
; Program Name: COMMANDS env_file=path\to\.env

; 静默运行（无控制台窗口）
; Program Name: "COMMANDS"
```

**示例**：
```ini
; Nginx: nginx.exe -p C:\laragon\bin\nginx
; MySQL: mysqld.exe --defaults-file=C:\laragon\bin\mysql\my.ini
; PHP: php-cgi.exe -b 127.0.0.1:9000
; Redis: redis-server.exe
; Node: autorun node start PORT=3000 PWD=C:\laragon\www\myapp
```

---

## 二、DevStack 当前实现分析

### 2.1 当前架构

```
┌─────────────────────────────────────────────────┐
│              DevStack (Wails App)                │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │         service.Manager                 │   │
│  │  - 管理服务状态                         │   │
│  │  - 依赖管理                             │   │
│  └─────────────────────────────────────────┘   │
│                      │                          │
│                      ▼                          │
│  ┌─────────────────────────────────────────┐   │
│  │         provider.*Provider              │   │
│  │  - NginxProvider                        │   │
│  │  - MySQLProvider                        │   │
│  │  - PHPProvider                          │   │
│  └─────────────────────────────────────────┘   │
│                      │                          │
│                      ▼                          │
│  ┌─────────────────────────────────────────┐   │
│  │         process.Manager                 │   │
│  │  - exec.Command 启动进程                │   │
│  │  - taskkill /F /T 停止进程              │   │
│  │  - netstat -ano 查找 PID               │   │
│  └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

### 2.2 与 Laragon 的差距

| 维度 | Laragon | DevStack | 差距 |
|------|---------|----------|------|
| **进程管理** | 子进程模式，父进程退出自动清理 | exec.Command + taskkill 强杀 | 需要改进 |
| **服务定义** | Procfile 声明式 | 代码硬编码 | 需要改进 |
| **端口管理** | 自动检测，用户可配置 | 自动分配（PortPool） | 基本一致 |
| **依赖管理** | 启动顺序管理 | 静态 rank 排序 | 需要改进 |
| **进程树管理** | Windows Job Object（内核级） | taskkill /T（用户级） | 需要改进 |
| **便携性** | 整个文件夹可移动 | 依赖系统环境 | 需要改进 |

---

## 三、DevStack 实现方案

### 3.1 核心目标

实现类似 Laragon 的**自定义服务编排**，核心是：
1. 不依赖 Windows 服务
2. 使用子进程模式管理服务
3. 支持 Procfile 声明式服务定义
4. 实现进程树管理（Windows Job Object）

### 3.2 架构设计

```
┌─────────────────────────────────────────────────┐
│              DevStack (Wails App)                │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │         orchestrator.Engine             │   │
│  │  - 读取 Procfile                        │   │
│  │  - 管理服务生命周期                     │   │
│  │  - 端口冲突检测                         │   │
│  │  - 依赖顺序管理                         │   │
│  └─────────────────────────────────────────┘   │
│                      │                          │
│                      ▼                          │
│  ┌─────────────────────────────────────────┐   │
│  │         process.Group                   │   │
│  │  - Windows Job Object                   │   │
│  │  - 进程树管理                           │   │
│  │  - 优雅关闭                             │   │
│  └─────────────────────────────────────────┘   │
│                      │                          │
│                      ▼                          │
│  ┌─────────────────────────────────────────┐   │
│  │         Child Processes                 │   │
│  │  - nginx.exe                           │   │
│  │  - mysqld.exe                          │   │
│  │  - php-cgi.exe                         │   │
│  └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

### 3.3 核心组件

#### 3.3.1 进程组（process.Group）

使用 Windows Job Object 管理进程树：

```go
// process/group.go
package process

import (
    "context"
    "sync"
    "syscall"
)

// Group 进程组管理（使用 Windows Job Object）
type Group struct {
    mu       sync.Mutex
    handle   uintptr  // Job Object handle
    processes map[int]*Process
}

// NewGroup 创建进程组
func NewGroup() (*Group, error) {
    // 创建 Windows Job Object
    handle, err := createJobObject()
    if err != nil {
        return nil, err
    }
    
    return &Group{
        handle:    handle,
        processes: make(map[int]*Process),
    }, nil
}

// Start 启动进程并加入进程组
func (g *Group) Start(ctx context.Context, cmd Command) (*Process, error) {
    // 1. 创建进程
    proc, err := g.createProcess(cmd)
    if err != nil {
        return nil, err
    }
    
    // 2. 将进程加入 Job Object
    if err := g.assignProcessToJob(proc); err != nil {
        proc.Kill()
        return nil, err
    }
    
    // 3. 记录进程
    g.mu.Lock()
    g.processes[proc.PID] = proc
    g.mu.Unlock()
    
    return proc, nil
}

// Shutdown 关闭所有进程
func (g *Group) Shutdown(ctx context.Context) error {
    g.mu.Lock()
    defer g.mu.Unlock()
    
    // 1. 发送关闭信号
    for _, proc := range g.processes {
        proc.Signal(syscall.SIGTERM)
    }
    
    // 2. 等待优雅关闭
    timer := time.NewTimer(5 * time.Second)
    defer timer.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-timer.C:
            // 超时，强制关闭
            g.terminateAll()
            return nil
        default:
            if g.allExited() {
                return nil
            }
            time.Sleep(100 * time.Millisecond)
        }
    }
}

// Close 关闭进程组（强制）
func (g *Group) Close() error {
    g.terminateAll()
    return closeJobObject(g.handle)
}
```

#### 3.3.2 服务编排引擎（orchestrator.Engine）

```go
// orchestrator/engine.go
package orchestrator

import (
    "context"
    "sync"
)

// Engine 服务编排引擎
type Engine struct {
    mu       sync.RWMutex
    group    *process.Group
    services map[string]*Service
    procfile *Procfile
}

// Service 服务定义
type Service struct {
    Name     string
    Command  string
    Args     []string
    Env      map[string]string
    WorkDir  string
    Port     int
    AutoRun  bool
    
    // 运行时状态
    Process  *process.Process
    State    ServiceState
}

// ServiceState 服务状态
type ServiceState int

const (
    StateStopped ServiceState = iota
    StateStarting
    StateRunning
    StateStopping
    StateError
)

// NewEngine 创建编排引擎
func NewEngine() (*Engine, error) {
    group, err := process.NewGroup()
    if err != nil {
        return nil, err
    }
    
    return &Engine{
        group:    group,
        services: make(map[string]*Service),
    }, nil
}

// LoadProcfile 加载 Procfile
func (e *Engine) LoadProcfile(path string) error {
    procfile, err := ParseProcfile(path)
    if err != nil {
        return err
    }
    
    e.procfile = procfile
    
    // 将 Procfile 中的服务转换为 Service 定义
    for _, entry := range procfile.Entries {
        svc := &Service{
            Name:    entry.Name,
            Command: entry.Command,
            Args:    entry.Args,
            Env:     entry.Env,
            WorkDir: entry.WorkDir,
            AutoRun: entry.AutoRun,
        }
        e.services[svc.Name] = svc
    }
    
    return nil
}

// StartAll 启动所有服务（按依赖顺序）
func (e *Engine) StartAll(ctx context.Context) error {
    // 1. 按依赖顺序排序
    order := e.getStartupOrder()
    
    // 2. 按顺序启动
    for _, name := range order {
        if err := e.Start(ctx, name); err != nil {
            return fmt.Errorf("start %s failed: %w", name, err)
        }
    }
    
    return nil
}

// Start 启动单个服务
func (e *Engine) Start(ctx context.Context, name string) error {
    e.mu.Lock()
    svc, ok := e.services[name]
    if !ok {
        e.mu.Unlock()
        return fmt.Errorf("service %s not found", name)
    }
    
    if svc.State == StateRunning {
        e.mu.Unlock()
        return nil
    }
    
    svc.State = StateStarting
    e.mu.Unlock()
    
    // 1. 检查端口
    if err := e.checkPort(svc); err != nil {
        e.setError(name, err)
        return err
    }
    
    // 2. 验证配置
    if err := e.validateConfig(svc); err != nil {
        e.setError(name, err)
        return err
    }
    
    // 3. 启动进程
    cmd := process.Command{
        Executable: svc.Command,
        Args:       svc.Args,
        Env:        svc.Env,
        WorkDir:    svc.WorkDir,
    }
    
    proc, err := e.group.Start(ctx, cmd)
    if err != nil {
        e.setError(name, err)
        return err
    }
    
    // 4. 等待服务就绪
    if err := e.waitForReady(svc, 30*time.Second); err != nil {
        proc.Kill()
        e.setError(name, err)
        return err
    }
    
    // 5. 更新状态
    e.mu.Lock()
    svc.Process = proc
    svc.State = StateRunning
    e.mu.Unlock()
    
    return nil
}

// Stop 停止单个服务
func (e *Engine) Stop(ctx context.Context, name string) error {
    e.mu.Lock()
    svc, ok := e.services[name]
    if !ok {
        e.mu.Unlock()
        return fmt.Errorf("service %s not found", name)
    }
    
    if svc.State == StateStopped {
        e.mu.Unlock()
        return nil
    }
    
    svc.State = StateStopping
    e.mu.Unlock()
    
    // 1. 发送优雅停止信号
    if svc.Process != nil {
        if err := svc.Process.Signal(syscall.SIGTERM); err != nil {
            // 信号发送失败，强制停止
            svc.Process.Kill()
        }
    }
    
    // 2. 等待进程退出
    if err := svc.Process.WaitContext(ctx); err != nil {
        // 超时，强制停止
        svc.Process.Kill()
    }
    
    // 3. 更新状态
    e.mu.Lock()
    svc.Process = nil
    svc.State = StateStopped
    e.mu.Unlock()
    
    return nil
}

// StopAll 停止所有服务（反向顺序）
func (e *Engine) StopAll(ctx context.Context) error {
    order := e.getShutdownOrder()
    
    for _, name := range order {
        if err := e.Stop(ctx, name); err != nil {
            // 记录错误但继续停止其他服务
            log.Printf("stop %s failed: %v", name, err)
        }
    }
    
    return nil
}

// Shutdown 关闭编排引擎
func (e *Engine) Shutdown(ctx context.Context) error {
    // 1. 停止所有服务
    e.StopAll(ctx)
    
    // 2. 关闭进程组
    return e.group.Close()
}
```

#### 3.3.3 Procfile 解析器

```go
// orchestrator/procfile.go
package orchestrator

import (
    "bufio"
    "os"
    "strings"
)

// ProcfileEntry Procfile 条目
type ProcfileEntry struct {
    Name    string
    Command string
    Args    []string
    Env     map[string]string
    WorkDir string
    AutoRun bool
}

// Procfile Procfile 定义
type Procfile struct {
    Entries []ProcfileEntry
}

// ParseProcfile 解析 Procfile
func ParseProcfile(path string) (*Procfile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    
    procfile := &Procfile{}
    
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        
        // 跳过空行和注释
        if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
            continue
        }
        
        entry, err := parseProcfileEntry(line)
        if err != nil {
            return nil, err
        }
        
        procfile.Entries = append(procfile.Entries, *entry)
    }
    
    return procfile, scanner.Err()
}

// parseProcfileEntry 解析单个条目
func parseProcfileEntry(line string) (*ProcfileEntry, error) {
    // 格式: ; Name: autorun COMMANDS env_file=PATH pwd=WORKDIR ENV_VAR=VALUE
    
    // 找到冒号分隔
    idx := strings.Index(line, ":")
    if idx == -1 {
        return nil, fmt.Errorf("invalid procfile line: %s", line)
    }
    
    name := strings.TrimSpace(line[:idx])
    rest := strings.TrimSpace(line[idx+1:])
    
    entry := &ProcfileEntry{
        Name: name,
        Env:  make(map[string]string),
    }
    
    // 解析 autorun
    if strings.HasPrefix(strings.ToLower(rest), "autorun ") {
        entry.AutoRun = true
        rest = strings.TrimSpace(rest[8:])
    }
    
    // 解析 env_file
    if strings.Contains(rest, "env_file=") {
        // 提取 env_file 参数
        // TODO: 实现 env_file 解析
    }
    
    // 解析 pwd
    if strings.Contains(rest, "pwd=") || strings.Contains(rest, "PWD=") {
        idx := strings.Index(strings.ToLower(rest), "pwd=")
        entry.WorkDir = extractValue(rest[idx+4:])
        rest = rest[:idx]
    }
    
    // 解析环境变量
    parts := strings.Fields(rest)
    var cmdParts []string
    
    for _, part := range parts {
        if strings.Contains(part, "=") {
            // 环境变量
            kv := strings.SplitN(part, "=", 2)
            entry.Env[kv[0]] = kv[1]
        } else {
            // 命令部分
            cmdParts = append(cmdParts, part)
        }
    }
    
    // 解析命令
    if len(cmdParts) > 0 {
        entry.Command = cmdParts[0]
        entry.Args = cmdParts[1:]
    }
    
    return entry, nil
}
```

---

## 四、实施计划

### 4.1 阶段 1：进程组（Windows Job Object）

**目标**：实现进程树管理，解决 `taskkill /F /T` 误杀问题。

**改动**：
- 创建 `internal/process/group.go`
- 实现 Windows Job Object 管理
- 替换现有的 `taskkill` 逻辑

**验证**：
- 启动 nginx，nginx 启动 php-cgi
- 停止 nginx 时，php-cgi 也被停止
- 不影响外部进程

### 4.2 阶段 2：Procfile 解析器

**目标**：支持声明式服务定义。

**改动**：
- 创建 `internal/orchestrator/procfile.go`
- 实现 Procfile 解析
- 支持 autorun、环境变量、工作目录

**验证**：
- 能正确解析 Procfile 格式
- 支持各种参数组合

### 4.3 阶段 3：服务编排引擎

**目标**：实现核心编排逻辑。

**改动**：
- 创建 `internal/orchestrator/engine.go`
- 实现服务启动/停止/重启
- 实现依赖顺序管理
- 实现端口冲突检测

**验证**：
- 按正确顺序启动服务
- 端口冲突时自动检测
- 服务崩溃时自动重启

### 4.4 阶段 4：集成到现有代码

**目标**：将新引擎集成到 DevStack。

**改动**：
- 修改 `internal/service/manager.go`
- 使用新的 orchestrator.Engine
- 保留现有 Provider 接口

**验证**：
- 现有功能正常工作
- 新引擎接管服务管理

---

## 五、与现有代码的关系

### 5.1 保留什么

- `provider/` 目录：保留 Provider 接口和实现
- `internal/service/`：保留 ServiceManager 的对外接口
- `internal/config/`：保留配置管理

### 5.2 替换什么

- `internal/process/process.go`：替换为 `internal/process/group.go`
- `internal/service/manager.go`：内部逻辑替换为 `internal/orchestrator/engine.go`

### 5.3 新增什么

- `internal/process/group.go`：Windows Job Object 进程组
- `internal/orchestrator/procfile.go`：Procfile 解析器
- `internal/orchestrator/engine.go`：服务编排引擎

---

## 六、风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Windows Job Object 兼容性 | 低 | Windows 7+ 都支持 |
| Procfile 格式兼容性 | 低 | 参考 Laragon 格式 |
| 现有代码迁移 | 中 | 保留对外接口，内部重构 |
| 性能影响 | 低 | Job Object 是内核级，性能更好 |

---

*创建时间：2026-08-20*
