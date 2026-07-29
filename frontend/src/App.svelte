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
import { subscribeReactiveEvents } from './lib/events'
import GlobalLoading from './lib/GlobalLoading.svelte'
import GlobalToast from './lib/GlobalToast.svelte'
import { safeInvoke } from './lib/safeInvoke'
import { addToast, pushToast, refreshSignal } from './lib/stores'

let currentView = 'dashboard'
let showWizard = false

function navigate(view) {
  currentView = view
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

  Promise.race([api.getSettings(), new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), 15000))])
    .then((settings) => {
      applyTheme(settings.theme)
      if (!settings.installDir) showWizard = true
    })
    .catch((e) => {
      console.warn('[DevStack] getSettings 失败:', e)
      showWizard = true
    })

  return unsubEvents
})

function wizardComplete() {
  showWizard = false
}

async function startAll() {
  await safeInvoke('activateEnvironment', () => api.activateEnvironment(''), {
    successMsg: '环境已按依赖顺序激活',
    errorPrefix: '环境激活失败',
  })
  refreshSignal.update((n) => n + 1)
}

async function stopAll() {
  const env = await api.getActiveEnvironment()
  const anyRunning =
    Object.values(env.services || {}).some((s) => s.state === 'running') ||
    Object.values(env.runtimes || {}).some((r) => r.state === 'running')
  if (!anyRunning) {
    pushToast('没有正在运行的服务', 'info')
    return
  }
  await safeInvoke('deactivateEnvironment', () => api.deactivateEnvironment(), {
    successMsg: '环境已按依赖顺序安全停止',
    errorPrefix: '环境停止失败',
  })
  refreshSignal.update((n) => n + 1)
}

async function restartNginx() {
  await safeInvoke('restart:nginx', () => api.restartService('nginx'), {
    successMsg: 'Nginx 已重启',
    errorPrefix: '重启失败',
  })
  refreshSignal.update((n) => n + 1)
}

$: pageTitle =
  {
    dashboard: '仪表盘',
    sites: '项目',
    software: '安装软件',
    environment: '环境',
    packages: '软件仓库',
    logs: '日志',
    settings: '设置',
  }[currentView] || ''
</script>

<div class="flex h-screen w-screen overflow-hidden">
  <Sidebar {currentView} on:navigate={(e) => navigate(e.detail)} />

  <div class="flex-1 flex flex-col overflow-hidden">
    <header class="flex justify-between items-center px-6 py-2.5 bg-base-100 border-b border-base-300 shrink-0">
      <div class="text-base font-semibold">{pageTitle}</div>
      <div class="flex gap-2">
        <button class="btn btn-circle btn-sm bg-success text-white border-none hover:brightness-110" title="激活当前项目所需环境" on:click={startAll}>▶</button>
        <button class="btn btn-circle btn-sm bg-warning text-white border-none hover:brightness-110" title="重启 Nginx" on:click={restartNginx}>⟳</button>
        <button class="btn btn-circle btn-sm bg-error text-white border-none hover:brightness-110" title="停止全部服务" on:click={stopAll}>■</button>
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
  </div>
</div>

<div class:hidden={!showWizard}>
  <SetupWizard on:complete={wizardComplete} />
</div>

<GlobalLoading />
<GlobalToast />