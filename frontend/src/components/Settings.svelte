<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { addToast } from '../lib/stores'
import { applyTheme, DARK_THEMES, findTheme, LIGHT_THEMES, type ThemeOption } from '../lib/themes'

type SaveState = 'idle' | 'saving' | 'saved' | 'error'

type InstallDirCheck = {
  currentDir: string
  targetDir: string
  valid: boolean
  errors: string[]
  warnings: string[]
  targetExists: boolean
  targetEmpty: boolean
  sourceSize: number
  fileCount: number
  running: string[]
  downloads: string[]
}

let settings = $state({ installDir: '', startOnBoot: false, minimizeToTray: true, theme: 'light', language: 'zh-CN' })
let systemInfo = $state<Record<string, string>>({})
let configPath = $state('')
let loading = $state(true)
let loadError = $state('')

let saveState = $state<SaveState>('idle')
let savedAt = $state('')
let saveTimer: ReturnType<typeof setTimeout> | null = null
let saveSeq = 0
let dirty = false
let inFlight: Promise<void> | null = null

let showMigrate = $state(false)
let migrateTarget = $state('')
let checking = $state(false)
let dirCheck = $state<InstallDirCheck | null>(null)
let checkError = $state('')
let stopping = $state(false)
let migrating = $state(false)
let migratePercent = $state(0)
let migrateMessage = $state('')
let migrateError = $state('')
let migrateDone = $state(false)
let removeOld = $state(false)
let migrationWarnings = $state<string[]>([])
let newInstallDir = $state('')

const infoLabels: Record<string, string> = {
  platform: '操作系统',
  arch: '架构',
  appVersion: '应用版本',
}

const currentTheme = $derived(findTheme(settings.theme))

function errorText(e: any): string {
  return String(e?.message || e || '未知错误').replace(/^Error:\s*/, '')
}

function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let idx = 0
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024
    idx++
  }
  return `${value >= 10 || idx === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[idx]}`
}

async function load() {
  try {
    const [s, info, path] = await Promise.all([api.getSettings(), api.getSystemInfo(), api.getConfigPath()])
    settings = { ...settings, ...s }
    systemInfo = info
    configPath = path
    applyTheme(settings.theme)
  } catch (e) {
    console.error(e)
    loadError = `读取设置失败：${errorText(e)}`
  } finally {
    loading = false
  }
}

function scheduleSave() {
  dirty = true
  saveState = 'saving'
  const seq = ++saveSeq
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    saveTimer = null
    void flushSave(seq)
  }, 500)
}

async function flushSave(seq = ++saveSeq) {
  if (!dirty) return
  const snapshot = { ...settings }
  inFlight = (async () => {
    try {
      await api.saveSettings(snapshot)
      if (seq === saveSeq) {
        dirty = false
        saveState = 'saved'
        savedAt = new Date().toLocaleTimeString('zh-CN', { hour12: false })
      }
    } catch (e) {
      if (seq === saveSeq) saveState = 'error'
      addToast(`设置保存失败：${errorText(e)}`, 'error', 5000)
    } finally {
      inFlight = null
    }
  })()
  return inFlight
}

async function persistNow() {
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  if (inFlight) await inFlight
  if (dirty) await flushSave()
}

function selectTheme(theme: ThemeOption) {
  settings.theme = theme.id
  applyTheme(theme.id)
  scheduleSave()
}

function handleChange() {
  scheduleSave()
}

function openMigrate() {
  if (!settings.installDir) {
    addToast('请先完成初始化设置', 'warning', 4000)
    return
  }
  migrateTarget = ''
  dirCheck = null
  checkError = ''
  migrateError = ''
  migrateDone = false
  migratePercent = 0
  migrateMessage = ''
  removeOld = false
  migrationWarnings = []
  newInstallDir = ''
  showMigrate = true
}

function closeMigrate() {
  if (migrating) return
  showMigrate = false
}

async function pickTarget() {
  try {
    const path = await api.selectInstallDir(migrateTarget || settings.installDir)
    if (path) {
      migrateTarget = path
      await runCheck()
    }
  } catch (e) {
    checkError = errorText(e)
  }
}

async function runCheck() {
  const target = migrateTarget.trim()
  if (!target) {
    dirCheck = null
    checkError = ''
    return
  }
  checking = true
  checkError = ''
  try {
    dirCheck = (await api.checkInstallDirChange(target)) as InstallDirCheck
  } catch (e) {
    dirCheck = null
    checkError = errorText(e)
  } finally {
    checking = false
  }
}

async function stopRunning() {
  stopping = true
  try {
    const env: any = await api.getEnvironment()
    const serviceIDs = Object.entries(env?.services || {})
      .filter(([, s]: [string, any]) => s?.state === 'running')
      .map(([id]) => id)
    const runtimeIDs = Object.entries(env?.runtimes || {})
      .filter(([, r]: [string, any]) => r?.state === 'running' || r?.running)
      .map(([id]) => id)
    const results = await Promise.allSettled([
      ...serviceIDs.map((id) => api.stopService(id)),
      ...runtimeIDs.map((id) => api.stopRuntime(id)),
    ])
    const failed = results.filter((r) => r.status === 'rejected').length
    if (failed > 0) addToast(`有 ${failed} 个组件停止失败，请到控制面板重试`, 'error', 5000)
    else addToast('组件已全部停止', 'success', 3000)
  } catch (e) {
    addToast(`停止组件失败：${errorText(e)}`, 'error', 5000)
  } finally {
    stopping = false
  }
  await runCheck()
}

async function startMigration() {
  if (!dirCheck?.valid || dirCheck.running.length > 0 || checking || migrating) return
  migrateError = ''
  migrating = true
  migrateDone = false
  migratePercent = 0
  migrateMessage = '正在准备迁移…'
  try {
    await persistNow()
    const result: any = await api.migrateInstallDir(dirCheck.targetDir, removeOld)
    newInstallDir = result?.installDir || dirCheck.targetDir
    migrationWarnings = result?.warnings || []
    settings.installDir = newInstallDir
    systemInfo = { ...systemInfo, installDir: newInstallDir }
    configPath = await api.getConfigPath().catch(() => configPath)
    dirty = false
    saveSeq++
    saveState = 'saved'
    savedAt = new Date().toLocaleTimeString('zh-CN', { hour12: false })
    migrateDone = true
    migratePercent = 100
    addToast('安装目录迁移完成', 'success', 4000)
  } catch (e) {
    migrateError = errorText(e)
  } finally {
    migrating = false
  }
}

async function openFolder(path: string) {
  try {
    await api.openFolder(path)
  } catch (e) {
    addToast(`打开目录失败：${errorText(e)}`, 'error', 4000)
  }
}

onMount(() => {
  void load()
  const unsubProgress = api.onInstallDirProgress((data: any) => {
    if (!migrating) return
    if (typeof data?.percent === 'number') migratePercent = Math.max(0, Math.min(100, data.percent))
    if (data?.message) migrateMessage = data.message
  })
  return () => {
    unsubProgress()
    if (saveTimer) clearTimeout(saveTimer)
  }
})
</script>

<div class="mx-auto flex w-full max-w-6xl flex-col gap-4">
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div>
      <h1 class="text-xl font-bold">系统设置</h1>
      <p class="mt-1 text-sm text-base-content/60">界面外观、启动行为与安装位置</p>
    </div>
    <div class="flex h-7 items-center gap-1.5 rounded-full border border-base-300 bg-base-100 px-3 text-xs">
      {#if saveState === 'saving'}
        <span class="loading loading-spinner loading-xs text-primary"></span>
        <span class="text-base-content/60">保存中…</span>
      {:else if saveState === 'error'}
        <span class="h-1.5 w-1.5 rounded-full bg-error"></span>
        <span class="text-error">保存失败</span>
      {:else if saveState === 'saved'}
        <span class="h-1.5 w-1.5 rounded-full bg-success"></span>
        <span class="text-base-content/60">已保存{savedAt ? ` · ${savedAt}` : ''}</span>
      {:else}
        <span class="h-1.5 w-1.5 rounded-full bg-base-content/30"></span>
        <span class="text-base-content/50">修改自动保存</span>
      {/if}
    </div>
  </div>

  {#if loading}
    <div class="space-y-4">
      <div class="h-64 animate-pulse rounded-2xl border border-base-300 bg-base-100"></div>
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div class="h-56 animate-pulse rounded-2xl border border-base-300 bg-base-100"></div>
        <div class="h-56 animate-pulse rounded-2xl border border-base-300 bg-base-100"></div>
      </div>
    </div>
  {:else if loadError}
    <div class="flex items-center gap-3 rounded-2xl border border-error/30 bg-error/10 px-5 py-4 text-sm text-error">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-5 w-5 shrink-0"><circle cx="12" cy="12" r="9"/><path d="M12 8v4M12 16h.01"/></svg>
      <span class="flex-1">{loadError}</span>
      <button class="btn btn-ghost btn-xs" onclick={() => { loading = true; loadError = ''; load() }}>重试</button>
    </div>
  {:else}
    <!-- 外观主题 -->
    <section class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-base-200 px-5 py-4">
        <div class="flex items-center gap-3">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 0 0 18z" fill="currentColor" stroke="none"/><circle cx="9" cy="9" r="1.2" fill="currentColor" stroke="none"/><circle cx="9" cy="15" r="1.2" fill="currentColor" stroke="none"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">外观主题</h2>
            <p class="text-xs text-base-content/50">按浅色 / 深色分组，选择后立即生效</p>
          </div>
        </div>
        <div class="flex items-center gap-2 text-xs text-base-content/60">
          <span class="badge badge-sm border-base-300 bg-base-200/70">{currentTheme.mode === 'light' ? '浅色' : '深色'}</span>
          <span class="font-medium text-base-content/80">{currentTheme.name}</span>
          <span class="text-base-content/40">{currentTheme.en}</span>
        </div>
      </div>

      <div class="space-y-5 p-5">
        <div>
          <div class="mb-2.5 flex items-center gap-2.5">
            <span class="flex items-center gap-1.5 rounded-md bg-warning/10 px-2 py-1 text-xs font-medium text-warning">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
              浅色 · {LIGHT_THEMES.length}
            </span>
            <div class="h-px flex-1 bg-base-200"></div>
          </div>
          <div class="grid grid-cols-2 gap-2.5 sm:grid-cols-3 xl:grid-cols-4">
            {#each LIGHT_THEMES as theme (theme.id)}
              <button
                type="button"
                class="group relative rounded-xl border p-2 text-left transition-all {settings.theme === theme.id ? 'border-primary bg-primary/5 ring-2 ring-primary/25' : 'border-base-300 bg-base-100 hover:border-primary/50 hover:shadow-sm'}"
                aria-pressed={settings.theme === theme.id}
                onclick={() => selectTheme(theme)}
              >
                <div class="overflow-hidden rounded-lg border border-base-300/70" data-theme={theme.id}>
                  <div class="flex h-[62px]">
                    <div class="flex w-8 shrink-0 flex-col gap-1 bg-base-200 p-1.5">
                      <span class="h-1.5 w-full rounded-full bg-primary"></span>
                      <span class="h-1 w-3/4 rounded-full bg-base-content/25"></span>
                      <span class="h-1 w-full rounded-full bg-base-content/10"></span>
                      <span class="h-1 w-2/3 rounded-full bg-base-content/10"></span>
                    </div>
                    <div class="min-w-0 flex-1 bg-base-100 p-2">
                      <div class="flex items-center gap-1">
                        <span class="h-1.5 w-8 rounded-full bg-base-content/50"></span>
                        <span class="ml-auto h-2 w-2 rounded-full bg-secondary"></span>
                      </div>
                      <div class="mt-2 h-3 rounded bg-base-200"></div>
                      <div class="mt-1.5 flex gap-1">
                        <span class="h-3.5 flex-1 rounded bg-primary"></span>
                        <span class="h-3.5 flex-1 rounded bg-accent/70"></span>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="mt-2 flex items-center gap-1.5 px-0.5">
                  <span class="text-[13px] font-medium">{theme.name}</span>
                  <span class="truncate text-[11px] text-base-content/40">{theme.en}</span>
                  {#if settings.theme === theme.id}
                    <span class="ml-auto flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-primary text-primary-content">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" class="h-2.5 w-2.5"><path d="M20 6 9 17l-5-5"/></svg>
                    </span>
                  {/if}
                </div>
              </button>
            {/each}
          </div>
        </div>

        <div>
          <div class="mb-2.5 flex items-center gap-2.5">
            <span class="flex items-center gap-1.5 rounded-md bg-base-200 px-2 py-1 text-xs font-medium text-base-content/70">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
              深色 · {DARK_THEMES.length}
            </span>
            <div class="h-px flex-1 bg-base-200"></div>
          </div>
          <div class="grid grid-cols-2 gap-2.5 sm:grid-cols-3 xl:grid-cols-4">
            {#each DARK_THEMES as theme (theme.id)}
              <button
                type="button"
                class="group relative rounded-xl border p-2 text-left transition-all {settings.theme === theme.id ? 'border-primary bg-primary/5 ring-2 ring-primary/25' : 'border-base-300 bg-base-100 hover:border-primary/50 hover:shadow-sm'}"
                aria-pressed={settings.theme === theme.id}
                onclick={() => selectTheme(theme)}
              >
                <div class="overflow-hidden rounded-lg border border-base-300/70" data-theme={theme.id}>
                  <div class="flex h-[62px]">
                    <div class="flex w-8 shrink-0 flex-col gap-1 bg-base-200 p-1.5">
                      <span class="h-1.5 w-full rounded-full bg-primary"></span>
                      <span class="h-1 w-3/4 rounded-full bg-base-content/25"></span>
                      <span class="h-1 w-full rounded-full bg-base-content/10"></span>
                      <span class="h-1 w-2/3 rounded-full bg-base-content/10"></span>
                    </div>
                    <div class="min-w-0 flex-1 bg-base-100 p-2">
                      <div class="flex items-center gap-1">
                        <span class="h-1.5 w-8 rounded-full bg-base-content/50"></span>
                        <span class="ml-auto h-2 w-2 rounded-full bg-secondary"></span>
                      </div>
                      <div class="mt-2 h-3 rounded bg-base-200"></div>
                      <div class="mt-1.5 flex gap-1">
                        <span class="h-3.5 flex-1 rounded bg-primary"></span>
                        <span class="h-3.5 flex-1 rounded bg-accent/70"></span>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="mt-2 flex items-center gap-1.5 px-0.5">
                  <span class="text-[13px] font-medium">{theme.name}</span>
                  <span class="truncate text-[11px] text-base-content/40">{theme.en}</span>
                  {#if settings.theme === theme.id}
                    <span class="ml-auto flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-primary text-primary-content">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" class="h-2.5 w-2.5"><path d="M20 6 9 17l-5-5"/></svg>
                    </span>
                  {/if}
                </div>
              </button>
            {/each}
          </div>
        </div>
      </div>
    </section>

    <div class="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
      <!-- 安装目录 -->
      <section class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
        <div class="flex items-start justify-between gap-3 border-b border-base-200 px-5 py-4">
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-info/10 text-info">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
            </span>
            <div>
              <h2 class="text-sm font-semibold">安装目录</h2>
              <p class="text-xs text-base-content/50">组件、站点数据、数据库与日志的存放位置</p>
            </div>
          </div>
          <button class="btn btn-primary btn-sm shrink-0" onclick={openMigrate} disabled={!settings.installDir}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><path d="M5 12h14M13 6l6 6-6 6"/></svg>
            迁移目录
          </button>
        </div>
        <div class="space-y-3 p-5">
          <div class="rounded-xl border border-base-300 bg-base-200/40 p-3.5">
            <div class="flex items-start gap-3">
              <span class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-base-100 text-base-content/60">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-4 w-4"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
              </span>
              <div class="min-w-0 flex-1">
                <p class="break-all font-mono text-[13px] leading-5 text-base-content/85">{settings.installDir || '尚未设置'}</p>
                <p class="mt-1 text-xs text-base-content/45">该路径不可直接编辑</p>
              </div>
              <button class="btn btn-ghost btn-xs shrink-0" onclick={() => openFolder(settings.installDir)} disabled={!settings.installDir}>
                打开
              </button>
            </div>
          </div>
          <div class="flex items-start gap-2 rounded-xl border border-info/20 bg-info/10 px-3.5 py-3 text-xs leading-5 text-base-content/70">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="mt-0.5 h-4 w-4 shrink-0 text-info"><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/></svg>
            <p>
              安装目录与组件路径、Nginx/MySQL 配置、站点及数据库数据强绑定，改一个路径并不会移动已有数据。
              更换位置请使用「迁移目录」：停止所有组件后复制数据、重写配置中的绝对路径，再切换配置。
            </p>
          </div>
        </div>
      </section>

      <!-- 启动与语言 -->
      <section class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
        <div class="border-b border-base-200 px-5 py-4">
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-success/10 text-success">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.14.6.73 1 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
            </span>
            <div>
              <h2 class="text-sm font-semibold">启动与显示</h2>
              <p class="text-xs text-base-content/50">登录启动、窗口行为与界面语言</p>
            </div>
          </div>
        </div>
        <div class="space-y-3 p-5">
          <div class="flex items-center justify-between gap-4 rounded-xl border border-base-300 bg-base-200/30 px-3.5 py-3">
            <div class="min-w-0">
              <p class="text-sm">开机自启</p>
              <p class="mt-0.5 text-xs text-base-content/50">登录 Windows 后自动启动 DevStack</p>
            </div>
            <input type="checkbox" class="toggle toggle-primary toggle-sm shrink-0" bind:checked={settings.startOnBoot} onchange={handleChange} />
          </div>
          <div class="flex items-center justify-between gap-4 rounded-xl border border-base-300 bg-base-200/30 px-3.5 py-3">
            <div class="min-w-0">
              <p class="text-sm">关闭时最小化到托盘</p>
              <p class="mt-0.5 text-xs text-base-content/50">关闭窗口后从托盘图标恢复或退出</p>
            </div>
            <input type="checkbox" class="toggle toggle-primary toggle-sm shrink-0" bind:checked={settings.minimizeToTray} onchange={handleChange} />
          </div>
          <label class="flex items-center justify-between gap-4 rounded-xl border border-base-300 bg-base-200/30 px-3.5 py-3">
            <div class="min-w-0">
              <p class="text-sm">显示语言</p>
              <p class="mt-0.5 text-xs text-base-content/50">界面文案语言（重启后完全生效）</p>
            </div>
            <select class="select select-bordered select-sm w-32 shrink-0" bind:value={settings.language} onchange={handleChange}>
              <option value="zh-CN">简体中文</option>
              <option value="en">English</option>
            </select>
          </label>
        </div>
      </section>
    </div>

    <!-- 系统信息 -->
    <section class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
      <div class="border-b border-base-200 px-5 py-4">
        <div class="flex items-center gap-3">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-base-200 text-base-content/60">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M8 2v4M16 2v4M3 10h18"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">系统信息</h2>
            <p class="text-xs text-base-content/50">运行环境与配置文件位置</p>
          </div>
        </div>
      </div>
      <div class="p-5">
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {#each Object.entries(infoLabels) as [key, label] (key)}
            <div class="rounded-xl border border-base-300 bg-base-200/40 px-3.5 py-2.5">
              <span class="text-xs text-base-content/45">{label}</span>
              <div class="mt-1 break-all text-sm font-medium">{systemInfo[key] || '—'}</div>
            </div>
          {/each}
          <div class="rounded-xl border border-base-300 bg-base-200/40 px-3.5 py-2.5 sm:col-span-4">
            <div class="flex items-center justify-between gap-3">
              <div class="min-w-0">
                <span class="text-xs text-base-content/45">配置文件路径</span>
                <div class="mt-1 break-all font-mono text-xs text-base-content/70">{configPath || '—'}</div>
              </div>
              <button class="btn btn-ghost btn-xs shrink-0" onclick={() => openFolder(configPath)} disabled={!configPath}>打开</button>
            </div>
          </div>
        </div>
      </div>
    </section>
  {/if}
</div>

<!-- 迁移安装目录 -->
{#if showMigrate}
  <div
    class="fixed inset-0 z-[100] flex items-center justify-center bg-black/45 p-4 backdrop-blur-sm"
    role="dialog"
    aria-modal="true"
    aria-label="迁移安装目录"
    tabindex="-1"
    onclick={(e) => e.target === e.currentTarget && closeMigrate()}
    onkeydown={(e) => e.key === 'Escape' && closeMigrate()}
  >
    <div class="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-2xl">
      <div class="flex items-start justify-between gap-4 border-b border-base-200 px-5 py-4">
        <div class="flex items-center gap-3">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><path d="M5 12h14M13 6l6 6-6 6"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">迁移安装目录</h2>
            <p class="text-xs text-base-content/50">复制数据并重写配置中的路径，原目录默认保留</p>
          </div>
        </div>
        <button class="btn btn-ghost btn-circle btn-sm" onclick={closeMigrate} disabled={migrating} aria-label="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>

      <div class="flex-1 overflow-y-auto px-5 py-4">
        {#if migrateDone}
          <div class="flex flex-col items-center py-6 text-center">
            <span class="flex h-14 w-14 items-center justify-center rounded-full bg-success/10 text-success">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="h-7 w-7"><path d="M20 6 9 17l-5-5"/></svg>
            </span>
            <h3 class="mt-3 text-base font-semibold">迁移完成</h3>
            <p class="mt-1 max-w-md text-sm text-base-content/60">安装目录已切换到新位置，组件与站点数据均已复制。</p>
            <div class="mt-4 w-full rounded-xl border border-base-300 bg-base-200/40 px-4 py-3 text-left">
              <span class="text-xs text-base-content/45">新安装目录</span>
              <div class="mt-1 break-all font-mono text-[13px]">{newInstallDir}</div>
            </div>
            {#if migrationWarnings.length > 0}
              <div class="mt-3 w-full rounded-xl border border-warning/30 bg-warning/10 px-4 py-3 text-left text-xs leading-5 text-base-content/70">
                {#each migrationWarnings as warning}
                  <p>· {warning}</p>
                {/each}
              </div>
            {/if}
            <div class="mt-4 flex gap-2">
              <button class="btn btn-ghost btn-sm" onclick={() => openFolder(newInstallDir)}>打开新目录</button>
              <button class="btn btn-primary btn-sm" onclick={closeMigrate}>完成</button>
            </div>
          </div>
        {:else if migrating}
          <div class="py-8">
            <div class="flex items-center justify-center">
              <span class="loading loading-spinner loading-lg text-primary"></span>
            </div>
            <p class="mt-4 text-center text-sm font-medium">正在迁移安装目录</p>
            <p class="mt-1 break-all text-center text-xs text-base-content/50">{migrateMessage || '正在处理…'}</p>
            <div class="mx-auto mt-4 h-2 w-full max-w-md overflow-hidden rounded-full bg-base-200">
              <div class="h-full rounded-full bg-primary transition-all duration-300" style="width: {migratePercent}%"></div>
            </div>
            <p class="mt-2 text-center text-xs tabular-nums text-base-content/45">{migratePercent}%</p>
            <p class="mt-5 text-center text-xs text-warning">迁移期间请勿关闭应用或启动组件</p>
          </div>
        {:else}
          <div class="space-y-4">
            <div>
              <label class="mb-1.5 block text-xs font-medium text-base-content/70" for="migrate-target">新的安装目录</label>
              <div class="flex gap-2">
                <input
                  id="migrate-target"
                  class="input input-bordered min-w-0 flex-1 font-mono text-sm"
                  bind:value={migrateTarget}
                  oninput={() => { dirCheck = null; checkError = '' }}
                  onblur={runCheck}
                  placeholder="例如 D:\DevStack"
                  disabled={checking}
                />
                <button class="btn btn-outline btn-sm shrink-0" onclick={pickTarget} disabled={checking}>浏览…</button>
                <button class="btn btn-outline btn-sm shrink-0" onclick={runCheck} disabled={checking || !migrateTarget.trim()}>
                  {checking ? '检查中…' : '检查'}
                </button>
              </div>
              <p class="mt-1.5 text-xs text-base-content/45">目标目录必须为空或尚不存在；不能位于当前安装目录内或其上级。</p>
            </div>

            <div class="rounded-xl border border-base-300 bg-base-200/30 p-3.5 text-xs">
              <div class="flex items-center justify-between gap-2">
                <span class="text-base-content/50">当前安装目录</span>
              </div>
              <div class="mt-1 break-all font-mono text-base-content/75">{settings.installDir}</div>
            </div>

            {#if checkError}
              <div class="rounded-xl border border-error/30 bg-error/10 px-3.5 py-2.5 text-xs text-error">{checkError}</div>
            {/if}

            {#if dirCheck}
              <div class="space-y-2.5 rounded-xl border border-base-300 p-3.5">
                <div class="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs">
                  <span class="text-base-content/50">需要复制</span>
                  <span class="font-medium">{formatSize(dirCheck.sourceSize)} · {dirCheck.fileCount} 个文件</span>
                  <span class="ml-auto inline-flex items-center gap-1.5">
                    {#if dirCheck.valid}
                      <span class="h-1.5 w-1.5 rounded-full bg-success"></span><span class="text-success">目标可用</span>
                    {:else}
                      <span class="h-1.5 w-1.5 rounded-full bg-error"></span><span class="text-error">目标不可用</span>
                    {/if}
                  </span>
                </div>
                <div class="flex flex-wrap gap-1.5 text-[11px]">
                  <span class="rounded-md bg-base-200 px-2 py-1 text-base-content/60">
                    {dirCheck.targetExists ? (dirCheck.targetEmpty ? '目标目录为空' : '目标目录非空') : '目标目录尚不存在，迁移时创建'}
                  </span>
                </div>

                {#if dirCheck.errors.length > 0}
                  <div class="rounded-lg border border-error/25 bg-error/10 px-3 py-2 text-xs leading-5 text-error">
                    {#each dirCheck.errors as error}
                      <p>· {error}</p>
                    {/each}
                  </div>
                {/if}
                {#if dirCheck.warnings.length > 0}
                  <div class="rounded-lg border border-warning/25 bg-warning/10 px-3 py-2 text-xs leading-5 text-base-content/70">
                    {#each dirCheck.warnings as warning}
                      <p>· {warning}</p>
                    {/each}
                  </div>
                {/if}

                {#if dirCheck.running.length > 0}
                  <div class="rounded-lg border border-warning/30 bg-warning/10 px-3 py-2.5 text-xs">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="text-base-content/70">迁移前需要停止以下组件：</span>
                      {#each dirCheck.running as name}
                        <span class="rounded-md bg-warning/20 px-1.5 py-0.5 text-warning">{name}</span>
                      {/each}
                      <button class="btn btn-warning btn-xs ml-auto" onclick={stopRunning} disabled={stopping}>
                        {#if stopping}<span class="loading loading-spinner loading-xs"></span>{/if}
                        {stopping ? '正在停止…' : '全部停止'}
                      </button>
                    </div>
                  </div>
                {/if}
                {#if (dirCheck.downloads?.length ?? 0) > 0}
                  <div class="rounded-lg border border-warning/30 bg-warning/10 px-3 py-2.5 text-xs">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="text-base-content/70">以下下载任务未完成，请等待完成或先取消后再迁移：</span>
                      {#each dirCheck.downloads as name}
                        <span class="rounded-md bg-warning/20 px-1.5 py-0.5 text-warning">{name}</span>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>

              <label class="flex items-start gap-2.5 rounded-xl border border-error/20 bg-error/5 px-3.5 py-3">
                <input type="checkbox" class="checkbox checkbox-error checkbox-sm mt-0.5" bind:checked={removeOld} />
                <span class="text-xs leading-5 text-base-content/70">
                  迁移成功后删除旧目录
                  <span class="font-mono text-base-content/50">（{settings.installDir}）</span>
                  <span class="mt-0.5 block text-error/80">删除后不可恢复；如无特殊需要建议保留，确认无误后再手动清理。</span>
                </span>
              </label>
            {/if}

            {#if migrateError}
              <div class="rounded-xl border border-error/30 bg-error/10 px-3.5 py-2.5 text-xs leading-5 text-error">
                迁移失败：{migrateError}
              </div>
            {/if}
          </div>
        {/if}
      </div>

      {#if !migrateDone && !migrating}
        <div class="flex items-center justify-between gap-3 border-t border-base-200 px-5 py-3.5">
          <span class="text-[11px] text-base-content/40">迁移只复制数据，不会安装或卸载组件</span>
          <div class="flex gap-2">
            <button class="btn btn-ghost btn-sm" onclick={closeMigrate}>取消</button>
            <button
              class="btn btn-primary btn-sm"
              onclick={startMigration}
              disabled={!dirCheck?.valid || (dirCheck?.running.length ?? 0) > 0 || (dirCheck?.downloads?.length ?? 0) > 0 || checking}
            >
              开始迁移
            </button>
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}
