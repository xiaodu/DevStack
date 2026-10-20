<script lang="ts">
import { onMount } from 'svelte'
import Dashboard from './components/Dashboard.svelte'
import Environment from './components/Environment.svelte'
import Logs from './components/Logs.svelte'
import Packages from './components/Packages.svelte'
import Settings from './components/Settings.svelte'
import SetupWizard from './components/SetupWizard.svelte'
import Sidebar from './components/Sidebar.svelte'
import Sites from './components/Sites.svelte'
import Snapshots from './components/Snapshots.svelte'
import Software from './components/Software.svelte'
import { api } from './lib/api'
import { subscribeOperationEvents, subscribeReactiveEvents } from './lib/events'
import GlobalLoading from './lib/GlobalLoading.svelte'
import GlobalOperation from './lib/GlobalOperation.svelte'
import GlobalToast from './lib/GlobalToast.svelte'
import { safeInvoke } from './lib/safeInvoke'
import { addToast, nanoBusy, nanoOperation, operation, pushToast, refreshSignal, runOperation, syncInfo } from './lib/stores'
import { applyTheme } from './lib/themes'

let currentView = $state('dashboard')
let showWizard = $state(false)

// 状态栏数据：当前环境状态 + 环境模板列表（用于 ID→name 映射）
let envState = $state<any>(null)
let envProfiles = $state<any[]>([])

async function loadEnvBar() {
  const [env, profiles] = await Promise.all([
    api.getEnvironment().catch(() => null),
    api.listEnvironments().catch(() => [] as any[]),
  ])
  envState = env
  envProfiles = (profiles as any[]) || []
}

// 当前激活环境名（ID→name 映射，映射不到回退 ID）
let activeEnvID = $derived(envState?.activeProfileID || '')
let activeEnvName = $derived.by(() => {
  const id = activeEnvID
  if (!id) return '未激活'
  const p = envProfiles.find((e: any) => e.id === id)
  return p?.name || id
})

// 运行中的组件名称列表（services + runtimes），用于状态栏展示
let runningServices = $derived.by(() => {
  if (!envState) return [] as string[]
  const labels: Record<string, string> = {
    nginx: 'Nginx',
    mysql: 'MySQL',
    redis: 'Redis',
    php: 'PHP',
    node: 'Node',
    python: 'Python',
  }
  const entries = [...Object.entries(envState.services || {}), ...Object.entries(envState.runtimes || {})]
  return entries
    .filter(([, s]: [string, any]) => s?.state === 'running')
    .map(([id]) => {
      const at = id.indexOf('@')
      const kind = at > 0 ? id.slice(0, at) : id
      const version = at > 0 ? id.slice(at + 1) : ''
      const label = labels[kind] || kind
      return version ? `${label} ${version}` : label
    })
})

// Nginx 是否运行中（重启按钮禁用依据）
let nginxRunning = $derived.by(() => {
  if (!envState?.services) return false
  const entry = Object.entries(envState.services).find(([id]) => id.startsWith('nginx@'))
  return entry?.[1]?.state === 'running'
})

// 是否有任意组件在运行（停止按钮禁用依据，与点击后的提示判定保持一致）
let anyRunning = $derived.by(() => {
  if (!envState) return false
  const running = (m: any) => Object.values(m || {}).some((s: any) => s?.state === 'running')
  return running(envState.services) || running(envState.runtimes)
})

function navigate(view) {
  currentView = view
  // 只挂载当前视图（{#if} 切换，切走即卸载），切到「已安装软件」时再补一次全局刷新，确保计数为最新
  if (view === 'software') refreshSignal.update((n) => n + 1)
}

function hideSplash() {
  const el = document.getElementById('splash')
  if (el) el.classList.add('hide')
}

onMount(() => {
  hideSplash()
  loadEnvBar()

  api.debugLog('onMount 开始, isWailsEnv=' + (typeof window !== 'undefined' && (window as any).go !== undefined))

  // Subscribe to backend reactive events → auto-refresh stores
  const unsubEvents = subscribeReactiveEvents()

  // 服务启停后刷新状态栏环境/运行数据
  const unsubRefresh = refreshSignal.subscribe(() => loadEnvBar())

  // 环境编排进度事件 → 更新全屏操作进度
  const unsubOp = subscribeOperationEvents((name, data) => {
    if (name === 'begin') {
      const steps = (data?.steps || []).map((s) => ({
        id: s.id,
        label: s.label,
        state:
          s.state === 'done' ? 'done' : s.state === 'error' ? 'error' : s.state === 'active' ? 'active' : 'pending',
        message: s.message || '',
      }))
      nanoOperation.set({ ...nanoOperation.get(), title: data?.title || nanoOperation.get().title, steps })
    } else if (name === 'step') {
      const op = nanoOperation.get()
      const steps = op.steps.map((st) =>
        st.id === data?.id
          ? {
              ...st,
              state:
                data.state === 'done'
                  ? 'done'
                  : data.state === 'error'
                    ? 'error'
                    : data.state === 'active'
                      ? 'active'
                      : st.state,
              message: data.message || st.message,
            }
          : st,
      )
      nanoOperation.set({ ...op, steps, message: data?.message || op.message })
    } else if (name === 'end') {
      if (data && data.ok === false && nanoOperation.get().status === 'running') {
        nanoOperation.set({
          ...nanoOperation.get(),
          status: 'error',
          error: data.message || '操作失败',
          message: '操作失败',
        })
      }
    }
  })

  new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('timeout')), 15000)
    api.debugLog('getSettings 开始调用')
    api
      .getSettings()
      .then(resolve, reject)
      .finally(() => clearTimeout(timer))
  })
    .then((settings) => {
      applyTheme(settings.theme)
      api.debugLog(
        'getSettings 成功, installDir=[' +
          settings.installDir +
          '] len=' +
          (settings.installDir?.length || 0) +
          ' showWizard=' +
          !settings.installDir,
      )
      console.log(
        '[DevStack] getSettings installDir:',
        JSON.stringify(settings.installDir),
        'len:',
        settings.installDir?.length,
      )
      if (!settings.installDir) showWizard = true
    })
    .catch((e) => {
      api.debugLog('getSettings 失败: ' + e.message)
      // 仅在不超时且确无 installDir 时才引导；超时/网络等任意失败不应弹设置向导（F11）
      console.warn('[DevStack] getSettings 失败，使用默认设置:', e)
    })

  return () => {
    unsubEvents()
    unsubOp()
    unsubRefresh()
  }
})

function wizardComplete() {
  showWizard = false
}

async function startAll() {
  // 顶部按钮只负责启动「当前已激活环境」的组件，不切换环境（切换在「环境管理」页做）
  const env = await api.getEnvironment().catch(() => ({ activeProfileID: '' }))
  const envId = env?.activeProfileID || ''
  if (!envId) {
    pushToast('尚未激活环境，请先在「环境管理」页激活', 'info')
    return
  }
  await runOperation({
    action: 'run',
    title: '正在启动组件',
    call: () => api.runEnvironment(envId),
    successMsg: '组件已启动',
    onDone: () => refreshSignal.update((n) => n + 1),
  })
}

async function stopAll() {
  const env = await api.getEnvironment().catch(() => ({ services: {}, runtimes: {} }))
  const runningSvcEntries = Object.entries(env.services || {}).filter(([, s]) => s.state === 'running')
  const runningRuntimes = Object.entries(env.runtimes || {}).filter(([, r]) => r.state === 'running')
  const total = runningSvcEntries.length + runningRuntimes.length
  if (total === 0) {
    pushToast('没有正在运行的组件', 'info')
    return
  }

  // 逐个停止运行中的组件，不清除环境激活状态
  await runOperation({
    action: 'stop-all',
    title: '正在停止组件',
    call: async () => {
      const results = await Promise.all([
        ...runningSvcEntries.map(([id]) =>
          api
            .stopService(id)
            .then(() => true)
            .catch(() => false),
        ),
        ...runningRuntimes.map(([id]) =>
          api
            .stopRuntime(id)
            .then(() => true)
            .catch(() => false),
        ),
      ])
      const failed = results.filter((ok) => !ok).length
      if (failed > 0) throw new Error(`有 ${failed} 个组件未能停止`)
    },
    successMsg: `已停止 ${total} 个组件`,
    onDone: () => refreshSignal.update((n) => n + 1),
  })
}

let restartingNginx = $derived(($nanoBusy['restart:nginx'] || 0) > 0)

async function restartNginx() {
  // 获取当前环境中的 nginx 服务完整 ID（如 nginx@1.24.0）
  const env = await api.getEnvironment().catch(() => ({ services: {} }))
  const nginxEntry = Object.entries(env.services || {}).find(([id]) => id.startsWith('nginx@'))
  const serviceId = nginxEntry ? nginxEntry[0] : 'nginx'
  // 仅在 Nginx 运行中时重启；未运行直接提示，避免"重启=启动"的误导语义
  if (nginxEntry?.[1]?.state !== 'running') {
    pushToast('Nginx 未运行，无需重启（请先点击绿色启动按钮）', 'info')
    return
  }
  await safeInvoke('restart:nginx', () => api.restartService(serviceId), {
    successMsg: 'Nginx 已重启',
    errorPrefix: '重启失败',
  })
  refreshSignal.update((n) => n + 1)
}

// 顶栏标题与侧边栏导航文案保持一致（Sidebar.svelte 为准）
let pageTitle = $derived(
  {
    dashboard: '控制面板',
    sites: '项目与网站',
    software: '已安装软件',
    environment: '环境管理',
    packages: '软件仓库',
    logs: '日志监控',
    snapshots: '快照备份',
    settings: '系统设置',
  }[currentView] || '',
)
</script>

<div class="flex h-screen w-screen overflow-hidden">
  <Sidebar {currentView} on:navigate={(e) => navigate(e.detail)} />

  <div class="flex-1 flex flex-col overflow-hidden">
    <header class="flex justify-between items-center px-6 py-2.5 bg-base-100 border-b border-base-300 shrink-0 rounded-none">
      <div class="text-base font-semibold">{pageTitle}</div>
      {#if currentView === 'dashboard'}
        <div class="flex gap-2">
          <button class="btn btn-circle btn-sm bg-success text-white border-none hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:brightness-100" title="启动当前环境的组件" onclick={startAll} disabled={$operation.active}>
            <svg viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path d="M8 5v14l11-7z"/></svg>
          </button>
          <button class="btn btn-circle btn-sm bg-warning text-white border-none hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:brightness-100" title={nginxRunning ? '重启 Nginx' : 'Nginx 未运行，无需重启'} onclick={restartNginx} disabled={$operation.active || restartingNginx || !nginxRunning}>
            {#if restartingNginx}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="16" height="16"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            {/if}
          </button>
          <button class="btn btn-circle btn-sm bg-error text-white border-none hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:brightness-100" title={anyRunning ? '停止当前环境的组件' : '当前没有正在运行的组件'} onclick={stopAll} disabled={$operation.active || !anyRunning}>
            <svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
          </button>
        </div>
      {/if}
    </header>

    <main class="flex-1 overflow-y-auto overflow-x-hidden px-8 py-6 bg-base-200">
      <!-- 仅挂载当前视图：隐藏视图不订阅 refreshSignal / 不轮询，避免每次 reactive 事件触发全部视图并行 reload（F14） -->
      {#if currentView === 'dashboard'}<Dashboard />{/if}
      {#if currentView === 'sites'}<Sites />{/if}
      {#if currentView === 'software'}<Software />{/if}
      {#if currentView === 'environment'}<Environment />{/if}
      {#if currentView === 'packages'}<Packages />{/if}
      {#if currentView === 'logs'}<Logs />{/if}
      {#if currentView === 'snapshots'}<Snapshots />{/if}
      {#if currentView === 'settings'}<Settings />{/if}
    </main>

    <footer class="flex items-center gap-4 px-6 py-1.5 bg-base-100 border-t border-base-300 text-xs text-base-content/60 shrink-0 rounded-none">
      <span class="flex items-center gap-1.5 min-w-0">
        <span>环境:</span>
        <strong class="shrink-0 {activeEnvID ? 'text-base-content/80' : 'text-base-content/40 font-normal'}">{activeEnvName}</strong>
        {#if runningServices.length > 0}
          <span class="shrink-0">(</span>
          <span class="flex items-center gap-3.5 min-w-0">
            {#each runningServices as svc (svc)}
              <span class="flex items-center gap-1 whitespace-nowrap">
                <span class="w-1.5 h-1.5 rounded-full bg-success shrink-0"></span>
                <span>{svc}</span>
              </span>
            {/each}
          </span>
          <span class="shrink-0">)</span>
        {:else if activeEnvID}
          <!-- 仅在确实激活了环境时才标注停止，未激活时不再叠加「已停止」造成语义重复 -->
          <span class="text-base-content/40">(已停止)</span>
        {/if}
      </span>
      <span class="flex items-center gap-1.5 ml-auto">
        {#if $syncInfo.syncing}
          <span class="loading loading-spinner loading-xs text-primary"></span>
          <span>{$syncInfo.msg}</span>
        {:else if $syncInfo.msg}
          <span class="text-warning">{$syncInfo.msg}</span>
        {:else if $syncInfo.at}
          <span>上次同步 {$syncInfo.at}</span>
        {/if}
      </span>
    </footer>
  </div>
</div>

<div class:hidden={!showWizard}>
  <SetupWizard on:complete={wizardComplete} />
</div>

<GlobalLoading />
<GlobalOperation />
<GlobalToast />