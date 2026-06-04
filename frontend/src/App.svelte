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

  let currentView = 'dashboard'
  let showWizard = false
  let appReady = false

  function navigate(view) {
    currentView = view
  }

  onMount(async () => {
    try {
      const settings = await api.getSettings()
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

{#if !appReady}
  <div class="splash">
    <div class="splash-spinner"></div>
    <p>加载中...</p>
  </div>
{:else if showWizard}
  <SetupWizard on:complete={wizardComplete} />
{:else}
  <div class="app-container">
    <Sidebar {currentView} on:navigate={(e) => navigate(e.detail)} />
    <div class="main-area">
      <header class="top-bar">
        <div class="page-info">{pageTitle}</div>
        <div class="top-actions">
          <button class="top-btn green" title="启动全部" on:click={async () => { try { await api.startService('nginx'); await api.startService('mysql'); await api.startService('redis'); } catch(e) {} }}>▶</button>
          <button class="top-btn orange" title="重启" on:click={async () => { try { await api.restartService('nginx'); } catch(e) {} }}>⟳</button>
          <button class="top-btn red" title="停止全部" on:click={async () => { try { await api.stopService('nginx'); await api.stopService('mysql'); await api.stopService('redis'); } catch(e) {} }}>■</button>
        </div>
      </header>
      <main class="main-content">
        {#if currentView === 'dashboard'}
          <Dashboard />
        {:else if currentView === 'sites'}
          <Sites />
        {:else if currentView === 'database'}
          <Database />
        {:else if currentView === 'languages'}
          <Languages />
        {:else if currentView === 'webserver'}
          <WebServer />
        {:else if currentView === 'packages'}
          <Packages />
        {:else if currentView === 'logs'}
          <Logs />
        {:else if currentView === 'settings'}
          <Settings />
        {/if}
      </main>
    </div>
  </div>
{/if}

<style>
  .splash {
    height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    color: var(--text-muted);
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
  }

  .main-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .top-bar {
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

  .top-actions { display: flex; gap: 8px; }

  .top-btn {
    width: 34px; height: 34px;
    border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 14px;
    color: #fff;
    transition: transform 0.1s;
  }
  .top-btn:hover { transform: scale(1.1); }
  .top-btn.green { background: var(--green); }
  .top-btn.orange { background: var(--orange); }
  .top-btn.red { background: var(--red); }

  .main-content {
    flex: 1;
    overflow-y: auto;
    padding: 24px 32px;
    background: var(--bg-primary);
  }
</style>
