<script lang="ts">
import { onMount } from 'svelte'
import { stopPropagation } from 'svelte/legacy'
import { api } from '../lib/api'
import { useDownloadProgress } from '../lib/events'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast, operation, refreshSignal, runOperation } from '../lib/stores'

let env = $state<any>({ services: {}, projects: {}, runtimes: {}, activeProfileID: '' })
let envLoading = $state(true)
let environments = $state<any[]>([])
let envsLoading = $state(true)
let selectedEnv: any = null
let editingEnv = $state<any>(null)
let envModalOpen = $state(false)
let snapshots = $state<any[]>([])
let snapshotDetails = $state<Record<string, any>>({})
let snapLoading = $state(true)
let busy = $state(false)
let confirmRollbackId: any = null
let confirmDeleteId = $state<any>(null)
let confirmDeleteEnvId = $state<any>(null)
let selectedSnapshotId = $state<any>(null)
let activeTab = $state('environments')
// 基于后端 getInstalledVersions 构建的安装状态映射，格式: { 'nginx 1.24.0': true }
let installStatus: Record<string, boolean> = {}
let installStatusReady = $state(false)

// 安装进度相关
let installModal = $state(false)
let installingEnv = $state<any>(null)
let installTasks = $state<any[]>([]) // [{ id, version, name, status: 'pending'|'downloading'|'extracting'|'done'|'error', progress: 0 }]
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

let sortedSnapshots = $derived(Array.isArray(snapshots) ? [...snapshots].reverse() : [])

// 当前激活的环境 ID
let activeEnvID = $derived(env.activeProfileID || '')
let hasRunningServices = $derived(
  Object.values(env.services || {}).some((s) => s.state === 'running') ||
    Object.values(env.runtimes || {}).some((r) => r.state === 'running'),
)

async function withTimeout(promise: Promise<any>, timeoutMs = 5000, fallback: any = null) {
  return Promise.race([promise, new Promise((resolve) => setTimeout(() => resolve(fallback), timeoutMs))])
}

async function loadEnvironment() {
  envLoading = true
  try {
    const result = await withTimeout(api.getEnvironment(), 5000, {
      services: {},
      projects: {},
      runtimes: {},
      activeProfileID: '',
    })
    env = result || { services: {}, projects: {}, runtimes: {}, activeProfileID: '' }
  } catch (e) {
    console.error('[Environment] load error:', e)
  } finally {
    envLoading = false
  }
}

async function loadEnvironments() {
  envsLoading = true
  try {
    const result = await withTimeout(api.listEnvironments(), 5000, [])
    environments = (Array.isArray(result) ? result : []).sort((a, b) => {
      // 预设环境排前面，然后按名称字母排序
      if (a.isBuiltin && !b.isBuiltin) return -1
      if (!a.isBuiltin && b.isBuiltin) return 1
      return (a.name || '').localeCompare(b.name || 'zh-CN')
    })
  } catch (e) {
    environments = []
  } finally {
    envsLoading = false
  }
}

async function loadSnapshots() {
  snapLoading = true
  try {
    const result = await withTimeout(api.getSnapshots(), 5000, [])
    snapshots = Array.isArray(result) ? result : []
  } catch (e) {
    console.error('[Environment] snapshots error:', e)
    snapshots = []
  } finally {
    snapLoading = false
  }
}

async function load() {
  await Promise.all([loadEnvironment(), loadEnvironments(), loadSnapshots()])
  await refreshInstallStatus()
}

// 使用后端 installStatus 检查组件安装状态
function checkEnvInstalled(env: any) {
  const units = env.units ? (Array.isArray(env.units) ? env.units : Object.values(env.units)) : []

  const allInstalled = []
  const missing = []

  for (const u of units) {
    const key = `${u.kind} ${u.version}`
    if (installStatus[key]) {
      allInstalled.push(key)
    } else {
      missing.push(key)
    }
  }

  return {
    allInstalled: missing.length === 0,
    missing,
    total: allInstalled.length + missing.length,
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
  // 立即反馈：未完成任务标记为已取消并关闭弹窗，不必等待正在进行的下载结束
  installTasks = installTasks.map((t) => (t.status === 'done' ? t : { ...t, status: 'cancelled', progress: 0 }))
  installModal = false
  addToast('已取消安装', 'warning', 3500)
}

const unitNames: Record<string, string> = {
  nginx: 'Nginx',
  mysql: 'MySQL',
  redis: 'Redis',
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

async function doDeleteSnapshot() {
  if (!confirmDeleteId) return
  if (busy) return
  busy = true
  const id = confirmDeleteId
  try {
    await withTimeout(api.deleteSnapshot(id), 10000)
    delete snapshotDetails[id]
    if (selectedSnapshotId === id) selectedSnapshotId = null
    confirmDeleteId = null
    snapshots = snapshots.filter((s) => s !== id)
    addToast('快照已删除', 'success', 3500)
  } catch (e: any) {
    addToast(`删除失败: ${e.message || e}`, 'error', 3500)
  } finally {
    busy = false
  }
}

function formatTime(snapshotId: string) {
  if (!snapshotId?.startsWith('snapshot-')) return snapshotId
  const ts = snapshotId.replace('snapshot-', '')
  if (ts.length !== 13) return snapshotId
  return `${ts.slice(0, 4)}-${ts.slice(4, 6)}-${ts.slice(6, 8)} ${ts.slice(9, 11)}:${ts.slice(11, 13)}`
}

function stateLabel(state: string) {
  const labels: Record<string, string> = {
    running: '运行中',
    stopped: '已停止',
    starting: '启动中',
    stopping: '停止中',
  }
  return labels[state] || state || '未知'
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

function loadSnapshotDetail(id: string) {
  if (selectedSnapshotId === id) {
    selectedSnapshotId = null
    return
  }
  if (snapshotDetails[id]) {
    selectedSnapshotId = id
    return
  }
  withTimeout(api.getSnapshotDetail(id), 8000)
    .then((detail) => {
      if (detail) {
        snapshotDetails = { ...snapshotDetails, [id]: detail }
        selectedSnapshotId = id
      }
    })
    .catch(() => addToast('加载快照详情失败', 'error', 3500))
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
  const avail = (v.available || []).filter((x) => !installed.includes(x))
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
    const units = (editingEnv.units || []).map((u) => {
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
  const idx = (editingEnv.units || []).findIndex((u) => u.kind === kind)
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
  editingEnv.units = (editingEnv.units || []).map((u) => (u.kind === kind ? { ...u, version: ver } : u))
}

// 展开/收起某类组件的版本列表
function toggleMore(kind) {
  expandedVersions = { ...expandedVersions, [kind]: !expandedVersions[kind] }
}

// 各软件对应的 iconfont 类名
const iconMap = {
  nginx: 'icon-nginx',
  mysql: 'icon-mysql',
  redis: 'icon-redis',
  php: 'icon-php1',
  node: 'icon-Nodejs',
  python: 'icon-python',
}

let svcEntries = $derived(Object.entries(env.services || {}))
let projEntries = $derived(Object.entries(env.projects || {}))
let rtEntries = $derived(Object.entries(env.runtimes || {}))

const unsub = refreshSignal.subscribe(() => load())
onMount(() => {
  load()
  return unsub
})
</script>

<div class="flex flex-col gap-5">
  <div class="flex justify-between items-center">
    <div>
      <h1 class="text-xl font-bold">环境管理</h1>
      <p class="text-sm text-base-content/60 mt-1">管理环境模板、当前状态和快照存档</p>
    </div>
    <div class="flex gap-2">
      {#if activeTab === 'environments'}
        <button class="btn btn-primary btn-sm" onclick={openCreateEnv} disabled={busy}>新建环境</button>
      {/if}
    </div>
  </div>

  <div role="tablist" class="tabs tabs-bordered">
    <button class="tab" class:tab-active={activeTab === 'environments'} onclick={() => activeTab = 'environments'}>我的环境</button>
    <button class="tab" class:tab-active={activeTab === 'overview'} onclick={() => activeTab = 'overview'}>运行状态</button>
    <button class="tab" class:tab-active={activeTab === 'snapshots'} onclick={() => activeTab = 'snapshots'}>快照存档</button>
  </div>

  {#if activeTab === 'environments'}
    <!-- 当前环境操作栏 -->
    <div class="flex items-center justify-between px-4 py-3 bg-base-100 border border-base-300 rounded-lg">
      <div class="flex items-center gap-3">
        {#if envLoading}
          <span class="text-sm text-base-content/60">加载中...</span>
        {:else if activeEnvID}
          <span class="w-2 h-2 rounded-full bg-success"></span>
          <span class="text-sm">
            当前环境：
            <span class="font-semibold text-primary">
              {environments.find((e) => e.id === activeEnvID)?.name || '自定义环境'}
            </span>
          </span>
        {:else if hasRunningServices}
          <span class="w-2 h-2 rounded-full bg-success"></span>
          <span class="text-sm">当前环境：<span class="font-semibold">基于项目推导</span></span>
        {:else}
          <span class="w-2 h-2 rounded-full bg-base-content/30"></span>
          <span class="text-sm text-base-content/60">未激活环境</span>
        {/if}
      </div>
      <div class="flex gap-2">
        {#if activeEnvID || hasRunningServices}
          <button class="btn btn-sm btn-outline" onclick={deactivateCurrent} disabled={$operation.active || busy}>
            停止环境
          </button>
        {/if}
      </div>
    </div>

    {#if envsLoading}
      <div class="text-center py-16 text-base-content/60">加载环境中...</div>
    {:else if environments.length === 0}
      <div class="flex flex-col items-center justify-center py-16 text-base-content/60 gap-2">
        <p class="text-base font-semibold">暂无环境</p>
        <p class="text-sm">新建一个环境，组合不同版本的软件和运行时</p>
        <p class="text-xs text-base-content/50">多个项目可共享同一个环境</p>
        <div class="mt-4"><button class="btn btn-primary btn-sm" onclick={openCreateEnv} disabled={busy}>新建第一个环境</button></div>
      </div>
    {:else}
      <div class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4">
        {#each environments as env (env.id)}
          {@const isActive = activeEnvID === env.id}
          {@const installCheck = checkEnvInstalled(env)}
          {@const envUnits = env.units ? (Array.isArray(env.units) ? env.units : Object.values(env.units)) : []}
          <div class="card bg-base-100 border border-base-300 overflow-hidden transition-all hover:border-primary hover:-translate-y-0.5 {isActive ? 'border-l-4 border-l-success border-primary shadow-md' : ''} {env.isBuiltin ? 'bg-gradient-to-br from-base-100 to-base-200' : ''}">
            <!-- 卡片头部 -->
            <div class="px-4 pt-4 pb-3">
              <div class="min-w-0">
                <div class="text-[15px] font-semibold flex items-center gap-1.5">
                  {#if env.isBuiltin}<span class="text-xs text-base-content/40 font-normal shrink-0">预设</span>{/if}
                  <span class="truncate">{env.name}</span>
                </div>
                {#if env.description}
                  <div class="text-xs text-base-content/60 mt-1 leading-relaxed">{env.description}</div>
                {/if}
              </div>
            </div>

            <!-- 卡片内容：组件摘要 + 安装状态 -->
            <div class="px-4 pb-3">
              <div class="text-xs bg-base-200 rounded-lg overflow-hidden min-h-[40px]">
                {#if envUnits.length === 0}
                  <div class="px-3 py-2.5"><span class="text-base-content/50">空环境</span></div>
                {:else}
                  <div class="px-3 py-2.5 flex flex-wrap gap-x-4 gap-y-1.5">
                    {#each envUnits as u}
                      <span class="inline-flex items-center gap-1.5">
                        {#if iconMap[u.kind]}
                          <span class="iconfont text-lg {iconMap[u.kind]}"></span>
                        {:else}
                          <span class="w-[18px] h-[18px] rounded-full bg-base-content/20 flex items-center justify-center text-[10px] font-bold text-base-content/50">{u.kind ? u.kind.charAt(0).toUpperCase() : '?'}</span>
                        {/if}
                        <span class="text-base-content/70">{unitNames[u.kind] || u.kind || '?'}</span>
                        <span class="text-base-content/40 font-mono">{u.version}</span>
                      </span>
                    {/each}
                  </div>
                {/if}
                {#if installStatusReady && installCheck.total > 0}
                  <div class="px-3 py-1.5 border-t border-base-300/50 flex items-center gap-1.5">
                    {#if installCheck.allInstalled}
                      <span class="text-success">✓ 全部已安装</span>
                    {:else}
                      <span class="text-warning">⚠ {installCheck.missing.length} 项未安装: {installCheck.missing.slice(0, 3).join('，')}{installCheck.missing.length > 3 ? '...' : ''}</span>
                    {/if}
                  </div>
                {:else if !installStatusReady}
                  <div class="px-3 py-1.5 border-t border-base-300/50 flex items-center gap-1.5">
                    <span class="loading loading-dots loading-xs text-primary"></span>
                    <span class="text-base-content/40">检查中...</span>
                  </div>
                {/if}
              </div>
            </div>

            <!-- 卡片底部：操作按钮 -->
            <div class="px-4 pb-4 pt-1 flex gap-2 items-center flex-wrap">
              {#if !installStatusReady}
                <button class="btn btn-sm btn-disabled" disabled>检查中...</button>
              {:else if isActive}
                <span class="text-xs text-success font-medium px-1">当前在用</span>
              {:else if installCheck.allInstalled}
                <button class="btn btn-sm btn-ghost" onclick={() => activateEnvOnly(env)} disabled={$operation.active || busy} title="仅切换当前环境标记，不启动组件">激活</button>
                <button class="btn btn-sm btn-primary" onclick={() => activateEnv(env)} disabled={$operation.active || busy} title="切换标记并启动全部组件">激活并运行</button>
              {:else}
                <button class="btn btn-sm btn-warning" onclick={() => installEnvComponents(env)} disabled={$operation.active || busy}>安装</button>
                <button class="btn btn-sm btn-ghost" onclick={() => activateEnvOnly(env)} disabled={$operation.active || busy} title="仅切换当前环境标记">激活</button>
              {/if}
              <div class="flex-1"></div>
              <button class="btn btn-sm btn-ghost btn-square" onclick={() => openEditEnv(env)} disabled={$operation.active || busy} title="编辑环境">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
              </button>
              <button class="btn btn-sm btn-ghost btn-square text-base-content/40 hover:text-error" onclick={() => confirmDeleteEnvId = env.id} disabled={$operation.active || busy} title="删除环境">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
              </button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}

  {#if activeTab === 'overview'}
    {#if envLoading}
      <div class="text-center py-16 text-base-content/60">加载环境状态中...</div>
    {:else}
      <div class="grid grid-cols-3 gap-5">
        <!-- Services -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">服务 ({svcEntries.length})</h3>
          {#if svcEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无已安装的服务</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each svcEntries as [id, svc]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm">
                  <span class="font-medium">{id}</span>
                  <span class="font-mono text-base-content/50 text-xs">{svc.version}</span>
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {svc.state === 'running' ? 'bg-success/10 text-success' : svc.state === 'stopped' ? 'bg-base-200 text-base-content/50' : 'bg-warning/10 text-warning'}">{stateLabel(svc.state)}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Projects -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">项目 ({projEntries.length})</h3>
          {#if projEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无已启用的项目</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each projEntries as [id, proj]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm flex-wrap">
                  <span class="font-medium">{proj.name || id}</span>
                  <span class="text-xs px-1.5 py-0.5 rounded bg-primary/10 text-primary">{proj.type}</span>
                  {#if proj.phpVersion}<span class="font-mono text-base-content/50 text-xs">PHP {proj.phpVersion}</span>{/if}
                  {#if proj.mysqlVersion}<span class="font-mono text-base-content/50 text-xs">MySQL {proj.mysqlVersion}</span>{/if}
                  {#if proj.nginxVersion}<span class="font-mono text-base-content/50 text-xs">Nginx {proj.nginxVersion}</span>{/if}
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {proj.enabled ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/50'}">{proj.enabled ? '已启用' : '已禁用'}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Runtimes -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">运行时 ({rtEntries.length})</h3>
          {#if rtEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无运行时</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each rtEntries as [id, rt]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm">
                  <span class="font-medium">{id}</span>
                  <span class="font-mono text-base-content/50 text-xs">{rt.version}</span>
                  {#if rt.active}<span class="text-xs px-1.5 py-0.5 rounded bg-warning/10 text-warning">默认</span>{/if}
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {rt.state === 'running' ? 'bg-success/10 text-success' : rt.state === 'stopped' ? 'bg-base-200 text-base-content/50' : 'bg-warning/10 text-warning'}">{stateLabel(rt.state)}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}

  {#if activeTab === 'snapshots'}
    {#if snapLoading}
      <div class="text-center py-16 text-base-content/60">加载快照中...</div>
    {:else if snapshots.length === 0}
      <div class="flex flex-col items-center justify-center py-16 text-base-content/60 gap-2">
        <p class="text-base font-semibold">暂无快照</p>
        <p class="text-sm">快照保存当前所有服务的运行状态和配置</p>
        <p class="text-xs text-base-content/50">创建快照功能暂未迁移</p>
      </div>
    {:else}
      <div class="flex flex-col gap-2.5">
        {#each sortedSnapshots as snap}
          <div class="card bg-base-100 border border-base-300" class:border-primary={selectedSnapshotId === snap}>
            <div class="card-body p-3.5">
              <div class="flex items-center justify-between cursor-pointer" role="button" tabindex="0" onclick={() => loadSnapshotDetail(snap)} onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); loadSnapshotDetail(snap) } }}>
                <div class="flex-1">
                  <div class="font-mono text-sm">{snap}</div>
                  <div class="text-xs text-base-content/60 mt-0.5">{formatTime(snap)}</div>
                  {#if snapshotDetails[snap]}
                    {@const detail = snapshotDetails[snap]}
                    <div class="flex gap-1.5 mt-1.5 flex-wrap">
                      {#if detail.services}<span class="badge badge-sm badge-ghost">{Object.keys(detail.services).length} 个服务</span>{/if}
                      {#if detail.projects}<span class="badge badge-sm badge-ghost">{Object.keys(detail.projects).length} 个项目</span>{/if}
                      {#if detail.runtimes}<span class="badge badge-sm badge-ghost">{Object.keys(detail.runtimes).length} 个运行时</span>{/if}
                    </div>
                  {/if}
                </div>
                <div class="flex gap-2 shrink-0">
                  <button class="btn btn-sm btn-ghost text-error" onclick={stopPropagation(() => confirmDeleteId = snap)} disabled={busy}>删除</button>
                </div>
              </div>

              {#if selectedSnapshotId === snap && snapshotDetails[snap]}
                {@const detail = snapshotDetails[snap]}
                <div class="mt-3 pt-3 border-t border-base-300 space-y-3.5">
                  {#if detail.services && Object.keys(detail.services).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">服务状态</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.services) as [id, svc]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{id}</span>
                            <span class="font-mono text-base-content/50">{svc.version}</span>
                            <span class="ml-auto px-1.5 py-0.5 rounded-full {svc.state === 'running' ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/50'}">{svc.state === 'running' ? '运行中' : '已停止'}</span>
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                  {#if detail.projects && Object.keys(detail.projects).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">项目环境</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.projects) as [id, proj]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{proj.name || id}</span>
                            <span class="text-base-content/50">{proj.type}</span>
                            {#if proj.phpVersion}<span class="font-mono text-base-content/50">PHP {proj.phpVersion}</span>{/if}
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                  {#if detail.runtimes && Object.keys(detail.runtimes).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">运行时</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.runtimes) as [id, rt]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{id}</span>
                            <span class="font-mono text-base-content/50">{rt.version}</span>
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
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
                <span class="text-xs font-mono text-primary">{task.progress}%</span>
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
            {@const selected = (editingEnv.units || []).some((u) => u.kind === c.kind)}
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

<!-- Delete Snapshot Confirm -->
<dialog class="modal" class:modal-open={!!confirmDeleteId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认删除</h3>
    <p class="py-2 text-sm text-base-content/70">确定要删除快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmDeleteId}</code> 吗？</p>
    <p class="text-xs text-warning">删除后无法恢复。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" onclick={() => confirmDeleteId = null}>取消</button>
      <button class="btn btn-error" onclick={doDeleteSnapshot} disabled={busy}>确认删除</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={() => confirmDeleteId = null}>关闭</button></form>
</dialog>
