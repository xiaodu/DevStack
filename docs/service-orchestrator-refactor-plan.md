# DevStack 服务编排引擎重构计划

## 重构目标

将 DevStack 的服务管理从"拼凑式"改造为"专业级"，对标 Laragon 的稳定性和 processkit-go 的架构质量。

---

## 当前问题总结

### P0 级问题（影响可靠性）

| 问题 | 位置 | 影响 |
|------|------|------|
| 双轨状态管理 | ServiceManager vs Provider 状态分裂 | 状态不一致，难以排查 |
| Start 后 netstat 反查 PID | `manager.go:362` | PID 获取不可靠 |
| ConfigHandler 与 Provider 配置重复 | `service/nginx.go` vs `provider/nginx_provider.go` | 维护困难 |
| Start 过程状态竞争窗口 | `manager.go:316-368` | 并发操作风险 |

### P1 级问题（影响健壮性）

| 问题 | 位置 | 影响 |
|------|------|------|
| 健康检查仅 TCP 探活 | `manager.go:561-581` | 无法检测应用层故障 |
| Manager 与 Provider HealthCheck 不一致 | 两层检查逻辑不同 | 检查结果不可靠 |
| watchProcess 无优雅关闭 | `manager.go:494-559` | goroutine 泄漏 |

---

## 重构方案

### 阶段 1：统一 PID 传递链路（P0）

**目标**：消除 Start 后的 netstat 扫描，Provider 直接返回 PID。

**改动**：

1. 修改 `ManagedEntity.Start` 接口，返回 PID
```go
// provider/capability.go
type ManagedEntity interface {
    Start(ctx context.Context) (int, error)  // 返回 PID
    Stop(ctx context.Context) error
    // ...
}
```

2. 修改 ServiceManager.Start，使用 Provider 返回的 PID
```go
// service/manager.go
pid, err := provider.Start(ctx)
if err != nil {
    return err
}
s.process = &process.ProcessHandle{PID: pid}
// 删除 FindPIDByPort 调用
```

3. 各 Provider 实现返回 PID
```go
// nginx_provider.go
func (i *NginxInstance) Start(ctx context.Context) (int, error) {
    pid, err := i.pm.Start(...)
    if err != nil {
        return 0, err
    }
    return pid, nil
}
```

**验证**：启动服务后，`List()` 和 `Get()` 返回的 PID 与实际进程一致。

---

### 阶段 2：消除配置代码重复（P0）

**目标**：统一配置生成逻辑到 Provider，ServiceManager 只做委托。

**改动**：

1. 删除 `service/nginx.go`、`service/mysql.go`、`service/redis.go` 的 ConfigHandler
2. ServiceManager.Configure 直接委托 Provider
```go
// service/manager.go
func (m *Manager) Configure(id string, values map[string]interface{}) error {
    svc := m.services[id]
    return svc.Provider.Configure(values)
}
```

3. Provider 的 Configure 方法负责配置生成和验证
```go
// nginx_provider.go
func (p *NginxProvider) Configure(values map[string]interface{}) error {
    // 1. 生成 nginx.conf
    // 2. 验证配置语法: nginx -t
    // 3. 返回结果
}
```

**验证**：修改配置后，只有一处代码生成配置文件。

---

### 阶段 3：修复状态竞争窗口（P0）

**目标**：Start 过程中状态变更原子化。

**方案 A：持锁整个 Start 过程**
```go
func (m *Manager) Start(id string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    s.Info.Status = core.StatusStarting
    // Provider.Start 可能耗时，但保持锁
    pid, err := provider.Start(ctx)
    if err != nil {
        s.Info.Status = core.StatusStopped
        return err
    }
    s.Info.Status = core.StatusRunning
    return nil
}
```

**方案 B：使用 atomic 状态**
```go
type ServiceInstance struct {
    status atomic.Int32  // ServiceStatus
}

func (s *ServiceInstance) Status() core.ServiceStatus {
    return core.ServiceStatus(s.status.Load())
}
```

**推荐方案 A**：简单直接，Provider Start 时间可控（<5s）。

---

### 阶段 4：增强健康检查（P1）

**目标**：从 TCP 探活升级为应用层健康检查。

**改动**：

1. Provider 实现应用层健康检查
```go
// nginx_provider.go
func (i *NginxInstance) HealthCheck(ctx context.Context) error {
    // HTTP GET http://127.0.0.1:port/
    // 检查 200 状态码
}

// mysql_provider.go
func (i *MySQLInstance) HealthCheck(ctx context.Context) error {
    // 执行 SELECT 1
    // 检查返回结果
}

// redis_provider.go
func (i *RedisInstance) HealthCheck(ctx context.Context) error {
    // 执行 PING
    // 检查返回 PONG
}
```

2. ServiceManager 委托 Provider 健康检查
```go
func (m *Manager) checkServiceHealth(id string) bool {
    svc := m.services[id]
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    return svc.Provider.HealthCheck(ctx) == nil
}
```

**验证**：MySQL 停止响应查询时，健康检查失败并触发重启。

---

### 阶段 5：watchProcess 优雅关闭（P1）

**目标**：Manager 关闭时统一停止所有 watcher goroutine。

**改动**：

1. 添加 Shutdown 方法
```go
func (m *Manager) Shutdown() {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    for id, svc := range m.services {
        if svc.stopped != nil {
            close(svc.stopped)
        }
    }
}
```

2. 在 App 关闭时调用
```go
// app.go
func (a *App) shutdown() {
    a.serviceManager.Shutdown()
    // ...
}
```

**验证**：程序退出时无 goroutine 泄漏警告。

---

## 实施顺序

```
阶段 1 (P0) ──→ 阶段 2 (P0) ──→ 阶段 3 (P0) ──→ 阶段 4 (P1) ──→ 阶段 5 (P1)
   │                │                │                │                │
   ↓                ↓                ↓                ↓                ↓
 统一 PID       消除配置重复    修复状态竞争    增强健康检查    优雅关闭
```

每个阶段独立可测试，完成后即可提交。

---

## 风险评估

| 阶段 | 风险 | 缓解措施 |
|------|------|----------|
| 阶段 1 | Provider Start 返回的 PID 不准确 | 添加 PID 验证逻辑 |
| 阶段 2 | 删除 ConfigHandler 影响其他功能 | 先检查所有调用点 |
| 阶段 3 | 持锁时间过长影响 UI 响应 | Provider Start 应异步化 |
| 阶段 4 | 应用层检查增加延迟 | 设置合理超时（2s） |
| 阶段 5 | 关闭时服务未完全停止 | 添加优雅关闭超时 |

---

## 验证清单

- [ ] 启动/停止服务 10 次，PID 始终正确
- [ ] 并发启动/停止同一服务，无竞态条件
- [ ] 服务崩溃后自动重启，最多 3 次
- [ ] 修改配置后立即生效（或提示重启）
- [ ] 程序退出时无 goroutine 泄漏
- [ ] Dashboard 刷新不触发全量 netstat 扫描

---

*创建时间：2026-08-20*
