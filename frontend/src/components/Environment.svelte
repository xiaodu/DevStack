<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { useDownloadPhase, useDownloadProgress } from '../lib/events'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast, operation, refreshSignal, runOperation } from '../lib/stores'

let env = $state<any>({ services: {}, projects: {}, runtimes: {}, activeProfileID: '' })
let envLoading = $state(true)
let environments = $state<any[]>([])
let envsLoading = $state(true)
// 加载失败与「暂无环境」必须区分：失败时展示横幅 + 重试，而不是空的卡片列表
let envError = $state('')
let envsError = $state('')
let loadErrors = $derived([envError, envsError].filter(Boolean))
let selectedEnv: any = null
let editingEnv = $state<any>(null)
let envModalOpen = $state(false)
let busy = $state(false)
let confirmDeleteEnvId = $state<any>(null)
// 基于后端 getInstalledVersions 构建的安装状态映射，格式: { 'nginx 1.24.0': true }
let installStatus = $state<Record<string, boolean>>({})
let installStatusReady = $state(false)

// 安装进度相关
let installModal = $state(false)
let installingEnv = $state<any>(null)
let installTasks = $state<any[]>([]) // [{ id, version, name, status: 'pending'|'downloading'|'extracting'|'done'|'error', progress: 0, phase: '', phaseLabel: '' }]
let installProgress: Record<string, any> = {} // { 'nginx@1.24.0': { downloaded: 0, total: 0 } }
let installCancelled = false // 用户点击取消时设为 true

// 监听下载进度事件（后端 key 格式 "download:nginx:1.24.0" → 前端 key "nginx@1.24.0"）
useDownloadProgress((data) => {
  if (!data || !data.key) return
  installProgress[data.key] = { downloaded: data.downloaded, total: data.total }
  const shortKey = data.key.replace('download:', '').replace(':', '@')
  installTasks = installTasks.map((t) => {
    if (`${t.id}@${t.version}` === shortKey && t.status === 'downloading') {
      return { ...t, progress: data.total > 0 ? Math.round((data.downloaded / data.total) * 100) : 0 }
    }
    return t
  })
})

// 监听下载阶段事件（downloading/extracting/configuring/done/error）
useDownloadPhase((data) => {
  if (!data || !data.service || !data.version) return
  const shortKey = `${data.service}@${data.version}`
  const phaseLabels: Record<string, string> = {
    downloading: '下载中',
    extracting: '解压中',
    configuring: '配置中',
    done: '已完成',
    error: '失败',
  }
  installTasks = installTasks.map((t) => {
    if (`${t.id}@${t.version}` === shortKey) {
      if (data.phase === 'done') {
        return { ...t, status: 'done', progress: 100 }
      }
      if (data.phase === 'error') {
        return { ...t, status: 'error', progress: 0 }
      }
      return { ...t, status: 'downloading', phase: data.phase, phaseLabel: phaseLabels[data.phase] || data.phase }
    }
    return t
  })
  installTasks = [...installTasks]
})

// 当前激活的环境 ID
let activeEnvID = $derived(env.activeProfileID || '')
let hasRunningServices = $derived(
  Object.values(env.services || {}).some((s: any) => s.state === 'running') ||
    Object.values(env.runtimes || {}).some((r: any) => r.state === 'running'),
)
let activeEnv = $derived(environments.find((e) => e.id === activeEnvID) || null)
let runningCount = $derived(
  Object.values(env.services || {}).filter((s: any) => s.state === 'running').length +
    Object.values(env.runtimes || {}).filter((r: any) => r.state === 'running').length,
)
let totalComponents = $derived(environments.reduce((sum, e) => sum + unitsOf(e).length, 0))
let totalMissing = $derived(
  installStatusReady ? environments.reduce((sum, e) => sum + checkEnvInstalled(e).missing.length, 0) : 0,
)

async function withTimeout(promise: Promise<any>, timeoutMs = 5000, fallback: any = null) {
  return Promise.race([promise, new Promise((resolve) => setTimeout(() => resolve(fallback), timeoutMs))])
}

// 首屏两路数据用「会抛错」的超时：withTimeout 的静默回退会把失败伪装成空列表，
// 用户看到「暂无环境」其实是没有取到数据（F：失败要能看出来）。
async function withTimeoutStrict(promise: Promise<any>, timeoutMs = 5000) {
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('加载超时')), timeoutMs)
      }),
    ])
  } finally {
    if (timer) clearTimeout(timer)
  }
}

async function loadEnvironment() {
  envLoading = true
  try {
    const result = await withTimeoutStrict(api.getEnvironment(), 5000)
    env = result || { services: {}, projects: {}, runtimes: {}, activeProfileID: '' }
    envError = ''
  } catch (e) {
    console.error('[Environment] load error:', e)
    envError = (e as any)?.message || String(e)
  } finally {
    envLoading = false
  }
}

async function loadEnvironments() {
  envsLoading = true
  try {
    const result = await withTimeoutStrict(api.listEnvironments(), 5000)
    environments = (Array.isArray(result) ? result : []).sort((a, b) => {
      // 预设环境排前面，然后按名称字母排序
      if (a.isBuiltin && !b.isBuiltin) return -1
      if (!a.isBuiltin && b.isBuiltin) return 1
      return (a.name || '').localeCompare(b.name || 'zh-CN')
    })
    envsError = ''
  } catch (e) {
    console.error('[Environment] 环境列表加载失败:', e)
    environments = []
    envsError = (e as any)?.message || String(e)
  } finally {
    envsLoading = false
  }
}

async function load() {
  await Promise.all([loadEnvironment(), loadEnvironments()])
  await refreshInstallStatus()
}

function unitsOf(env: any) {
  const units = env?.units
  return units ? (Array.isArray(units) ? units : Object.values(units)) : []
}

// 使用后端 installStatus 检查组件安装状态
function checkEnvInstalled(env: any) {
  const units = unitsOf(env)

  const installed = []
  const missing = []

  for (const u of units) {
    const key = `${u.kind} ${u.version}`
    if (installStatus[key]) {
      installed.push(key)
    } else {
      missing.push(key)
    }
  }

  return {
    allInstalled: missing.length === 0,
    missing,
    total: installed.length + missing.length,
    installedCount: installed.length,
  }
}

// 收集所有环境涉及的服务类型，查询后端实际安装版本
async function refreshInstallStatus() {
  installStatusReady = false
  const types = new Set<string>()
  for (const e of environments) {
    const units = Array.isArray(e.units) ? e.units : Object.values(e.units || {})
    for (const u of units) types.add(u.kind)
  }
  const map: Record<string, boolean> = {}
  for (const kind of types) {
    const versions = await withTimeout(api.getInstalledVersions(kind), 5000, []).catch(() => [])
    if (Array.isArray(versions)) {
      for (const v of versions) map[`${kind} ${v}`] = true
    }
  }
  installStatus = map
  installStatusReady = true
}

async function deactivateCurrent() {
  await runOperation({
    action: 'deactivate',
    title: '正在停止环境',
    call: () => api.deactivateEnvironment(),
    successMsg: '环境已安全停止',
    onDone: () => {
      refreshSignal.update((n) => n + 1)
      return load()
    },
  })
}

async function activateEnv(env: any) {
  await runOperation({
    action: 'activate',
    title: `正在激活「${env.name}」`,
    call: () => api.activateEnvironment(env.id),
    successMsg: `已按「${env.name}」激活并启动环境`,
    onDone: () => {
      refreshSignal.update((n) => n + 1)
      return load()
    },
  })
}

// 仅切换 activeEnvironmentId 标记，不启停任何组件
async function activateEnvOnly(env: any) {
  await runOperation({
    action: 'activate-only',
    title: `正在激活「${env.name}」`,
    call: () => api.activateEnvironmentOnly(env.id),
    successMsg: `已激活「${env.name}」（未启动组件）`,
    onDone: () => {
      refreshSignal.update((n) => n + 1)
      return load()
    },
  })
}

// 仅启动环境内的全部组件，不修改激活标记 —— 已移除 UI 入口，禁止直接运行未激活的环境
// async function runEnv(env) {
//   await runOperation({
//     action: 'run',
//     title: `正在启动「${env.name}」`,
//     call: () => api.runEnvironment(env.id),
//     successMsg: `已启动「${env.name}」的全部组件`,
//     onDone: () => {
//       refreshSignal.update((n) => n + 1)
//       return load()
//     },
//   })
// }

async function installEnvComponents(env: any) {
  if (busy) return
  const installCheck = checkEnvInstalled(env)
  if (!installCheck.missing.length) {
    addToast('所有组件已安装，可直接激活', 'info', 3500)
    return
  }

  installCancelled = false
  installTasks = []
  const units = Array.isArray(env.units) ? env.units : Object.values(env.units || {})
  for (const u of units) {
    if (installCheck.missing.includes(`${u.kind} ${u.version}`)) {
      installTasks.push({
        id: u.kind,
        version: u.version,
        name: unitNames[u.kind] || u.name || u.kind,
        status: 'pending',
        progress: 0,
      })
    }
  }

  installingEnv = env
  installModal = true
  busy = true

  for (const task of installTasks) {
    if (installCancelled) break
    task.status = 'downloading'
    installTasks = [...installTasks]
    try {
      await api.downloadService(task.id, task.version)
      if (installCancelled) break
      task.status = 'done'
      task.progress = 100
      installTasks = [...installTasks]
    } catch (e: any) {
      task.status = 'error'
      installTasks = [...installTasks]
      if (!installCancelled) {
        addToast(`安装 ${task.name} ${task.version} 失败: ${e.message || e}`, 'error', 3500)
      }
    }
  }

  for (const task of installTasks) {
    if (task.status === 'pending' || task.status === 'downloading') {
      task.status = 'error'
      task.progress = 0
    }
  }
  installTasks = [...installTasks]

  await load()
  busy = false

  if (installCancelled) {
    // 已由 cancelInstall 提示
  } else if (installTasks.every((t) => t.status === 'done')) {
    addToast('所有组件安装完成，可点击「激活并运行」启动环境', 'success', 3500)
  }
}

function cancelInstall() {
  if (installCancelled) return
  installCancelled = true
  // 通知后端真正中止所有仍在下载中的任务，否则下载继续占带宽并落盘
  for (const task of installTasks) {
    if (task.status === 'pending' || task.status === 'downloading') {
      api.cancelDownload(`download:${task.id}:${task.version}`).catch((e) => {
        console.error('取消下载失败:', e)
      })
    }
  }
  // 立即反馈：未完成任务标记为已取消并关闭弹窗，不必等待正在进行的下载结束
  installTasks = installTasks.map((t) => (t.status === 'done' ? t : { ...t, status: 'cancelled', progress: 0 }))
  installModal = false
  addToast('已取消安装', 'warning', 3500)
}

const componentMeta: Record<string, { name: string; icon: string; color: string }> = {
  nginx: { name: 'Nginx', icon: 'icon-nginx', color: '#009639' },
  mysql: { name: 'MySQL', icon: 'icon-mysql', color: '#4479A1' },
  redis: { name: 'Redis', icon: 'icon-redis', color: '#DC382D' },
  memcached: { name: 'Memcached', icon: '', color: '#5B8C5A' },
  php: { name: 'PHP', icon: 'icon-php1', color: '#777BB4' },
  node: { name: 'Node.js', icon: 'icon-Nodejs', color: '#339933' },
  python: { name: 'Python', icon: 'icon-python', color: '#3776AB' },
}

const unitNames: Record<string, string> = {
  nginx: 'Nginx',
  mysql: 'MySQL',
  redis: 'Redis',
  memcached: 'Memcached',
  php: 'PHP',
  node: 'Node.js',
  python: 'Python',
}

function closeInstallModal() {
  installModal = false
  installingEnv = null
  installTasks = []
  installProgress = {}
}

async function saveEnv() {
  if (!editingEnv.name?.trim()) {
    addToast('请填写环境名称', 'error', 3500)
    return
  }
  await safeInvoke(
    'saveEnv',
    async () => {
      if (editingEnv.id) {
        await api.updateEnvironment(editingEnv)
      } else {
        await api.createEnvironment(editingEnv)
      }
      envModalOpen = false
      editingEnv = null
      await loadEnvironments()
    },
    { successMsg: editingEnv.id ? '环境已更新' : '环境已创建', errorPrefix: '保存失败' },
  )
}

async function doDeleteEnv() {
  if (!confirmDeleteEnvId) return
  const deletedId = confirmDeleteEnvId
  await safeInvoke(
    'deleteEnv',
    async () => {
      await api.deleteEnvironment(deletedId)
      confirmDeleteEnvId = null
      if (selectedEnv?.id === deletedId) selectedEnv = null
      await loadEnvironments()
    },
    { successMsg: '环境已删除', errorPrefix: '删除失败' },
  )
}

function openCreateEnv() {
  expandedVersions = {}
  editingEnv = { id: '', name: '', description: '', isBuiltin: false, units: [] }
  preloadVersions()
  envModalOpen = true
}

function openEditEnv(env: any) {
  expandedVersions = {}
  editingEnv = JSON.parse(JSON.stringify(env))
  preloadVersions()
  envModalOpen = true
}

// 组件目录：环境编辑器中可选的全部组件类型
const catalog = [
  { kind: 'nginx', name: 'Nginx', icon: 'icon-nginx' },
  { kind: 'mysql', name: 'MySQL', icon: 'icon-mysql' },
  { kind: 'redis', name: 'Redis', icon: 'icon-redis' },
  { kind: 'memcached', name: 'Memcached', icon: '' },
  { kind: 'php', name: 'PHP', icon: 'icon-php1' },
  { kind: 'node', name: 'Node.js', icon: 'icon-Nodejs' },
  { kind: 'python', name: 'Python', icon: 'icon-python' },
]

// 每类组件的版本缓存: { [kind]: { installed: string[], available: string[], loading: bool } }
// installed 来自 getInstalledVersions，available 来自 getAvailableVersions（可下载）
let versionCache = $state<Record<string, any>>({})

// 版本列表是否展开（false = 只显示一行，通过"更多"展开）
let expandedVersions = $state<Record<string, boolean>>({})

function sortVersions(list: any[]) {
  return [...list].sort((a, b) => b.localeCompare(a, 'en', { numeric: true }))
}

// 合并某类组件的全部版本：已安装优先（带 ✓ 标识），其后为可下载版本
function allVersions(kind: string) {
  const v = versionCache[kind] || { installed: [], available: [] }
  const installed = v.installed || []
  const avail = (v.available || []).filter((x: string) => !installed.includes(x))
  return [...sortVersions(installed), ...sortVersions(avail)]
}

function isInstalled(kind: string, ver: string) {
  return (versionCache[kind]?.installed || []).includes(ver)
}

async function ensureVersions(kind: string) {
  if (versionCache[kind]) return
  versionCache[kind] = { installed: [], available: [], loading: true }
  const [installed, available] = await Promise.all([
    withTimeout(api.getInstalledVersions(kind), 5000, []).catch(() => []),
    withTimeout(api.getAvailableVersions(kind), 8000, []).catch(() => []),
  ])
  versionCache[kind] = {
    installed: Array.isArray(installed) ? installed : [],
    available: Array.isArray(available) ? available.map((r) => r.version) : [],
    loading: false,
  }
  // 已选中但尚未选择版本的类型，自动补上第一个可用版本
  if (editingEnv) {
    let changed = false
    const units = (editingEnv.units || []).map((u: any) => {
      if (u.kind === kind && !u.version) {
        changed = true
        return { ...u, version: allVersions(kind)[0] || '' }
      }
      return u
    })
    if (changed) editingEnv.units = units
  }
}

function preloadVersions() {
  for (const c of catalog) ensureVersions(c.kind)
}

// 切换组件类型：已添加则移除，未加入则加入并默认选中第一个版本
function toggleKind(kind: string) {
  const idx = (editingEnv.units || []).findIndex((u: any) => u.kind === kind)
  if (idx >= 0) {
    const a = [...editingEnv.units]
    a.splice(idx, 1)
    editingEnv.units = a
    return
  }
  ensureVersions(kind)
  const cached = versionCache[kind]
  const def = cached && !cached.loading ? allVersions(kind)[0] || '' : ''
  editingEnv.units = [...(editingEnv.units || []), { kind, version: def, name: '' }]
}

function selectVersion(kind: string, ver: string) {
  editingEnv.units = (editingEnv.units || []).map((u: any) => (u.kind === kind ? { ...u, version: ver } : u))
}

// 展开/收起某类组件的版本列表
function toggleMore(kind: string) {
  expandedVersions = { ...expandedVersions, [kind]: !expandedVersions[kind] }
}

const unsub = refreshSignal.subscribe(() => load())
onMount(() => {
  load()
  return unsub
})
</script>

<div class="flex flex-col gap-5">
  <!-- 当前环境概览 -->
  <div class="relative overflow-hidden rounded-2xl border border-base-300 bg-base-100">
    <div class="pointer-events-none absolute inset-0 bg-gradient-to-r from-primary/[0.06] via-transparent to-success/[0.06]"></div>
    <div class="relative flex flex-wrap items-center gap-4 px-5 py-4">
      <div class="relative flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl {activeEnvID || hasRunningServices ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/35'}">
        {#if activeEnvID || hasRunningServices}
          <span class="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-base-100 bg-success"></span>
        {/if}
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5"/><path d="m3 17 9 5 9-5"/></svg>
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-[11px] font-medium uppercase tracking-[0.14em] text-base-content/40">当前环境</div>
        {#if envLoading}
          <div class="mt-1 flex items-center gap-2 text-sm text-base-content/50">
            <span class="loading loading-dots loading-xs text-primary"></span>正在读取环境状态
          </div>
        {:else if activeEnvID || hasRunningServices}
          <div class="mt-0.5 flex flex-wrap items-center gap-2">
            <span class="truncate text-lg font-semibold">{activeEnv?.name || (activeEnvID ? '自定义环境' : '基于项目推导')}</span>
            {#if hasRunningServices}
              <span class="inline-flex items-center gap-1.5 rounded-full bg-success/10 px-2 py-0.5 text-[11px] font-medium text-success">
                <span class="h-1.5 w-1.5 rounded-full bg-success"></span>
                {runningCount} 个组件运行中
              </span>
            {:else}
              <span class="rounded-full bg-base-200 px-2 py-0.5 text-[11px] text-base-content/50">已停止</span>
            {/if}
          </div>
          <p class="mt-0.5 text-xs text-base-content/45">激活的环境决定面板与状态栏展示的组件集合</p>
        {:else}
          <div class="mt-0.5 text-lg font-semibold text-base-content/45">未激活环境</div>
          <p class="mt-0.5 text-xs text-base-content/45">从下方选择一套模板，点击「激活并运行」即可启动整套环境</p>
        {/if}
      </div>
      <div class="flex items-center gap-2">
        {#if activeEnvID || hasRunningServices}
          <button class="btn btn-sm btn-outline gap-1.5" onclick={deactivateCurrent} disabled={$operation.active || busy}>
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
            停止环境
          </button>
        {/if}
      </div>
    </div>
  </div>

  <!-- 加载失败横幅：取数失败 ≠ 没有环境 -->
  {#if loadErrors.length > 0}
    <div class="alert alert-warning alert-sm flex items-center gap-3 py-2.5">
      <span class="text-sm">数据加载失败：{loadErrors.join('；')}</span>
      <button class="btn btn-sm btn-ghost ml-auto shrink-0" onclick={() => load()} disabled={envLoading || envsLoading}>
        {envLoading || envsLoading ? '重试中...' : '重试'}
      </button>
    </div>
  {/if}

  <!-- 模板工具栏 -->
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h2 class="text-base font-semibold">环境模板</h2>
      <p class="mt-0.5 text-xs text-base-content/45">组合软件版本并一键激活，多个项目可共享同一套环境</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <span class="inline-flex items-center gap-1.5 rounded-lg border border-base-300 bg-base-100 px-2.5 py-1 text-[11px] text-base-content/55">
        {environments.length} 个模板
      </span>
      <span class="inline-flex items-center gap-1.5 rounded-lg border border-base-300 bg-base-100 px-2.5 py-1 text-[11px] text-base-content/55">
        {totalComponents} 个组件
      </span>
      {#if installStatusReady && totalMissing > 0}
        <span class="inline-flex items-center gap-1.5 rounded-lg border border-warning/30 bg-warning/10 px-2.5 py-1 text-[11px] font-medium text-warning">待安装 {totalMissing} 项</span>
      {:else if installStatusReady && environments.length > 0}
        <span class="inline-flex items-center gap-1.5 rounded-lg border border-success/30 bg-success/10 px-2.5 py-1 text-[11px] font-medium text-success">全部就绪</span>
      {/if}
      <button class="btn btn-primary btn-sm gap-1.5" onclick={openCreateEnv} disabled={busy}>
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        新建环境
      </button>
    </div>
  </div>

  {#if envsLoading}
    <div class="grid grid-cols-[repeat(auto-fill,minmax(340px,1fr))] gap-4">
      {#each [1, 2, 3] as _}
        <div class="rounded-2xl border border-base-300 bg-base-100 p-5">
          <div class="flex items-start gap-3">
            <div class="h-10 w-10 animate-pulse rounded-xl bg-base-200"></div>
            <div class="flex-1 space-y-2">
              <div class="h-3 w-2/5 animate-pulse rounded bg-base-200"></div>
              <div class="h-2 w-4/5 animate-pulse rounded bg-base-200"></div>
            </div>
          </div>
          <div class="mt-6 flex gap-2">
            <div class="h-6 w-24 animate-pulse rounded-lg bg-base-200"></div>
            <div class="h-6 w-20 animate-pulse rounded-lg bg-base-200"></div>
            <div class="h-6 w-28 animate-pulse rounded-lg bg-base-200"></div>
          </div>
          <div class="mt-8 h-1 w-full animate-pulse rounded bg-base-200"></div>
        </div>
      {/each}
    </div>
  {:else if environments.length === 0 && !envsError}
    <div class="flex flex-col items-center justify-center rounded-2xl border border-dashed border-base-300 bg-base-100/60 px-6 py-16 text-center">
      <div class="flex h-14 w-14 items-center justify-center rounded-2xl bg-base-200 text-base-content/30">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-7 w-7" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5"/><path d="m3 17 9 5 9-5"/></svg>
      </div>
      <p class="mt-4 text-sm font-semibold">还没有环境模板</p>
      <p class="mt-1 max-w-sm text-xs leading-relaxed text-base-content/45">把常用的软件版本组合保存为模板，之后即可一键切换整套开发环境</p>
      <button class="btn btn-primary btn-sm mt-5 gap-1.5" onclick={openCreateEnv} disabled={busy}>
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        新建第一个环境
      </button>
    </div>
  {:else}
    <div class="grid grid-cols-[repeat(auto-fill,minmax(340px,1fr))] gap-4">
      {#each environments as env (env.id)}
        {@const isActive = activeEnvID === env.id}
        {@const installCheck = checkEnvInstalled(env)}
        {@const envUnits = unitsOf(env)}
        <article class="group relative flex flex-col overflow-hidden rounded-2xl border bg-base-100 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg hover:shadow-base-content/5 {isActive ? 'border-primary/40 shadow-md shadow-primary/5' : 'border-base-300 hover:border-primary/30'}">
          <div class="h-1 w-full {isActive ? 'bg-gradient-to-r from-primary via-primary to-success' : 'bg-transparent'}"></div>

          <!-- 卡片头部 -->
          <div class="flex items-start gap-3 px-5 pb-3 pt-3.5">
            <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl {isActive ? 'bg-primary text-primary-content' : env.isBuiltin ? 'bg-primary/10 text-primary' : 'bg-base-200 text-base-content/45'}">
              {#if env.isBuiltin}
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 2.7 5.5 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.8 1-6.1-4.4-4.3 6.1-.9L12 3Z"/></svg>
              {:else}
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5"/><path d="m3 17 9 5 9-5"/></svg>
              {/if}
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5">
                <h3 class="min-w-0 truncate text-[15px] font-semibold" title={env.name}>{env.name}</h3>
                {#if isActive}
                  <span class="shrink-0 rounded-full bg-success/10 px-2 py-0.5 text-[10px] font-medium text-success">使用中</span>
                {:else if env.isBuiltin}
                  <span class="shrink-0 rounded-full bg-base-200 px-2 py-0.5 text-[10px] text-base-content/45">预设</span>
                {/if}
              </div>
              <p class="mt-1 line-clamp-2 text-xs leading-relaxed text-base-content/50">{env.description || '自定义环境模板'}</p>
            </div>
            <div class="flex shrink-0 items-center gap-0.5 opacity-50 transition-opacity group-hover:opacity-100">
              <button class="flex h-7 w-7 items-center justify-center rounded-lg text-base-content/45 transition-colors hover:bg-base-200 hover:text-base-content disabled:opacity-40" onclick={() => openEditEnv(env)} disabled={$operation.active || busy} title="编辑环境">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M11 5H6a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-5"/><path d="m18.4 2.6 3 3L11 16H8v-3L18.4 2.6Z"/></svg>
              </button>
              <button class="flex h-7 w-7 items-center justify-center rounded-lg text-base-content/45 transition-colors hover:bg-error/10 hover:text-error disabled:opacity-40" onclick={() => confirmDeleteEnvId = env.id} disabled={$operation.active || busy} title="删除环境">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M19 7l-.867 12.142A2 2 0 0 1 16.138 21H7.862a2 2 0 0 1-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v3M4 7h16"/></svg>
              </button>
            </div>
          </div>

          <!-- 组件清单 -->
          <div class="flex-1 px-5 pb-4">
            <div class="mb-2 flex items-center justify-between">
              <span class="text-[10px] font-medium uppercase tracking-wider text-base-content/35">组件清单</span>
              <span class="font-mono text-[10px] text-base-content/35">{envUnits.length}</span>
            </div>
            {#if envUnits.length === 0}
              <div class="rounded-xl border border-dashed border-base-300 px-3 py-5 text-center text-xs text-base-content/35">空环境 · 点击编辑添加组件</div>
            {:else}
              <div class="flex flex-wrap gap-1.5">
                {#each envUnits as u (u.kind)}
                  {@const meta = componentMeta[u.kind]}
                  {@const unitInstalled = installStatus[u.kind + ' ' + u.version]}
                  <span class="inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 {installStatusReady && !unitInstalled ? 'border-warning/30 bg-warning/5' : 'border-base-300/70 bg-base-200/50'}">
                    {#if meta?.icon}
                      <span class="iconfont text-[15px] leading-none" style="color:{meta.color}"></span>
                    {:else}
                      <span class="flex h-4 w-4 items-center justify-center rounded-full text-[9px] font-bold text-white" style="background:{meta?.color || '#94a3b8'}">{(meta?.name || u.kind || '?').charAt(0)}</span>
                    {/if}
                    <span class="text-[11px] font-medium text-base-content/75">{meta?.name || unitNames[u.kind] || u.kind}</span>
                    <span class="font-mono text-[10px] text-base-content/40">{u.version}</span>
                    {#if installStatusReady}
                      {#if unitInstalled}
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-success" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m5 13 4 4L19 7"/></svg>
                      {:else}
                        <span class="h-1.5 w-1.5 rounded-full bg-warning"></span>
                      {/if}
                    {/if}
                  </span>
                {/each}
              </div>
            {/if}
          </div>

          <!-- 底部：安装就绪度 + 操作 -->
          <div class="mt-auto flex flex-wrap items-center gap-2 border-t border-base-300 bg-base-200/30 px-5 py-3">
            <div class="flex min-w-0 items-center gap-2">
              {#if !installStatusReady}
                <span class="loading loading-spinner loading-xs text-primary"></span>
                <span class="text-xs text-base-content/40">检查组件</span>
              {:else if envUnits.length === 0}
                <span class="text-xs text-base-content/40">尚未配置组件</span>
              {:else if installCheck.allInstalled}
                <span class="flex h-5 w-5 items-center justify-center rounded-full bg-success/10 text-success">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m5 13 4 4L19 7"/></svg>
                </span>
                <span class="text-xs font-medium text-success">全部就绪</span>
                <span class="font-mono text-[10px] text-base-content/35">{installCheck.installedCount}/{installCheck.total}</span>
              {:else}
                <span class="flex h-5 w-5 items-center justify-center rounded-full bg-warning/10 text-warning">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v4m0 4h.01M10.3 4.3 2.6 18a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"/></svg>
                </span>
                <div class="min-w-0">
                  <div class="text-xs font-medium text-warning">缺 {installCheck.missing.length} 项</div>
                  <div class="truncate text-[10px] text-base-content/35">{installCheck.missing.slice(0, 2).join(' · ')}{installCheck.missing.length > 2 ? ' …' : ''}</div>
                </div>
              {/if}
            </div>
            <div class="ml-auto flex flex-wrap items-center justify-end gap-1.5">
              {#if !installStatusReady}
                <button class="btn btn-sm btn-disabled" disabled>检查中…</button>
              {:else if isActive}
                {#if !installCheck.allInstalled}
                  {#if hasRunningServices}
                    <button class="btn btn-xs btn-outline" onclick={deactivateCurrent} disabled={$operation.active || busy}>停止环境</button>
                    <button class="btn btn-xs btn-warning" disabled title="组件正在运行，请先停止环境再安装">安装</button>
                  {:else}
                    <button class="btn btn-sm btn-warning" onclick={() => installEnvComponents(env)} disabled={$operation.active || busy}>安装缺失组件</button>
                  {/if}
                {:else}
                  <span class="text-[11px] text-base-content/40">已激活</span>
                {/if}
              {:else if installCheck.allInstalled}
                <button class="btn btn-xs btn-ghost" onclick={() => activateEnvOnly(env)} disabled={$operation.active || busy} title="仅切换当前环境标记，不启动组件">仅激活</button>
                <button class="btn btn-sm btn-primary" onclick={() => activateEnv(env)} disabled={$operation.active || busy} title="切换标记并启动全部组件">激活并运行</button>
              {:else}
                <button class="btn btn-sm btn-warning" onclick={() => installEnvComponents(env)} disabled={$operation.active || busy}>安装缺失组件</button>
              {/if}
            </div>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<!-- 安装进度 Modal -->
<dialog class="modal" class:modal-open={installModal}>
  <div class="modal-box max-w-[520px]">
    <h3 class="font-bold text-lg mb-1">安装环境组件</h3>
    <p class="text-xs text-base-content/60 mb-4">{installingEnv?.name || ''}</p>

    {#if installTasks.length === 0}
      <div class="text-sm text-base-content/70 py-4">无需安装</div>
    {:else}
      <div class="flex flex-col gap-2.5 max-h-[360px] overflow-y-auto pr-1">
        {#each installTasks as task}
          <div class="flex items-center gap-3 px-3 py-2.5 bg-base-200 rounded-lg">
            <div class="w-5 h-5 shrink-0 flex items-center justify-center">
              {#if task.status === 'done'}
                <span class="text-success text-sm">✓</span>
              {:else if task.status === 'error'}
                <span class="text-error text-sm">✕</span>
              {:else if task.status === 'downloading'}
                <span class="loading loading-spinner loading-xs text-primary"></span>
              {:else}
                <span class="w-2 h-2 rounded-full bg-base-content/30"></span>
              {/if}
            </div>
            <div class="flex-1 min-w-0">
              <div class="text-sm font-medium">{task.name}</div>
              <div class="text-xs font-mono text-base-content/50">{task.version}</div>
            </div>
            <div class="text-right shrink-0 min-w-[60px]">
              {#if task.status === 'downloading'}
                <span class="text-xs font-mono text-primary">{task.phaseLabel || '下载中'} {task.progress}%</span>
              {:else if task.status === 'done'}
                <span class="text-xs text-success">已完成</span>
              {:else if task.status === 'error'}
                <span class="text-xs text-error">失败</span>
              {:else}
                <span class="text-xs text-base-content/40">等待中</span>
              {/if}
            </div>
          </div>
          {#if task.status === 'downloading'}
            <progress class="progress progress-primary w-full h-1 -mt-1.5" value={task.progress} max="100"></progress>
          {/if}
        {/each}
      </div>
    {/if}

    <div class="modal-action">
      {#if installTasks.every(t => t.status === 'done' || t.status === 'cancelled')}
        <button class="btn btn-primary btn-sm" onclick={closeInstallModal}>完成</button>
      {:else if installTasks.some(t => t.status === 'error')}
        <button class="btn btn-ghost btn-sm" onclick={closeInstallModal}>关闭</button>
      {:else}
        <button class="btn btn-ghost btn-sm" onclick={cancelInstall}>取消</button>
      {/if}
    </div>
  </div>
  <!-- 安装中禁止点击外部关闭 -->
  {#if installTasks.every(t => t.status === 'done' || t.status === 'cancelled') || installTasks.some(t => t.status === 'error')}
    <form method="dialog" class="modal-backdrop"><button onclick={closeInstallModal}>关闭</button></form>
  {:else}
    <div class="modal-backdrop"></div>
  {/if}
</dialog>

<!-- Environment Edit Modal -->
<dialog class="modal" class:modal-open={envModalOpen && !!editingEnv}>
  <div class="modal-box max-w-[620px]">
    <h3 class="font-bold text-lg mb-3">{editingEnv?.id ? '编辑环境' : '新建环境'}</h3>
    {#if editingEnv}
      <div class="grid grid-cols-2 gap-3 mb-3.5">
        <label class="form-control col-span-2"><span class="label-text text-xs text-base-content/70 mb-1">名称 *</span><input class="input input-bordered input-sm" type="text" bind:value={editingEnv.name} placeholder="如：PHP7.4 + MySQL5.7" /></label>
        <label class="form-control col-span-2"><span class="label-text text-xs text-base-content/70 mb-1">描述</span><input class="input input-bordered input-sm" type="text" bind:value={editingEnv.description} placeholder="可选，简要说明用途" /></label>
      </div>

      <div class="mb-3.5 p-3 bg-base-200 rounded-lg">
        <div class="flex justify-between items-center mb-2.5 text-sm font-medium">
          <span>组件</span>
          <span class="text-xs font-normal text-base-content/50">点击卡片选择组件，点击版本或「更多」切换版本</span>
        </div>

        <!-- 组件类型卡片网格 -->
        <div class="grid grid-cols-4 gap-2">
          {#each catalog as c (c.kind)}
            {@const selected = (editingEnv.units || []).some((u: any) => u.kind === c.kind)}
            <button
              type="button"
              class="flex items-center gap-1.5 rounded-xl border px-2.5 py-2 transition-all select-none
                {selected ? 'border-primary bg-primary/10 shadow-sm' : 'border-base-300 bg-base-100 hover:border-primary/60'}"
              onclick={() => toggleKind(c.kind)}>
              {#if c.icon}
                <span class="iconfont text-xl leading-none {c.icon}"></span>
              {:else}
                <span class="w-6 h-6 shrink-0 rounded-full bg-base-content/20 flex items-center justify-center text-xs font-bold text-base-content/60">{c.name.charAt(0)}</span>
              {/if}
              <span class="text-xs font-medium text-base-content/80 leading-none truncate">{c.name}</span>
              {#if selected}
                <span class="ml-auto w-4 h-4 shrink-0 rounded-full bg-primary text-primary-content flex items-center justify-center text-[10px] leading-none">✓</span>
              {/if}
            </button>
          {/each}
        </div>

        <!-- 已选组件的版本选择 -->
        {#if (editingEnv.units || []).length > 0}
          <div class="mt-3 flex flex-col gap-2">
            {#each editingEnv.units as spec}
              {@const c = catalog.find((x) => x.kind === spec.kind)}
              {@const vcache = versionCache[spec.kind]}
              {@const versions = allVersions(spec.kind)}
              {@const expanded = !!expandedVersions[spec.kind]}
              <div class="flex items-start gap-2.5 rounded-lg bg-base-100 border border-base-300 px-3 py-2.5">
                <div class="flex items-center gap-1.5 w-28 shrink-0 pt-0.5">
                  {#if c?.icon}
                    <span class="iconfont text-lg leading-none {c.icon}"></span>
                  {:else}
                    <span class="w-4 h-4 rounded-full bg-base-content/20 flex items-center justify-center text-[9px] font-bold text-base-content/50">{(c?.name || spec.kind || '?').charAt(0)}</span>
                  {/if}
                  <span class="text-xs font-semibold">{c?.name || spec.kind}</span>
                </div>
                <div class="flex-1 min-w-0">
                  {#if vcache?.loading}
                    <div class="flex items-center gap-1.5 text-xs text-base-content/50">
                      <span class="loading loading-dots loading-xs text-primary"></span> 加载版本...
                    </div>
                  {:else if versions.length === 0}
                    <div class="text-xs text-warning">暂无可用版本，可先在「软件仓库」页下载对应组件</div>
                  {:else}
                    <div class="flex items-center gap-1.5">
                      <div class="flex-1 min-w-0 flex {expanded ? 'flex-wrap' : 'flex-nowrap overflow-hidden'} gap-1.5">
                        {#if spec.version && !versions.includes(spec.version)}
                          <button
                            type="button"
                            class="shrink-0 px-2 py-1 rounded-md text-xs font-mono border border-primary bg-primary text-primary-content"
                            onclick={() => selectVersion(spec.kind, spec.version)}>
                            {spec.version}（自定义）
                          </button>
                        {/if}
                        {#each versions as ver (ver)}
                          <button
                            type="button"
                            class="shrink-0 px-2 py-1 rounded-md text-xs font-mono border transition-colors
                              {spec.version === ver
                                ? 'border-primary bg-primary text-primary-content'
                                : 'border-base-300 bg-base-100 text-base-content/70 hover:border-primary/60'}"
                            onclick={() => selectVersion(spec.kind, ver)}>
                            {ver}
                            {#if isInstalled(spec.kind, ver)}
                              <span class="{spec.version === ver ? 'text-primary-content' : 'text-success'}">✓</span>
                            {/if}
                          </button>
                        {/each}
                      </div>
                      <button
                        type="button"
                        class="shrink-0 px-2 py-1 rounded-md text-xs border border-base-300 bg-base-100 text-base-content/60 hover:border-primary/60"
                        onclick={() => toggleMore(spec.kind)}>{expanded ? '收起' : '更多'}</button>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {:else}
          <div class="text-xs text-base-content/50 py-1">从上方卡片选择组件类型开始搭建环境</div>
        {/if}
      </div>
    {/if}
    <div class="modal-action">
      <button class="btn btn-ghost" onclick={() => { envModalOpen = false; editingEnv = null }} disabled={busy}>取消</button>
      <button class="btn btn-primary" onclick={saveEnv} disabled={busy}>{busy ? '保存中...' : '保存'}</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={() => { envModalOpen = false; editingEnv = null }}>关闭</button></form>
</dialog>

<!-- Delete Environment Confirm -->
<dialog class="modal" class:modal-open={!!confirmDeleteEnvId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认删除环境</h3>
    <p class="py-2 text-sm text-base-content/70">删除后无法恢复，正在使用该环境的项目将回退到独立配置。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" onclick={() => confirmDeleteEnvId = null}>取消</button>
      <button class="btn btn-error" onclick={doDeleteEnv} disabled={busy}>确认删除</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={() => confirmDeleteEnvId = null}>关闭</button></form>
</dialog>
