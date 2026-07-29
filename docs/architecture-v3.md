# DevStack Architecture v3

## Overview
DevStack is a desktop application built with Wails v2 (Go backend) + Svelte 3 (frontend + Tailwind CSS + daisyUI). It manages local development environments (Nginx, MySQL, PHP, Redis, Node.js, Python) for Windows.

## Key Components

### Backend (Go)
- `app.go` — Wails app binding; all API methods exposed to frontend
- `internal/config/` — Viper + TOML config management, directory structure
- `internal/core/` — Core types (ServiceInfo, RuntimeInfo, Project, etc.)
- `internal/service/` — Service manager (nginx, mysql, redis)
- `internal/runtime/` — Runtime manager (php, node, python)
- `internal/project/` — Project manager (sites)
- `internal/environment/` — Environment profiles
- `internal/engine/` — State store, reconciler, snapshots
- `internal/log/` — slog + lumberjack logger, OpStore (operation log), LogWatcher
- `internal/process/` — Process manager for service binaries
- `internal/resource/` — Resource (port) manager
- `internal/reactive/` — Event engine (publish/subscribe with rules)
- `internal/provider/` — Service providers (Nginx, MySQL, PHP)
- `internal/download/` — Download/extract manager

### Frontend (Svelte + TypeScript + Tailwind + daisyUI)
- `src/main.ts` — Entry point, mounts App.svelte
- `src/App.svelte` — Layout, sidebar, navigation, GlobalLoading/GlobalToast
- `src/lib/stores.ts` — nanostores atoms + bridge to Svelte stores
- `src/lib/safeInvoke.ts` — Debounced API call wrapper with toast/loading
- `src/lib/events.ts` — Wails Events subscription helpers
- `src/lib/api.ts` — API bindings (imported from generated wailsjs/...)
- `src/components/` — Dashboard, Sites, Software, Environment, Packages, Logs, Settings, etc.

### State Management
- **nanostores** atoms for cross-component state (framework-agnostic)
  - `nanoLoading` — loading overlay visibility
  - `nanoStatusMsg` — status bar message
  - `nanoToasts` — toast notifications
  - `nanoRefresh` — signal for data refresh
  - Bidirectional `bridge()` function syncs atoms ↔ Svelte stores

### Loading Flow
1. `App.svelte` mounts → hides splash, calls `getSettings()` (15s timeout)
2. `Dashboard.svelte` mounts → calls `load()` with per-API 15s timeouts
3. Each API call wrapped in `Promise.race([call, timeout(15s)])` + catch fallback
4. All 7 API calls run in parallel, `Promise.all` waits for all wraps to settle
5. Failing calls return default values (empty arrays/objects)
6. `GlobalLoading.svelte` has 30s safety timer that auto-hides

**Logging:**
- `[Dashboard] 所有API调用已启动` - All API calls started
- `[Dashboard] ${label} 完成` - Each API call completed
- `[Dashboard] ${label} 超时或失败: ...` - If timeout occurs
- `[Dashboard] 所有API调用完成，在 ...ms 中完成` - All completed time summary

**Timeout:** Any single API call can hang indefinitely, but each has a 15s timeout that returns an empty object/array as fallback, so Dashboard should succeed even if some backend calls hang.

### Directory Structure
```
{installDir}/
  config/        — settings.toml, app state
  data/          — MySQL datadir, SSL certs
  logs/          — devstack.log (slog + lumberjack), wails.log
  programs/      — service binaries grouped by type
  www/           — project files
```

### Logging
- slog structured logger with lumberjack auto-rotation
  - Max 10MB per file, 30 days retention
  - Output: `{installDir}/logs/devstack.log`
- OpStore: in-memory circular buffer of operations (last 200)
- Wails log: `{installDir}/logs/wails.log` (standard log)

### Config
- Viper reads `{installDir}/config/settings.toml`
- No JSON migration — clean start with TOML

### Backend → Frontend Push
- Reactive engine events bridged to Wails Events in `startup()` context
- 14 event types (ServiceStarted, ServiceStopped, PortConflict, etc.)
- Frontend: `subscribeReactiveEvents()` subscribes to all, triggers `nanoRefresh`
- Download progress: `download:progress` events with {key, service, version, downloaded, total}
