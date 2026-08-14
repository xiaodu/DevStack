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
import Software from './components/Software.svelte'
import { api } from './lib/api'
import { subscribeOperationEvents, subscribeReactiveEvents } from './lib/events'
import GlobalLoading from './lib/GlobalLoading.svelte'
import GlobalOperation from './lib/GlobalOperation.svelte'
import GlobalToast from './lib/GlobalToast.svelte'
import { safeInvoke } from './lib/safeInvoke'
import { addToast, nanoOperation, operation, pushToast, refreshSignal, runOperation, syncInfo } from './lib/stores'

let currentView = $state('dashboard')
let showWizard = $state(false)

function navigate(view) {
  currentView = view
  // 视图保持挂载（class:hidden 切换），切换到「软件管理」时强制刷新数据，避免过期状态
  if (view === 'software') refreshSignal.update((n) => n + 1)
}

function applyTheme(theme) {
  document.documentElement.setAttribute('data-theme', theme)
}

function hideSplash() {
  const el = document.getElementById('splash')
  if (el) el.classList.add('hide')
}

onMount(() => {
  hideSplash()

  // Subscribe to backend reactive events → auto-refresh stores
  const unsubEvents = subscribeReactiveEvents()

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

  Promise.race([api.getSettings(), new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), 15000))])
    .then((settings) => {
      applyTheme(settings.theme)
      if (!settings.installDir) showWizard = true
    })
    .catch((e) => {
      console.warn('[DevStack] getSettings 失败:', e)
      showWizard = true
    })

  return () => {
    unsubEvents()
    unsubOp()
  }
})

function wizardComplete() {
  showWizard = false
}

async function startAll() {
  // 获取当前环境状态，使用 activeProfileID 激活
  const env = await api.getEnvironment().catch(() => ({ activeProfileID: '' }))
  const envId = env?.activeProfileID || ''
  if (!envId) {
    pushToast('未激活任何环境，请先在「环境」页激活并运行', 'info')
    return
  }
  await runOperation({
    action: 'activate',
    title: '正在激活环境',
    call: () => api.activateEnvironment(envId),
    successMsg: '环境已按依赖顺序激活',
    onDone: () => refreshSignal.update((n) => n + 1),
  })
}

async function stopAll() {
  const env = await api.getEnvironment().catch(() => ({ services: {}, runtimes: {} }))
  const runningServices = Object.entries(env.services || {}).filter(([, s]) => s.state === 'running')
  const runningRuntimes = Object.entries(env.runtimes || {}).filter(([, r]) => r.state === 'running')
  const total = runningServices.length + runningRuntimes.length
  if (total === 0) {
    pushToast('没有正在运行的服务', 'info')
    return
  }

  // 逐个停止运行中的服务/运行时，不清除环境激活状态
  await runOperation({
    action: 'stop-all',
    title: '正在停止全部组件',
    call: async () => {
      const results = await Promise.all([
        ...runningServices.map(([id]) =>
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
    successMsg: `已停止 ${total} 个运行中的组件`,
    onDone: () => refreshSignal.update((n) => n + 1),
  })
}

async function restartNginx() {
  // 获取当前环境中的 nginx 服务完整 ID（如 nginx@1.24.0）
  const env = await api.getEnvironment().catch(() => ({ services: {} }))
  const nginxEntry = Object.entries(env.services || {}).find(([id]) => id.startsWith('nginx@'))
  const serviceId = nginxEntry ? nginxEntry[0] : 'nginx'
  await safeInvoke('restart:nginx', () => api.restartService(serviceId), {
    successMsg: 'Nginx 已重启',
    errorPrefix: '重启失败',
  })
  refreshSignal.update((n) => n + 1)
}

let pageTitle = $derived(
  {
    dashboard: '仪表盘',
    sites: '项目',
    software: '软件管理',
    environment: '环境',
    packages: '软件仓库',
    logs: '日志',
    settings: '设置',
  }[currentView] || '',
)
</script>

<div class="flex h-screen w-screen overflow-hidden">
  <Sidebar {currentView} on:navigate={(e) => navigate(e.detail)} />

  <div class="flex-1 flex flex-col overflow-hidden">
    <header class="flex justify-between items-center px-6 py-2.5 bg-base-100 border-b border-base-300 shrink-0">
      <div class="text-base font-semibold">{pageTitle}</div>
      <div class="flex gap-2">
        <button class="btn btn-circle btn-sm bg-success text-white border-none hover:brightness-110" title="激活当前项目所需环境" onclick={startAll} disabled={$operation.active}>
          <svg viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path d="M8 5v14l11-7z"/></svg>
        </button>
        <button class="btn btn-circle btn-sm bg-warning text-white border-none hover:brightness-110" title="重启 Nginx" onclick={restartNginx} disabled={$operation.active}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="16" height="16"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
        </button>
        <button class="btn btn-circle btn-sm bg-error text-white border-none hover:brightness-110" title="停止全部服务" onclick={stopAll} disabled={$operation.active}>
          <svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto overflow-x-hidden px-8 py-6 bg-base-200">
      <div class:block={currentView === 'dashboard'} class:hidden={currentView !== 'dashboard'}><Dashboard /></div>
      <div class:block={currentView === 'sites'} class:hidden={currentView !== 'sites'}><Sites /></div>
      <div class:block={currentView === 'software'} class:hidden={currentView !== 'software'}><Software /></div>
      <div class:block={currentView === 'environment'} class:hidden={currentView !== 'environment'}><Environment /></div>
      <div class:block={currentView === 'packages'} class:hidden={currentView !== 'packages'}><Packages /></div>
      <div class:block={currentView === 'logs'} class:hidden={currentView !== 'logs'}><Logs /></div>
      <div class:block={currentView === 'settings'} class:hidden={currentView !== 'settings'}><Settings /></div>
    </main>

    <footer class="flex items-center gap-3 px-6 py-1.5 bg-base-100 border-t border-base-300 text-xs text-base-content/60 shrink-0">
      <span class="flex items-center gap-1.5">
        {#if $syncInfo.syncing}
          <span class="loading loading-spinner loading-xs text-primary"></span>
          <span>{$syncInfo.msg}</span>
        {:else if $syncInfo.msg}
          <span class="text-warning">{$syncInfo.msg}</span>
        {:else if $syncInfo.at}
          <span>上次同步 {$syncInfo.at}</span>
        {/if}
      </span>
      <span class="ml-auto">DevStack</span>
    </footer>
  </div>
</div>

<div class:hidden={!showWizard}>
  <SetupWizard on:complete={wizardComplete} />
</div>

<GlobalLoading />
<GlobalOperation />
<GlobalToast />