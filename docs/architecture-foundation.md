# DevStack 底层架构重构方案

> 本文档定位：不是功能需求文档，而是基础设施设计文档。
> 解决的核心问题：项目当前是"功能正确的工具集合"，但缺乏"本地环境编排平台"应有的底层基础。

---

## 1. 现状诊断

### 1.1 代码层面的具体脆弱点

| 位置 | 问题 | 影响 |
|------|------|------|
| `service/manager.go:288-318` | `Start()` 解锁后立即 `go m.watchProcess(id)`，handler 可能尚未设置 `s.process` | 竞态条件，watchProcess 可能看到空 process |
| `service/manager.go:358-397` | `watchProcess` 检测到崩溃直接 `m.Start(id)` 重启，无重试上限、无退避 | 配置错误时引发无限重启风暴，CPU 占满 |
| `service/manager.go:159-240` vs `L242-286` | `List()` 和 `Get()` 维护运行状态的逻辑完全复制粘贴 | 改一处必须改两处，极易遗漏 |
| `service/manager.go:719-721` | `taskkill` / `attrib` 的错误被 `cmd.Run()` 直接忽略 | 卸载失败无反馈，留下脏文件 |
| `app.go:365` | `releases, _ := a.dlMgr.GetAvailableVersions(...)` 错误被吞掉 | 用户看到"未找到下载地址"，而非真实错误 |
| `app.go:440-442` | `logWatcher.ReadRecent` 出错直接返回空数组 | 前端显示空日志，用户无法感知错误 |
| `app.go:149-186` | `GetServiceConfig` 手写 `switch-case` 转换 struct → map | 新增配置项需改 3 处：struct、UI、switch |
| `app.go:413-444` | `GetLogs` 用 `if-else` 硬编码各服务日志路径 | 新增服务必须修改 app.go |
| `service/helpers.go:44-49` | `handlers` 是全局 map，服务类型硬编码 | 新增服务类型需改 core/types、handlers、多处 switch |
| `process/process.go:125-143` | `FindPIDByPort` 解析 `netstat` 文本输出 | 依赖文本格式，无容错，跨平台脆弱 |

### 1.2 架构层面的根本缺失

当前代码完成了"功能"，但没有建立"领域抽象"。具体表现为四个缺失：

**缺失一：没有期望状态（Desired State）**

系统只能执行命令（Start/Stop/Create），不能维持状态。用户必须自己记住执行顺序、处理失败、维护一致性。

**缺失二：没有统一资源模型**

端口、路径、进程 ID、配置文件都是资源，但没有资源层。端口硬编码、路径拼字符串、进程只记 PID 不记树，导致冲突无法自动解决，泄漏无法自动回收。

**缺失三：没有事务与快照**

安装、创建项目、切换版本都是多步骤操作，但中间失败会留下半完成状态（如下载成功但解压失败，目录存在但配置未写）。没有回滚能力。

**缺失四：没有依赖图**

"项目"和"服务"是两个独立概念，系统不知道"项目 A 需要 Nginx + PHP 8.3 + MySQL"。启动项目时不会自动启动依赖，停止 MySQL 时不会警告有项目依赖它。

---

## 2. 目标架构愿景

DevStack 的本质是一个**本地开发环境的基础设施编排平台**。其底层应由六大基础设施构成：

```
┌─────────────────────────────────────────────────────────────┐
│                    用户界面 / API 层                         │
├─────────────────────────────────────────────────────────────┤
│  声明式状态引擎  │  环境上下文  │  反应式规则引擎              │
│  (Desired State)│ (Context)   │  (Reactive)                  │
├─────────────────────────────────────────────────────────────┤
│  依赖图引擎    │  事务与快照系统  │  统一资源调度器              │
│  (Dependency)  │  (Transaction) │  (Resource Manager)          │
├─────────────────────────────────────────────────────────────┤
│         Provider 架构（Nginx / MySQL / PHP / ...）           │
├─────────────────────────────────────────────────────────────┤
│              进程管理  │  配置管理  │  包管理                   │
└─────────────────────────────────────────────────────────────┘
```

### 2.1 声明式状态引擎（Desired State Engine）

从"命令式"转向"声明式"。用户或系统定义期望状态，引擎持续对比 Current State 与 Desired State，自动收敛。

```go
// 用户声明：我要这个环境
desired := Environment{
    Services: []ServiceRef{
        {ID: "nginx", Version: "1.26", State: Running},
        {ID: "mysql", Version: "8.4", State: Running},
        {ID: "php", Version: "8.3", State: Running},
    },
    Projects: []Project{
        {Name: "erp", Domain: "erp.local", PHPVersion: "8.3", Enabled: true},
    },
}

// 系统自动完成：启动 MySQL → 启动 PHP 8.3 → 启动 Nginx → 生成 vhost → reload Nginx
reconciler.Apply(desired)
```

**核心价值：**
- 启动项目时自动推导并启动依赖链
- 切换 PHP 版本变成原子操作：更新 Desired State → 自动停旧版 → 启新版 → 更新 Nginx upstream → reload
- 支持环境模板：导入 `devstack.yaml` 一键还原整个团队环境

### 2.2 统一资源调度器（Resource Manager）

所有可分配/可回收的实体都是资源，统一分配、冲突检测、自动回收。

```go
type ResourceType string
const (
    ResourcePort     ResourceType = "port"
    ResourcePath     ResourceType = "path"
    ResourceProcess  ResourceType = "process"
)

type ResourceManager interface {
    AllocatePort(preferred int, owner string) (int, error)     // 冲突时自动递增
    ReleasePort(port int, owner string) error
    AllocatePath(scope, name string) string                    // 统一命名空间
    RegisterProcessTree(rootPID int, owner string) (*ProcessTree, error)
}
```

**核心价值：**
- 创建项目时 80 端口被占用，自动分配 8080，并级联更新所有引用该端口的配置
- 卸载服务时资源自动回收，端口回到池中
- 再也不会出现"改了端口但忘了改 Nginx 配置"的问题

### 2.3 事务与快照系统（Transaction & Snapshot）

每个操作都是原子事务，支持回滚。每次重大变更前自动打快照。

```go
tx := state.NewTransaction("install-php-8.3")
tx.Step("download", func() error { ... })
tx.Step("extract", func() error { ... })
tx.Step("configure", func() error { ... })
if err := tx.Commit(); err != nil {
    tx.Rollback()  // 自动清理已创建的文件和注册信息
}

// 快照
snapshot := state.Snapshot("before-nginx-config-change")
// ... 修改配置 ...
state.Rollback(snapshot.ID)  // 一键回滚
```

**核心价值：**
- 下载成功但解压失败时，不会留下脏目录
- 改 Nginx 配置导致启动失败时，一键回滚到上一个稳定状态
- 切换 PHP 版本失败时，自动恢复旧版本运行状态

### 2.4 依赖图引擎（Dependency Graph）

项目、服务、运行时之间建立显式依赖关系，系统自动处理拓扑排序和级联操作。

```
Project "erp"
  → depends on Nginx (reverse proxy)
  → depends on PHP 8.3 (FastCGI)
  → depends on MySQL (database)

Project "api"
  → depends on Nginx
  → depends on Node 20 (user-managed process)
```

**核心价值：**
- 启动 `erp` 时自动推导：MySQL → PHP 8.3 → Nginx → vhost
- 停止全局 MySQL 时警告"有 2 个项目依赖此服务"
- 卸载 PHP 8.3 时检查是否有项目引用它

### 2.5 能力导向的 Provider 架构

系统内建的是"能力"（Capability），不是"服务名称"。具体实现通过 Provider 插件化接入。

```go
// 内建：系统知道什么是 Web Server 能力
type WebServerCapability interface {
    ServeHTTP(root string, upstream Upstream) error
    Reload() error
    AddVhost(vhost VhostConfig) error
}

// Nginx 或 Caddy 只需实现这个接口
type NginxProvider struct{}
type CaddyProvider struct{}

// 注册时自描述
func init() {
    registry.Register("nginx", &NginxProvider{}, CapabilityWebServer)
}
```

**核心价值：**
- 新增服务类型从"改 20 个文件"降到"实现 3 个接口"
- `core.ServiceType` 的硬编码枚举可以逐步消除
- 用户甚至可以接入自定义的二进制（如 OpenResty、MariaDB）

### 2.6 反应式规则引擎（Reactive Rule Engine）

系统内所有状态变更都是事件，事件触发自动决策，而不是仅记录日志。

```
[事件] php-8.3 进程退出
   ↓
[策略] 健康检查失败
   ↓
[决策] 尝试重启（第 1/3 次）
   ↓
[事件] 重启后端口冲突
   ↓
[决策] 从资源池申请新端口
   ↓
[动作] 更新 php.ini 端口 → 重启 → 更新 nginx upstream → reload nginx
```

规则可配置：
```yaml
rules:
  - when: service.crashed
    if: restart_count < 3
    then: service.restart
  - when: port.conflict
    then: [resource.reallocate_port, config.update, service.reload]
```

**核心价值：**
- 不是"服务崩溃后记录日志"，而是"自动恢复或优雅降级"
- 前端通过订阅事件流实时更新 UI，无需轮询
- 为后续"智能运维"（自动扩缩容、健康自愈）打下基础

---

## 3. 概念模型升级

### 3.1 当前模型（v3）

```
服务 (Service)   — 基础设施进程（Nginx / MySQL / Redis）
运行时 (Runtime) — 语言运行时（PHP / Node / Python）
项目 (Project)   — 用户应用
```

三者之间没有显式关系，靠前端手动协调。

### 3.2 目标模型（Foundation）

```
环境 (Environment)       — 顶层聚合，对应一个 Desired State
  ├─ 全局服务 (GlobalService)   — Nginx, MySQL, Redis（被多个项目共享）
  ├─ 运行时实例 (RuntimeInstance) — PHP-8.3, Node-20（可被项目引用）
  └─ 项目 (Project)             — 用户应用，声明依赖哪些服务和运行时
       └─ 依赖声明 (Dependencies)
            → WebServer: nginx
            → FPMRuntime: php-8.3
            → Database: mysql
```

**关键变化：**
- `Project` 不再只是一个 Nginx vhost 配置，而是一个**环境上下文**，声明了它所需的一切
- `Service` 和 `Runtime` 统一为**可管理实体**（Managed Entity），实现相同接口
- 所有实体都有 `DesiredState` 和 `CurrentState`，由 Reconciler 自动收敛

### 3.3 统一接口设计

```go
// 所有可管理实体都实现此接口
type ManagedEntity interface {
    ID() string
    Type() EntityType
    
    // 生命周期
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Status() (EntityStatus, error)
    HealthCheck() error
    
    // 配置
    Configure(values map[string]interface{}) error
    ConfigSchema() ConfigSchema
    
    // 可观测性
    LogPaths() []string
    Metrics() (Metrics, error)
}

// 全局状态协调器
type Reconciler interface {
    Apply(env Environment) error           // 声明式应用
    Diff(env Environment) ([]Change, error) // 差异分析
    Rollback(snapshotID string) error       // 快照回滚
}

// 资源管理器
type ResourceManager interface {
    Allocate(rt ResourceType, preferred interface{}, owner string) (Resource, error)
    Release(id ResourceID) error
    ListByOwner(owner string) []Resource
}

// 依赖图
type DependencyGraph interface {
    AddNode(node EntityNode)
    AddEdge(from, to string)
    TopoSort() ([]string, error)
    DependentsOf(id string) []string
}
```

---

## 4. 分阶段实施路线图

> 原则：先建骨架，再填血肉；先让新架构能跑，再迁移旧代码。

### Phase 0: 地基清理（1 周）

**目标：** 消除最明显的脆弱点，让后续重构有一个稳定的基础。

| 任务 | 文件 | 具体动作 |
|------|------|----------|
| 错误处理补全 | `app.go`, `service/manager.go` | 所有 `_ = cmd.Run()` 改为显式处理或至少记录日志；所有被吞掉的 error 返回给上层 |
| 状态检测复用 | `service/manager.go` | 提取 `syncServiceState(s *ServiceInstance)` 统一方法，消除 `List()` 和 `Get()` 的重复逻辑 |
| 重启风暴修复 | `service/manager.go` | `watchProcess` 增加重试计数（最多 3 次）和冷却时间（5s → 10s → 20s），超限后标记为 Error 不再自动重启 |
| 竞态修复 | `service/manager.go` | `Start()` 中确保 `s.process` 被 handler 设置完成后再 Unlock，或让 watchProcess 能处理 process 为 nil 的情况 |
| 进程树跟踪 | `process/process.go` | `Manager.Start` 返回 `*ProcessTree` 而非仅 PID，支持追踪子进程（Nginx master/worker） |

**验收标准：**
- `go test ./...` 通过（需补充单元测试）
- 手动测试：故意给 Nginx 错误配置，确认它只重启 3 次后停止，不再无限循环
- 手动测试：在 `List()` 和 `Get()` 返回不一致时触发告警（可用临时断言检测）

### Phase 1: 声明式状态引擎（3 周）

**目标：** 建立 Desired State → Current State → Reconcile 的核心骨架。

| 任务 | 新建/修改 | 说明 |
|------|-----------|------|
| 新建 `internal/engine/` | 新建目录 | 声明式状态引擎核心 |
| `engine/types.go` | 新建 | 定义 `Environment`, `ServiceRef`, `ProjectRef`, `EntityState` 等 |
| `engine/reconciler.go` | 新建 | 实现 `Reconciler` 接口：`Apply()`, `Diff()`, `Rollback()` |
| `engine/state_store.go` | 新建 | 持久化 Desired State 和 Current State（JSON 文件） |
| `engine/snapshot.go` | 新建 | 快照创建、存储、回滚机制 |
| 修改 `service/manager.go` | 修改 | 让 `ServiceInstance` 支持 `DesiredState` 和 `CurrentState` |
| 修改 `app.go` | 修改 | 新增 `ApplyEnvironment(env engine.Environment)` API，保留旧 API 做兼容 |

**设计要点：**
- `Reconciler.Apply` 的流程：
  1. 计算 Diff（Desired - Current）
  2. 按依赖图拓扑排序
  3. 逐个执行 Change（Start/Stop/Configure）
  4. 每步成功更新 Current State；失败触发 Rollback
- 先只支持 `Service` 的声明式管理，`Project` 和 `Runtime` 在 Phase 2 接入

**验收标准：**
- 可以通过 JSON/YAML 定义一组服务的期望状态，调用 `Apply` 后系统自动启动/停止对应服务
- 故意在 Apply 中间制造失败（如端口被占用），验证 Rollback 能恢复到之前状态
- 旧的前端 API（`StartService`/`StopService`）仍然可用，但内部转发到新的 Reconciler

### Phase 2: 统一资源调度器（2 周）

**目标：** 让端口、路径、进程成为一等公民，自动分配回收。

| 任务 | 新建/修改 | 说明 |
|------|-----------|------|
| 新建 `internal/resource/` | 新建目录 | 资源管理核心 |
| `resource/manager.go` | 新建 | 实现 `ResourceManager` 接口 |
| `resource/port.go` | 新建 | 端口池管理：范围 8000-9999，冲突时自动递增，记录 owner |
| `resource/path.go` | 新建 | 路径命名空间管理：`{installDir}/{scope}/{name}` |
| `resource/process.go` | 新建 | 进程树资源包装 |
| 修改 `runtime/php.go` | 修改 | PHP 端口分配从硬编码 `9000 + index` 改为调用 `resource.AllocatePort` |
| 修改 `service/nginx.go` | 修改 | Nginx 配置生成时，PHP upstream 引用资源句柄而非硬编码端口 |
| 修改 `project/manager.go` | 修改 | 创建项目时自动分配端口（如果冲突），并级联更新 Nginx 配置 |

**验收标准：**
- 安装 PHP 8.3 和 8.2 时，系统自动分配两个不冲突的端口
- 手动占用一个已分配端口后重启系统，系统能检测到冲突并重新分配
- 卸载 PHP 8.3 后，其端口回到池中，被下一个安装的版本复用

### Phase 3: 依赖图与环境上下文（2 周）

**目标：** 让项目成为自包含的环境单元，系统自动处理依赖。

| 任务 | 新建/修改 | 说明 |
|------|-----------|------|
| 新建 `internal/graph/` | 新建目录 | 依赖图引擎 |
| `graph/graph.go` | 新建 | 实现 `DependencyGraph`：加节点、加边、拓扑排序、查找依赖方 |
| `graph/resolver.go` | 新建 | 根据项目声明解析完整依赖树 |
| 修改 `core/types.go` | 修改 | `Project` 增加 `Dependencies map[string]string` 字段 |
| 修改 `project/manager.go` | 修改 | `Create` / `Toggle` 时调用 `graph.Resolver` 获取依赖链，按拓扑顺序启动 |
| 修改 `service/manager.go` | 修改 | `Stop` 时检查 `graph.DependentsOf(id)`，如有依赖方则返回警告错误 |
| 修改 `engine/reconciler.go` | 修改 | `Apply` 时自动构建并遍历依赖图 |

**验收标准：**
- 创建一个 PHP 项目，只调用 `project.Enable`，系统自动启动 MySQL → PHP → Nginx → 生成 vhost
- 停止 MySQL 时，如果已有项目依赖它，返回明确错误"MySQL 被项目 erp、blog 依赖，无法停止"
- 删除项目时，只停止项目专属的 Runtime，不停止被其他项目共享的 Global Service

### Phase 4: 事务与快照系统（2 周）

**目标：** 每次操作可原子、可回滚。

| 任务 | 新建/修改 | 说明 |
|------|-----------|------|
| 新建 `internal/tx/` | 新建目录 | 事务系统 |
| `tx/transaction.go` | 新建 | 实现 `Transaction`：`Step(name, fn)`、`Commit()`、`Rollback()` |
| `tx/compensate.go` | 新建 | 补偿操作注册：每个 Step 可同时注册正向操作和反向补偿操作 |
| 修改 `download/manager.go` | 修改 | `DownloadAndExtract` 包装为事务：下载 → 解压（失败时删除已下载文件） |
| 修改 `service/manager.go` | 修改 | `Install` / `SetVersion` 包装为事务 |
| 修改 `engine/reconciler.go` | 修改 | `Apply` 的每一步变更都作为事务 Step，支持整体回滚 |
| 修改 `engine/snapshot.go` | 修改 | 在 Apply 前自动对关键配置文件（nginx.conf、php.ini）打快照 |

**验收标准：**
- 安装一个服务，在解压步骤制造失败（如磁盘满），确认已下载的临时文件被自动清理
- 修改 Nginx 配置导致启动失败，系统能自动回滚配置并恢复到旧版本运行状态
- 切换项目 PHP 版本失败时，项目保持旧版本运行，不产生中断

### Phase 5: Provider 架构与反应式引擎（3 周）

**目标：** 系统从"写死的服务列表"变成"可插拔的能力平台"。

| 任务 | 新建/修改 | 说明 |
|------|-----------|------|
| 新建 `internal/provider/` | 新建目录 | Provider 框架 |
| `provider/registry.go` | 新建 | 能力注册中心：`Register(name, provider, capabilities...)` |
| `provider/capability.go` | 新建 | 定义标准能力接口：`WebServer`, `Database`, `FPMRuntime` 等 |
| 重构 `service/nginx.go` | 修改 | 提取为 `NginxProvider`，实现 `WebServerCapability` |
| 重构 `service/mysql.go` | 修改 | 提取为 `MySQLProvider`，实现 `DatabaseCapability` |
| 重构 `runtime/php.go` | 修改 | 提取为 `PHPProvider`，实现 `FPMRuntimeCapability` |
| 新建 `internal/reactive/` | 新建目录 | 反应式规则引擎 |
| `reactive/engine.go` | 新建 | 事件订阅、规则匹配、动作执行 |
| `reactive/rules.go` | 新建 | 内置规则：崩溃重启、端口冲突重分配、配置热加载 |
| 修改 `service/manager.go` | 修改 | `watchProcess` 改为发布事件到 `reactive.Engine`，由规则引擎决策 |

**验收标准：**
- 新增一个"模拟服务"只需实现 `ManagedEntity` 接口并注册，无需修改 `core/types.go` 或 `app.go`
- 故意 kill 掉一个 PHP-CGI worker，系统在 5 秒内检测到、尝试重启、成功恢复，前端通过事件流实时更新
- 手动占用 Nginx 端口，系统检测到冲突后自动分配新端口并级联更新所有配置，无需人工干预

### Phase 6: 前端与 API 适配（2 周）

**目标：** 前端从"命令式调用"升级到"声明式交互"。

| 任务 | 说明 |
|------|------|
| `lib/api.js` | 新增 `applyEnvironment`、`getEnvironmentDiff`、`rollbackSnapshot` 等 API |
| `stores.js` | 引入事件流订阅（WebSocket 或轮询），实时同步服务状态 |
| 项目创建向导 | 用户选择项目类型后，系统自动推荐并勾选依赖的服务 |
| 环境模板导入/导出 | 支持导出 `devstack.yaml`，支持从模板一键创建环境 |

---

## 5. 里程碑与风险

### 5.1 里程碑

| 阶段 | 时间 | 交付物 | 可演示点 |
|------|------|--------|----------|
| Phase 0 | 第 1 周 | 稳定的现有代码 | Nginx 错误配置时只重启 3 次 |
| Phase 1 | 第 2-4 周 | 声明式引擎骨架 | 用 JSON 定义环境，一键 Apply |
| Phase 2 | 第 5-6 周 | 资源调度器 | 自动分配端口，冲突自恢复 |
| Phase 3 | 第 7-8 周 | 依赖图 | 启用项目自动启动全部依赖 |
| Phase 4 | 第 9-10 周 | 事务与快照 | 配置改崩了一键回滚 |
| Phase 5 | 第 11-13 周 | Provider + 反应式 | 新增服务不改核心代码；崩溃自动恢复 |
| Phase 6 | 第 14-15 周 | 前端适配 | 环境模板导入导出 |

### 5.2 风险与对策

| 风险 | 可能性 | 影响 | 对策 |
|------|--------|------|------|
| 重构期间引入回归 Bug | 高 | 现有功能不可用 | Phase 0 先补测试；每个 Phase 保留旧 API 做兼容层；关键路径手动测试 |
| 声明式引擎性能问题（大规模环境 Diff 慢） | 中 | Apply 延迟高 | 初期只管理 10-20 个实体，性能问题在量起来后才需优化；Diff 算法可缓存 |
| 前端改动跟不上后端 | 中 | 新能力无法暴露 | 每个 Phase 的后端都提供兼容旧 API 的 facade，前端可以分阶段升级 |
| Windows 进程管理特殊性 | 高 | 进程树、优雅停止难以实现 | 保留现有 `taskkill` 兜底；进程树追踪先只支持 Nginx 和 PHP-CGI |
| 团队对声明式范式不熟悉 | 中 | 开发效率下降 | 文档先行；Phase 1 的接口设计充分讨论后再编码 |

### 5.3 回滚策略

如果在任何 Phase 发现重大问题，可以：
1. **代码层面**：每个 Phase 独立分支开发，可单独回退
2. **API 层面**：旧 API（`StartService`/`StopService`）始终保留，前端可随时切回旧逻辑
3. **数据层面**：Phase 4 快照系统完成后，配置文件变更都有备份

---

## 6. 立即开始的第一步

如果确认本方案，建议按以下顺序启动：

1. **今天**：创建 `feature/foundation-phase0` 分支，开始 Phase 0 的错误处理和竞态修复
2. **本周内**：完成 Phase 0，合并到主分支，确保地基稳定
3. **下周**：启动 Phase 1，设计 `internal/engine/` 的接口（先写接口和测试，再写实现）

Phase 0 的具体代码改动我已经可以立即开始。是否继续？
