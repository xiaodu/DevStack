# DevStack 项目记忆

## 项目真相（重要）
- **DevStack** 是一个本地开发环境管理工具（类似 Laragon/PhpStudy）：Wails v2 + Go 后端 + **Svelte** 前端。
- 后端 domain：`internal/{config,service,runtime,project,environment,engine,reactive,process,resource,provider,download,tray}`。
- 管理组件：nginx / mysql / redis / php / node / python，支持多版本、环境编排（Reconciler）、快照、响应式规则引擎。
- ⚠️ **`~/.workbuddy/USER.md` 描述为"Vue.js + Three.js 的 3D 国际象棋游戏"——完全失实**，会误导后续会话，建议修正为 DevStack 实际栈。

## 主要问题地图（2026-08-07 分析）
P0 严重：
1. 两个独立 reactive.Engine 实例（app.go:163 vs service/manager.go:50），svcMgr 内部 engine 发布的崩溃/健康检查事件前端收不到（桥接只在 app 的 engine）。
2. service.Manager.Stop 持全局锁 m.mu 最长阻塞 ~3s（外部 Stop + 端口轮询），冻结所有服务 API。✅ 2026-08-19 重构解决：删端口轮询兜底，分层停止，阻塞下移到 provider 内部（锁外）。
3. service.Manager.Start 仅拦 Running，并发启动同一 id 会拉起双进程（Starting 不拦截）。
4. reactive.Engine.dispatch 裸 goroutine 无 panic recovery，handler panic 会 crash 进程。
5. main.go Debug.OpenInspectorOnStartup=true，生产构建也弹 DevTools。

P1：
6. app.go 是 1393 行 God Object，应按 domain 拆分。
7. DownloadService 与 downloadServiceVersion 重复且行为分歧。
8. getActiveEnvironmentID 静默忽略 json.Unmarshal 错误，绕过 StateStore 抽象。
9. Configure 事务与 m.mu 锁边界混乱。

P2 债务：
10. 前端依赖栈过时（vite 3 / svelte 3 / typescript 7.0.2，lock 已锁但生态不兼容）。
11. README 仍是 Wails Svelte 模板默认内容，无 DevStack 说明。
12. service/reconciler/process 缺单元测试，并发缺陷正是盲区。
13. process.Start 子进程 stdout/stderr=nil 输出被丢弃。

## 构建/运行
- `wails build` 会先 `npm run build` 再 embed `frontend/dist`；dist 已存在说明前端曾成功构建。
- 前端 src 仅约 20 个 .svelte/.ts 文件，"3357 文件"绝大多数是 node_modules。

## 服务停止设计范式（2026-08-19 重构确立）
- **分层停止**：优雅停止（服务自带命令）→ 等进程退出 → 兜底 `taskkill /F /T`
  - nginx: `nginx -s stop`（依赖 `logs/nginx.pid`）
  - mysql: `mysqladmin --port=<p> -u root shutdown`（避免强杀数据损坏，等 10s）
  - redis: `redis-cli -p <p> shutdown`（触发 RDB save）
  - php-cgi: 无自带停止命令，直接 taskkill /T
- **统一判据**：进程退出（`IsProcessRunning`），**不用**端口是否释放
- **进程所有权**：`ownedPIDByPort` 只收集 PID 不杀，统一在 provider.Stop 内 `i.pm.Stop`
- **错误透传**：`process.Manager.Stop` 捕获 taskkill stderr，真实失败原因带给用户
- **service.Manager.Stop**：信任 provider 返回，不再叠加端口扫描兜底
- 参考 ServBay（stop/kill 分离）/ XAMPP（服务自带停止命令）
