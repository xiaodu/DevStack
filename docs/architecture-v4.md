# DevStack Architecture v4 — Environment 模型

## 核心实体

### Unit

系统中一切可操作的软件实例。**唯一标识 = `kind@version`**。

| 示例 | 含义 |
|------|------|
| `nginx@1.24.0` | Nginx 1.24.0 |
| `mysql@5.7.44` | MySQL 5.7.44 |
| `php@7.4.33` | PHP 7.4.33 |

Unit 的属性：
- `kind` — 同类软件的类别名（nginx, mysql, php...）
- `version` — 语义版本号
- `installed` — 是否已安装（true / false）
- `running` — 是否在运行（true / false）

不存在"无版本的 Unit"。`nginx` 仅作为 kind 名，不可以作为操作对象。

### Environment

一个**命名的 Unit 清单**，是可直接激活的实例。不是模板，不是抽象层。

```
Environment "LNMP (PHP 7.4 + MySQL 5.7 + Nginx 1.20)" = [
  nginx@1.20.2,
  mysql@5.7.44,
  php@7.4.33,
  redis@7.2.5
]
```

每个 Environment 独立、完整、可直接激活。预设（built-in）是系统自带的，用户也可以创建自定义 Environment。

> **决策记录（2026-07-31）**：内置预设与自定义 Environment 完全等同，**可编辑、可删除**。预设只是一份「开箱即用的初始清单」，一旦被用户修改即视为用户自有数据，与普通 Environment 采用同一套持久化规则。

## 系统状态

系统只保存一条运行时信息：**当前激活的是哪个 Environment**。

```
activeEnvironmentId: "builtin-lnmp" | null
```

激活时从 Environment 定义中读取 Unit 清单，与任何缓存状态无关。

## 核心操作

### 安装 Unit

与任何 Environment 无关。在 Unit 维度独立进行。

```
Install(kind@version)
  → 下载 → 落盘 → Unit.installed = true
```

### 激活 Environment

先停旧的全部，再启新的全部。不关心交集。

```
Activate(envId):
  currentUnits ← 当前 active 的 Environment 的 Unit 集合（如果有）
  targetUnits  ← EnvironmentManager.Get(envId) 的 Unit 集合

  stop(所有在 currentUnits 中的 Unit)
  start(所有在 targetUnits 中的 Unit)

  activeEnvironmentId ← envId
```

前置条件：targetUnits 中所有 Unit 的 `installed == true`。不满足则报错中断。

### 停用

```
Deactivate():
  stop(当前 active 的 Environment 中的全部 Unit)
  activeEnvironmentId ← null
```

### 管理 Environment

```
CreateEnvironment(name, [kind@version, ...]) → Environment
UpdateEnvironment(id, [kind@version, ...])   // 预设与自定义同样可改
DeleteEnvironment(id)                        // 预设与自定义同样可删
```

## 持久化

两处独立持久化：

| 数据 | 存储方式 | 用途 |
|------|----------|------|
| Environment 定义 | `environments.json`（含预设+自定义） | 用户管理 Environment |
| `activeEnvironmentId` | `environment.json` | 记住当前激活的是哪个 Environment |

不复存在的内容：
- ❌ `EnvironmentState` 全量快照（services/projects/runtimes 状态缓存）
- ❌ `EnvironmentProfile`（被 Environment 替代）
- ❌ Desired State 的持久化

## 约束

1. Unit 身份是 `kind@version`，不存在裸 kind 操作。
2. Environment 只声明 Unit 清单，不持有运行时信息。
3. 激活 = 替换全集。先停旧、再启新，不优化交集。
4. 安装和激活是两个正交流程，激活中途不会隐式安装。
5. 激活时所需的信息全部来自 Environment 定义和 Unit 管理器，不从持久化缓存读取。
