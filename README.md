# DevStack

本地开发环境管理器 - 基于 Wails v2 + Go + Svelte 构建的桌面应用。

## 功能特性

- 服务管理：Nginx、MySQL、Redis 等服务的安装、启动、停止、配置
- 运行时管理：PHP、Node.js、Python 多版本共存
- 项目管理：自动配置虚拟主机、端口分配、SSL 证书
- 环境配置：保存/切换不同的环境配置方案
- 开机自启：支持 Windows 开机自动启动
- 系统托盘：最小化到托盘，后台运行

## 技术栈

- 后端：Go 1.23 + Wails v2
- 前端：Svelte 3 + Vite 3 + TypeScript + TailwindCSS + DaisyUI
- 架构：响应式事件引擎 + 状态协调器

## 目录结构

```
DevStack/
├── main.go              # 入口
├── app.go               # 核心应用逻辑（API 层）
├── internal/
│   ├── service/         # 服务管理（生命周期、健康检查）
│   ├── runtime/         # 运行时管理（PHP/Node/Python）
│   ├── project/         # 项目管理（虚拟主机配置）
│   ├── environment/     # 环境配置管理
│   ├── reactive/        # 响应式事件引擎
│   ├── engine/          # 状态协调器
│   ├── process/         # 进程管理
│   ├── download/        # 下载管理
│   ├── provider/        # 服务提供者（Nginx/MySQL/Redis）
│   └── config/          # 配置管理
└── frontend/            # Svelte 前端
```

## 构建与运行

```bash
# 开发模式
wails dev

# 生产构建
wails build

# 开发模式（启用 DevTools）
$env:WAILS_DEV="1"; wails dev
```

## 已知问题

详见 [docs/code-review-issues.md](docs/code-review-issues.md)
