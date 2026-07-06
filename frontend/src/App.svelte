<script>
  import { onMount } from 'svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Sites from './components/Sites.svelte'
  import Database from './components/Database.svelte'
  import Languages from './components/Languages.svelte'
  import WebServer from './components/WebServer.svelte'
  import Packages from './components/Packages.svelte'
  import Logs from './components/Logs.svelte'
  import Settings from './components/Settings.svelte'
  import SetupWizard from './components/SetupWizard.svelte'
  import { api } from './lib/api.js'
  import { refreshSignal, toasts, addToast, globalLoading } from './lib/stores.js'

  let currentView = 'dashboard'
  let showWizard = false
  let appReady = false
  let topBusy = false

  function navigate(view) {
    currentView = view
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme || 'light')
  }

  onMount(async () => {
    try {
      const settings = await api.getSettings()
      applyTheme(settings.theme)
      if (!settings.installDir) {
        showWizard = true
      }
    } catch (e) {
      showWizard = true
    } finally {
      appReady = true
    }
  })

  function wizardComplete() {
    showWizard = false
  }

  async function startAll() {
    if (topBusy) return
    topBusy = true
    globalLoading.set(true)
    try {
      const svcs = await api.getServices()
      // DAG start order: nginx (depends on nothing) → php (nginx needs it) → mysql, redis
      const order = { nginx: 0, php: 1, mysql: 2, redis: 2 }
      const ids = svcs
        .filter(s => s.installed && order[s.id] !== undefined)
        .sort((a, b) => order[a.id] - order[b.id])
        .map(s => s.id)
      let ok = 0, fail = 0
      for (const id of ids) {
        try { await api.startService(id); ok++ }
        catch (e) { fail++ }
      }
      if (fail === 0) addToast(`${ok} 个服务已启动`, 'success')
      else addToast(`${ok} 个成功，${fail} 个失败`, fail > 0 ? 'error' : 'success')
    } catch (e) {
      addToast('操作失败', 'error')
    }
    refreshSignal.update(n => n + 1)
    globalLoading.set(false)
    topBusy = false
  }

  async function stopAll() {
    if (topBusy) return
    topBusy = true
    globalLoading.set(true)
    try {
      const svcs = await api.getServices()
      const order = { nginx: 0, php: 1, mysql: 2, redis: 2 }
      const ids = svcs
        .filter(s => s.installed && s.status === 'running' && order[s.id] !== undefined)
        .sort((a, b) => order[b.id] - order[a.id])
        .map(s => s.id)
      if (ids.length === 0) {
        addToast('没有正在运行的服务', 'info')
        globalLoading.set(false)
        topBusy = false
        return
      }
      let ok = 0, fail = 0
      for (const id of ids) {
        try { await api.stopService(id); ok++ }
        catch (e) { fail++ }
      }
      if (fail === 0) addToast(`${ok} 个服务已停止`, 'success')
      else addToast(`${ok} 个成功，${fail} 个失败`, fail > 0 ? 'error' : 'success')
    } catch (e) {
      addToast('操作失败', 'error')
    }
    refreshSignal.update(n => n + 1)
    globalLoading.set(false)
    topBusy = false
  }

  async function restartNginx() {
    if (topBusy) return
    topBusy = true
    globalLoading.set(true)
    try {
      await api.restartService('nginx')
      addToast('Nginx 已重启', 'success')
    } catch (e) {
      addToast('重启失败: ' + (e.message || e), 'error')
    }
    refreshSignal.update(n => n + 1)
    globalLoading.set(false)
    topBusy = false
  }

  $: pageTitle = {
    dashboard: '仪表盘',
    sites: '网站',
    database: '数据库',
    languages: '语言',
    webserver: 'Web 服务器',
    packages: '软件包',
    logs: '日志',
    settings: '设置',
  }[currentView] || ''
</script>

<div class="splash" class:app-ready={appReady}>
  <div class="splash-spinner"></div>
  <p>加载中...</p>
</div>

<div class="app-container" class:app-ready={appReady}>
  <Sidebar {currentView} on:navigate={(e) => navigate(e.detail)} />
  <div class="main-area">
    <header class="top-bar">
      <div class="page-info">
        {pageTitle}
      </div>
      <div class="load-bar" class:active={$globalLoading}></div>
      <div class="top-actions">
        <button class="top-btn green" title="启动全部服务 (Nginx → PHP → MySQL + Redis)" on:click={startAll} disabled={topBusy}>▶</button>
        <button class="top-btn orange" title="重启 Nginx" on:click={restartNginx} disabled={topBusy}>⟳</button>
        <button class="top-btn red" title="停止全部服务 (逆向顺序)" on:click={stopAll} disabled={topBusy}>■</button>
      </div>
    </header>
    <main class="main-content">
      <div class="view-stack">
        <div class="view-pane" class:active={currentView === 'dashboard'}><Dashboard /></div>
        <div class="view-pane" class:active={currentView === 'sites'}><Sites /></div>
        <div class="view-pane" class:active={currentView === 'database'}><Database /></div>
        <div class="view-pane" class:active={currentView === 'languages'}><Languages /></div>
        <div class="view-pane" class:active={currentView === 'webserver'}><WebServer /></div>
        <div class="view-pane" class:active={currentView === 'packages'}><Packages /></div>
        <div class="view-pane" class:active={currentView === 'logs'}><Logs /></div>
        <div class="view-pane" class:active={currentView === 'settings'}><Settings /></div>
      </div>
    </main>
  </div>
</div>

<div class="wizard-layer" class:hidden={!showWizard}>
  <SetupWizard on:complete={wizardComplete} />
</div>

<div class="toast-container">
  {#each $toasts as t (t.id)}
    <div class="toast toast-{t.type}">{t.msg}</div>
  {/each}
</div>

<style>
  .splash {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    background: var(--bg-primary);
    color: var(--text-muted);
    transition: opacity 0.3s;
  }
  .splash.app-ready {
    opacity: 0;
    pointer-events: none;
  }

  .splash-spinner {
    width: 32px; height: 32px;
    border: 3px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .app-container {
    display: flex;
    height: 100vh;
    width: 100vw;
    opacity: 0;
    transition: opacity 0.3s;
  }
  .app-container.app-ready {
    opacity: 1;
  }

  .main-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .top-bar {
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 24px;
    background: var(--bg-card);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .page-info {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .load-bar {
    position: absolute;
    bottom: 0;
    left: 0;
    height: 1.5px;
    width: 0;
    background: var(--accent);
    transition: width 0.15s, opacity 0.15s;
    opacity: 0;
    pointer-events: none;
  }
  .load-bar.active {
    width: 100%;
    opacity: 1;
    animation: load-slide 1s ease-in-out infinite;
  }
  @keyframes load-slide {
    0% { width: 0; margin-left: 0; }
    50% { width: 70%; margin-left: 15%; }
    100% { width: 0; margin-left: 100%; }
  }

  .top-actions { display: flex; gap: 8px; }

  .top-btn {
    width: 34px; height: 34px;
    border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 14px;
    color: #fff;
  }
  .top-btn:hover { filter: brightness(1.15); }
  .top-btn:active { filter: brightness(0.9); }
  .top-btn:disabled { opacity: 0.4; cursor: default; filter: none; }
  .top-btn.green { background: var(--green); }
  .top-btn.orange { background: var(--orange); }
  .top-btn.red { background: var(--red); }

  .main-content {
    flex: 1;
    overflow: hidden;
    scrollbar-gutter: stable;
    padding: 24px 32px;
    background: var(--bg-primary);
  }

  .view-stack { position: relative; }
  .view-pane { display: none; }
  .view-pane.active { display: block; }

  .wizard-layer {
    position: fixed;
    inset: 0;
    z-index: 50;
    background: var(--bg-primary);
  }
  .wizard-layer.hidden { display: none; }

  .toast-container {
    position: fixed;
    top: 60px;
    right: 24px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 9999;
    pointer-events: none;
  }

  .toast {
    padding: 10px 18px;
    border-radius: var(--radius);
    font-size: 13px;
    color: #fff;
    box-shadow: 0 4px 12px rgba(0,0,0,0.2);
    animation: toast-in 0.25s ease-out;
    pointer-events: auto;
  }
  .toast-success { background: var(--green); }
  .toast-error { background: var(--red); }
  .toast-info { background: var(--accent); }

  @keyframes toast-in {
    from { opacity: 0; transform: translateX(40px); }
    to { opacity: 1; transform: translateX(0); }
  }
</style>
