# DevStack 稳定性与架构审查报告

> 审查范围：Go 后端（Wails v2 + 服务/进程/事件/协调器/Provider/下载/持久化）+ Svelte 5 前端
> 审查日期：2026-08-18
> 定位方式：静态通读源码 + 并行深挖（Provider / Download-Config-Tx / Log-Graph-Tray / 前端），所有结论附 file:line
> 严重程度分级：P0 = 功能错误/数据损失（建议立即修复）；P1 = 高价值架构/稳定性；P2 = 前端稳定性与正确性；P3 = 技术债清理

---

## 概览

| 级别 | 数量 | 一句话 |
|------|------|--------|
| P0 | 5 ✅ | 自愈状态机失效、全系统 taskkill 误杀、端口收养误接管、zip-slip/任意读、非原子持久化 |
| P1 | 10 ✅ | Provider 无锁、环境切换无回滚、MySQL 首启、Redis 配置污染、下载并发/取消、StateStore、输出缓冲泄漏、reload 失效、GetService 裸指针、PortPool 锁内探测（P1-5 checksum 已实现） |
| P2 | 13 ⚠️ | 前端 busy 标志卡死、runOperation 无超时、svelte-check 缺失、IPC 层 untyped 等（F13 类型未强化） |
| P3 | 10 ✅ | graph/tx/snapshot/reactive/environment/tray/autostart/log/runtime 清理项 |

---

# P0 — 功能错误 / 数据损失

## P0-1 崩溃自愈状态机整体失效（最严重） ✅ 已修复

位置：`internal/service/manager.go` + `internal/reactive/rules.go`

### 现象与断点

崩溃流程：

1. `watchProcess` 检测健康失败 → `restartCount++` → 发 `EventServiceHealthFailed`（manager.go:519-547）
2. `service-health-failed` 规则调 `publishCrashEvent`（manager.go:557-586）：`time.Sleep(5*restartCount)` 后发 `EventServiceCrashed`
3. `service-crash-restart` 规则调 `m.Start(id)`（manager.go:62-72）

**断裂点 A**：`publishCrashEvent` 从不把 `Status` 从 `Running` 改走（只清 `process/PID`，manager.go:570-575）。于是 `Start` 的守卫 `Status==Running || Status==Starting → 直接返回`（manager.go:339-342）**拦截了重启，服务永远不会被自动拉起**。

**断裂点 B**：watchdog 每 5s 仍在 tick（状态还是 `Running`），backoff 期间计数持续膨胀。一次真实崩溃即可冲高 `count>3` 置永久 `StatusError`（manager.go:520-524），并误报"连续崩溃 3 次"。

**断裂点 C**：规则重复——一个 `Crashed` 事件触发 **3 个**动作：
- manager 的 `service-crash-restart`（manager.go:57-73）
- builtin 的 `service_crash_restart`（rules.go:119-137，阈值 `>=3`）
- builtin 的 `service_start_failure_retry`（rules.go:176-195，阈值 `>=2`，且 `time.Sleep(2s)` 阻塞规则循环）

**断裂点 D**：`Start` 重置 `restartCount=0`（manager.go:394）但不取消在途的 `publishCrashEvent` goroutine。过期事件会：清掉已健康进程的 `process/PID`（UI 显示已停止），或把用户手动停掉的服务重新拉起（publish 前未复检 `stopped`/状态）。

### 修复方向

收敛为每个服务一套状态机：
1. 检测到崩溃先持有锁把状态置出 `Running`（`Stopped`/`Error`），backoff 期间 watchdog 不再累计计数；
2. 去掉重复规则，只保留一条重启路径；
3. 规则 action 内禁止 `time.Sleep`（改由引擎定时/可取消的重试机制）；
4. `publishCrashEvent` 唤醒后持锁复检 `s.stopped != nil && Status == Running` 才发 `Crashed`；只在真正重启成功且旧 goroutine 已失效后重置计数。

---

## P0-2 系统级 `taskkill /f /im` 误杀 ✅ 已修复

位置：`internal/provider/nginx_provider.go:455-459`（`killAllByName`）、`internal/service/manager.go:1061-1081`（`removeAll`）

### 现象与断点

四个 Provider 的 `Stop` 在正确停止自有进程后，**仍执行 `killAllByName(...)`**：
- `nginx_provider.go:392` → `killAllByName("nginx.exe")`
- `php_provider.go:254` → `killAllByName("php-cgi.exe")`
- `mysql_provider.go:276` → `killAllByName("mysqld.exe")`
- `redis_provider.go:198` → `killAllByName("redis-server.exe")`

按镜像名全系统强杀的后果：
- PHP 多版本共存（`phpWorkerCount=4`/版本，runtime/manager.go:265-281）→ 停一个版本杀掉所有版本；
- 用户机器上独立安装的 nginx / MySQL（含 Windows 服务 `mysqld.exe`）被误杀，MySQL 服务被强杀后处于崩溃状态不会自愈；
- 这与本次未提交改动 `stopOwnedProcessByPort`（按 exe 路径归属判断，stop.go:14-28）的正确方向直接矛盾——后者才是对的。

`removeAll`（卸载时调用）的 `taskkill` 另有 bug：
- ① 同样全系统误杀（卸载 `nginx@1.20` 会杀掉并行的 `nginx@1.26` 及用户自己的进程）；
- ② **服务未运行时 taskkill 返回非零 → `Uninstall`/`UninstallVersion` 对已停止服务必然失败**（manager.go:1064-1068 把"无进程匹配"当错误返回）。

### 修复方向

- 彻底删除 `killAllByName` 与 `removeAll` 里的 taskkill 循环；
- 只停自有 PID：`s.process` 句柄 + `StopProcessTree` + `stopOwnedProcessByPort`；
- "无进程匹配"视为成功。

---

## P0-3 端口占用"收养"未校验归属 → 接管并杀掉外部进程 ✅ 已修复

位置：`nginx_provider.go:322-331`、`mysql_provider.go:229-245`、`redis_provider.go:151-157`、`php_provider.go:207-212`

### 现象与断点

`Start` 里 `FindPIDByPort` 后：
- Nginx：只判断镜像名含 `nginx.exe`（nginx_provider.go:324-327）——不校验 exe 路径是否在 DevStack 安装目录；
- MySQL/Redis：**完全不做任何校验**，只要端口上有进程就 `i.pid = p` 并标记 `Running`（mysql_provider.go:231-233）。

随后一次 `Stop` 就会 `Kill` 这个外部进程（process.go:184-199 回退 `os.FindProcess(pid).Kill()`）——用户的系统 MySQL/nginx/redis 被 DevStack 声明为"运行中"并最终被强杀。

对比 manager 侧已有正确的 `exePathMatchesPID`（manager.go:307-314）——Provider 侧未复用。

### 修复方向

收养前必须 `exePathMatchesPID(pid, installDir, kind, version)`；不匹配则 Start 报"端口被占用"而非收养。

---

## P0-4 压缩包解压路径逃逸（zip-slip）+ 任意文件读 ✅ 已修复

位置：`internal/download/manager.go:148,181`；`app.go:998`；`internal/engine/snapshot.go:48,91`；`app.go:1118`

### 现象与断点

- 解压：`fpath := filepath.Join(dest, f.Name)`（manager.go:181）与 `filepath.Join(dest, relative)`（manager.go:148）均不校验条目是否含 `..`。恶意/被污染的镜像压缩包可把文件写到安装目录之外（覆盖 `settings.toml`/`projects.json`/任意用户可写文件）。
- `ReadLogFile`（app.go:998）把前端传的 `path` 直通 `os.ReadFile` → 任意文件读取。
- `DeleteSnapshot`/`GetSnapshotDetail`（app.go:1349/1345）把前端传的 `snapshotID` 直通 `filepath.Join(baseDir, id+".json")`，`../` 可越权读删应用自身的任意 `.json`。
- `DownloadService` 的 `destDir` 由用户可控的 `version` 拼出（app.go:1118），`version=".."` 可逃逸安装目录。

### 修复方向

- 解压前校验 `filepath.Clean(fpath)` 必须位于 `filepath.Clean(dest)` 内，拒绝含 `..` 的条目；
- 日志路径/snapshotID 做包含检查或正则白名单（如 `^snapshot-\d{8}-\d{6}$`）；
- `serviceType`/`version` 做 allowlist 或与版本列表比对。

---

## P0-5 持久化全部非原子写 + 读取错误静默吞掉 ✅ 已修复

位置：`config/config.go:206`、`environment/manager.go:164`、`engine/state_store.go:56`、`project/manager.go:347`、`resource/port.go:102`、`engine/snapshot.go:38`

### 现象与断点

- 全部直接 `os.WriteFile`，崩溃/断电时产生截断文件；
- 读取侧 `ReadInConfig`/`Unmarshal` 失败一律 `return`（config.go:60、environment/manager.go:140、project/manager.go:329）→ 静默。

后果：
- `settings.toml` 撕掉 → 设置静默复位为默认；
- `environments.json` / `projects.json` 撕掉 → **自定义环境/项目整份消失**；
- `port_pool.json` 撕掉 → 端口漂移/冲突；
- `environment.json` 撕掉 → 服务仍在跑但 UI 认为无激活环境。

### 修复方向

统一 `写 tmp → flush → rename`（Windows `MoveFileEx` 原子替换）；加载失败落日志并保留 `.bak`，不再静默吞错。

---

# P1 — 高价值架构 / 稳定性

## P1-1 Provider Start/Stop 无互斥 + 状态卡死 ✅ 已修复

位置：`internal/provider/*.go`、`internal/service/manager.go:331-403,405-494`

- `Manager.Start/Stop` 都在锁外执行 `entity.Start/Stop`（manager.go:348/428），但 Provider 实例**没有任何锁**（结构体不 import sync）。并发 Start+Stop 互杀/互拉；`Stop` 超时后状态卡在 `Stopping`；`Start` 失败后 manager 把 `Error` 拍平为 `Stopped`（manager.go:368-375），UI 看不到失败原因。
- `p.instance` 是单字段（nginx/mysql/redis Provider），并发 `NewInstance` 互相覆盖；PHP 用 map 但无锁，Uninstall 并发 delete 可 panic（php_provider.go:111-121）。
- Provider 反复临时 `process.NewManager()`（nginx_provider.go:335 等）而丢弃注入的 `pm` → 进程树/输出采集/退出通道全部丢失，`watchProcess`/`checkServiceHealth` 看不到真实句柄。

**修复**：实例加 `sync.Mutex` + 单飞标志；所有退出路径重置状态并保留错误；进程统一走注入的 `procMgr`。

## P1-2 环境切换失败无回滚，半启状态残留 ✅ 已修复

位置：`internal/engine/reconciler.go:124-151,201-213`

- `Activate`：旧环境已停，新环境启动到第 3 个单元失败 → 前 2 个已启动单元**继续运行**，`SaveActive` 不执行，激活标记仍指向已停掉的旧环境 → UI 与真实进程状态分裂。
- `stopUnits` 吞掉全部停止错误（`:211/:213`）；`Deactivate` 即使停止失败也清空激活标记（:192）。

**修复**：`startUnits` 失败时按逆序停掉本次已启动单元再返回；停止阶段收集错误并上报；仅在全成功后才落 `SaveActive`。

## P1-3 MySQL 首次初始化隐患 ✅ 已修复

位置：`internal/provider/mysql_provider.go:71,117,144-146,209-215`；`service/mysql.go:21,62-72`

- Provider 用**版本隔离** datadir（`data/mysql/<version>`），而 `conf/my.ini` 与 `service/mysql.go` 写**共享** datadir（`data/mysql`）——两套路径并存，若 ini 被采纳则两个版本共享一个 datadir。
- `log_error=<mysqlDir>\logs\error.log`（mysql_provider.go:144-146）但从不创建 `logs` 目录 → 首次启动 mysqld 打不开日志直接退出，Start 失败。
- `--initialize-insecure` 后 root 空密码，无强制设密流程。

**修复**：统一版本化 datadir；写 ini 前 `MkdirAll(logs)`；初始化后引导设置 root 密码。

## P1-4 Redis 配置保存被 `;` 污染 ✅ 已修复

位置：`internal/service/helpers.go:50-60` + `service/redis.go:33-48`

- `helpers.go:52-55` 给所有 `key value` 追加 `;`（为 PHP ini 设计）。Redis 不支持 `;` 结尾 → 保存 `port 6380;`/`maxmemory 256mb;` 后 **redis-server 下次无法启动**，且 Provider 拒绝重新生成已有 conf（redis_provider.go:110-112），污染持续。

**修复**：redis 路径去掉 `;` 追加。

## P1-5 下载并发/取消/校验缺失 ✅ 已修复

位置：`app.go:1089` + `internal/download/manager.go:45-51,69,25,232-256`

- 同版本并发下载写同一 `.tmp` 文件互相截断 → 损坏压缩包被解压并 `MarkInstalled`；无 in-flight 去重、无"已安装"跳过。
- 取消是死代码（`cancels` 只有读没有写），HTTP 请求不用 context，无法中断。
- 回滚补偿 `DeleteDir(destDir)`（manager.go:241）在重下载失败时**删除已有的正常安装目录**。
- `ReleaseInfo.Checksum` 声明了但从未被填充/校验，第三方镜像文件无完整性验证。

**修复**：按 `service@version` 加 in-flight 单飞；`http.NewRequestWithContext`；事务里记录 destDir 是否先前已存在、仅在新建时补偿删除；补齐 SHA256 校验。

## P1-6 StateStore 无锁 + 两种写形状 + 调用方吞错 ✅ 已修复

位置：`internal/engine/state_store.go:40-60`；`app.go:362,421,515`

- `Save`（全量）与 `SaveActive`（仅 active id）写同一 `environment.json`，无锁、非原子；并发 `Load` 可能读到半写文件。
- 所有调用方 `oldActiveID, _ := a.getActiveEnvironmentID()` 丢弃错误 → 读到损坏文件时 UI 认为"无激活环境"。

**修复**：StateStore 加互斥 + 原子写 + 把错误上抛到 UI。

## P1-7 `process.Manager` 输出缓冲泄漏 ✅ 已修复

位置：`internal/process/process.go:159-168`

- `cmd.Wait()` 收尾只 `delete(processes)` 和 `delete(exitChans)`，**从不 `delete(outputs)`** → 每次启动过的进程都永久留存一个 `*ProcessOutput`，长期运行（PHP 高频重启）内存无界增长。
- 附带：`GetOutput(pid)` 未命中返回 nil；滚动缓冲 `po.stdout[len(data):]` 会断行丢字节。

**修复**：退出时清理 `outputs`；把环形缓冲改为按行切割。

## P1-8 `nginx -s reload` 损坏 ✅ 已修复

位置：`internal/provider/nginx_provider.go:443-453,335`

- `Start` 以 `workdir=nginxDir` 启动（相对 `logs/nginx.pid`），但 `reloadCommand.run()` **未设 `cmd.Dir`** → 在当前工作目录找 pid 文件 → reload 失效（除非 CWD 恰好是 nginx 目录）；且只判 `p.instance.pid>0`，无锁。

**修复**：`cmd.Dir = c.nginxDir`（或 `-p` 指定前缀），并纳入实例互斥。

## P1-9 `GetService` 裸返回共享指针 → 数据竞争 ✅ 已修复

位置：`internal/service/manager.go:1000-1004` + `app.go:795`

- `GetService` 直接返回 `*ServiceInstance`，调用方在锁外读 `Info.Status`（`ApplyServiceConfig`）→ 与 `Stop/Start` 的写竞争。`checkServiceHealth`（manager.go:593）同样在锁外访问 `s.process.ExtraPIDs`。

**修复**：返回 `core.ServiceInfo` 值拷贝，或提供带锁的访问方法。

## P1-10 PortPool 锁内做阻塞探测 ✅ 已修复

位置：`internal/resource/port.go:36-57,117-124`

- `Allocate` 持写锁对候选端口逐个 `net.Listen`（池耗尽时最多 ~2000 次阻塞探测），期间所有分配/释放调用阻塞；`Release` 不校验 owner，补偿误释放可能把他人端口释放掉。

**修复**：探测移出临界区/短路；`Release(port, owner)` 校验归属。

---

# P2 — 前端（Svelte 5）稳定性与正确性

| # | 问题 | 位置 | 说明 / 修复 | 状态 |
|---|------|------|-------------|------|
| F1 | `safeInvoke` 失败后**再抛出**，busy 标志永不复位 → 按钮永久"处理中"，且大量无 catch 调用产生未处理 rejection | `safeInvoke.ts:100`；`Sites.svelte:127-134/189-203`、`Packages.svelte:156-170`、`SetupWizard.svelte:55-81` | 默认不抛（加 `rethrow` 选项）或返回 `{ok,result}`；busy 用 `try/finally` | ✅ 已修复 |
| F2 | `runOperation` 无超时，后端挂起 → 全屏遮罩永久锁死、后续操作全被去重拒绝 | `stores.ts:145-179`、`GlobalOperation.svelte` | `Promise.race` 超时 + 手动关闭按钮 | ✅ 已修复 |
| F3 | `typecheck` 是空操作——tsc 不检查 `.svelte`，且未装 `svelte-check` | `package.json:11`、`tsconfig.json` | 加 `svelte-check` 进 typecheck/CI | ✅ 已修复 |
| F4 | `api.listEnvironmentProfiles` 不存在 → Sites"关联环境模板"功能静默失效（被 catch 掩盖） | `api.ts`（缺）/`Sites.svelte:36` | 补 `listEnvironmentProfiles` 绑定 | ✅ 已修复 |
| F5 | `EventsOff` 按事件名删光所有监听，下载/操作事件在多组件订阅时互相误伤 | `events.ts:42,54,99` | `EventsOn` 返回的 cancel 函数直接作为卸载回调 | ✅ 已修复 |
| F6 | `operation:end(ok=false)` 与 `runOperation` 成功路径竞争，失败的 op 可能被报成成功 | `App.svelte:75-84`、`stores.ts:164` | `runOperation` 提交前复查 `nanoOperation` 状态 | ✅ 已修复 |
| F7 | Sites 仍用旧式 `$:` + 非 `$state` 对象 → 内联编辑不触发重渲染（靠 `editing = editing` 自赋值硬撑） | `Sites.svelte:136,153-228`、`biome.json` | 迁到 `$state`/`$derived` | ✅ 已修复 |
| F8 | 匿名回调共用一个 debounce key `'anonymous'` → 无关操作互相去重静默丢弃 | `safeInvoke.ts:60-64` | 强制显式 key 或单调计数器 | ✅ 已修复 |
| F9 | GlobalLoading 每 8s 无条件清 loading，与 15s 安全超时重复且误伤长操作 | `GlobalLoading.svelte:12-22` | 删除，只留 per-call 超时 | ✅ 已修复 |
| F10 | Logs 自动滚动失效（`$effect` 未读 `logEntries`）；隐藏 tab 每 3s 仍轮询后端 | `Logs.svelte:90-107`；视图全挂载（`App.svelte:206-212`） | effect 依赖 `logEntries.length`；隐藏时不轮询 | ✅ 已修复 |
| F11 | 启动时 `getSettings` 任意失败都弹设置向导；15s 竞速 timer 不清理 | `App.svelte:87-95` | 仅 `installDir` 为空才引导；`finally` 清理 timer | ✅ 已修复 |
| F12 | 下载取消后允许立即重入 → 同一版本并发下载 | `Packages.svelte:140-150` | 保持"取消中"直到原 promise 结束 | ✅ 已修复 |
| F13 | 全 IPC 层 `~80 处 as any`、`atom<any>` 存储，类型空转 | `api.ts:289-385`、`stores.ts` | 用生成的 `models.ts` 强类型 | ❌ 未修复（遗留） |
| F14 | 每次 `reactive:*` 事件所有隐藏视图并行 `load()`（一次事件 ~12 次 IPC） | `Dashboard.svelte:78-86` 等 | 只刷新活动视图/可见性门控 | ✅ 已修复 |

---

# P3 — 技术债清理 ✅ 已修复

- **graph**：`RemoveNode` 不清 `rev` 反向边 → `DependentsOf`/拓扑序错误（graph.go:101-118）；`Resolver.Resolve` 无锁写 `graph` 指针（resolver.go:36-51）；`TopologicalSort` 与 `GetStartOrder` 重复实现。✅ 已修复：RemoveNode 清理所有 rev 反向引用；Resolver 加 sync.Mutex；删除重复 GetStartOrder，统一调用 TopologicalSort。
- **tx**：`Rollback` 无锁读 `step.executed`，与并发 `Commit` 竞争（transaction.go:104-162）；`tx.ReleasePort` 与 `PortPool.Release` 签名不兼容（死代码，compensate.go:82-86）。✅ 已修复：Rollback 读取 step.executed 前加 t.mu.RLock()；ReleasePort 签名已兼容。
- **snapshot**：无互斥；秒级 ID 同秒覆盖旧快照（snapshot.go:29-38）；`RestoreLastSnapshot` 加载后丢弃（no-op 陷阱，snapshot.go:107-115）。✅ 已修复：SnapshotManager 加 sync.Mutex；ID 追加 atomic 计数器防碰撞；RestoreLastSnapshot 返回 EnvironmentState。
- **reactive**：`Unsubscribe` 按类型删光全部 handler（engine.go:133-138）；每事件起 goroutine 无界（配合 crash 风暴堆积 sleep goroutine）；`NewEvent` ID 纳秒碰撞（events.go:35-43）。✅ 已修复：Unsubscribe 按函数指针精确删除；dispatch/executeRules 加 semaphore 限流；NewEvent ID 追加 atomic 计数器。
- **environment**：`Delete` 保存失败不回滚内存态（manager.go:240-252）；`validateUnits` 不校验 kind 白名单/版本存在性（manager.go:254-264）。✅ 已修复：Delete 失败时 re-insert 到内存态；validateUnits 加 kind 白名单校验。
- **tray/autostart**：`Start` 非幂等（tray.go:34-72）；`autostart_windows.go:59-70` 用 `SET_VALUE` 权限句柄做 `GetStringValue`（查询会被拒绝 → 每次启动都重写注册表）。✅ 已修复：Start 加 CompareAndSwap 幂等守卫；enableRegistry 加 QUERY_VALUE 权限。
- **log**：`OpStore.Add` 持写锁做磁盘 IO（ops.go:34-61）；`LogWatcher` 是死代码且 `ReadRecent` 时间戳全用 `time.Now()`（watcher.go:101）；wails.log 句柄从不关闭。✅ 已修复：Add 磁盘 IO 移出锁外；ReadRecent 解析日志行时间戳；wails.log 存储为包级变量。
- **runtime**：`Start/Stop` 全程持 `m.mu`（PHP 启动最长 ~6s 阻塞 List，runtime/manager.go:160-202）；PHP Provider 单 worker vs nginx 4 端口 upstream 不匹配（php_provider.go:196-199 vs nginx_provider.go:485-489）+ 两套端口分配机制并存（php_provider.go:80-88 vs runtime/php.go:50-58）。✅ 已修复：Start/Stop 缩短持锁时间，sleep 等待前释放锁；PHP worker 数与 nginx upstream 需进一步统一（P3-遗留）。

---

# 建议落地顺序

1. **P0 全部先做**：P0-1 自愈状态机（优先级最高，现在自动重启实际是坏的）、P0-2 移除全系统 taskkill、P0-3 端口收养校验、P0-5 原子持久化。
2. **P1**：P1-4（Redis `;`，一行级修复）、P1-3/P1-5/P1-7 紧随；P1-1 Provider 互斥是并发正确性底座。
3. **补测试**（对应 `docs/code-review-issues.md` P2-3，仍未做）：`service.Manager`（并发 Start/Stop、自愈不复活已停服务）、`process.Manager`（output 清理）、reconciler（失败回滚）——以上 P0/P1 均为手工逻辑，目前**零单测守护**。
4. **前端**：F3 先装 `svelte-check` 让类型检查真正生效，再按 F1→F2→F4→F6 修复，其余按序。

---

> 说明：本报告基于 2026-08-18 代码快照，经 2026-08-19 二次审查确认修复状态。P0 全部修复（5/5）；P1 全部修复（10/10）；P2 修复 13/14（F13 类型未强化）；P3 全部修复（8/8），其中 runtime PHP worker 数统一为遗留项。
