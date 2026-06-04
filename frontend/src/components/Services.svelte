<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let services = []
  let availableVersions = {}
  let loading = true
  let expandedService = null
  let installing = {}

  async function load() {
    try {
      services = await api.getServices()
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function loadVersions(type) {
    try {
      availableVersions[type] = await api.getAvailableVersions(type)
    } catch (e) {
      availableVersions[type] = []
    }
  }

  async function toggleService(svc) {
    try {
      if (svc.status === 'running') {
        await api.stopService(svc.id)
      } else {
        await api.startService(svc.id)
      }
      await load()
    } catch (e) {
      console.error(e)
    }
  }

  async function installVersion(type, version, url) {
    const key = `${type}-${version}`
    installing[key] = true
    try {
      await api.downloadService(type, version, url)
      await load()
    } catch (e) {
      console.error(e)
    } finally {
      installing[key] = false
    }
  }

  async function switchVersion(svc, version) {
    try {
      await api.setServiceVersion(svc.id, version)
      await load()
    } catch (e) {
      console.error(e)
    }
  }

  function toggleExpand(type) {
    if (expandedService === type) {
      expandedService = null
    } else {
      expandedService = type
      if (!availableVersions[type]) {
        loadVersions(type)
      }
    }
  }

  const serviceIcons = {
    nginx: '🌐',
    mysql: '🗄️',
    redis: '⚡',
    php: '🐘',
  }

  onMount(load)
</script>

<div class="services-page">
  <h1 class="page-title">服务管理</h1>
  <p class="page-desc">管理所有服务的启停、版本切换及安装</p>

  {#if loading}
    <div class="loading">加载中...</div>
  {:else}
    <div class="svc-list">
      {#each services as svc}
        <div class="svc-item" class:expanded={expandedService === svc.type}>
          <div class="svc-main">
            <div class="svc-left">
              <span class="svc-icon">{serviceIcons[svc.id] || '📦'}</span>
              <div class="svc-info">
                <div class="svc-name">{svc.name}</div>
                <div class="svc-meta">
                  <span class="ver-badge">{svc.version}</span>
                  <span class="port-badge">端口 {svc.port}</span>
                  {#if svc.installed}
                    <span class="installed-badge">已安装</span>
                  {:else}
                    <span class="missing-badge">未安装</span>
                  {/if}
                </div>
              </div>
            </div>
            <div class="svc-right">
              <span class="status-indicator" class:on={svc.status === 'running'} class:off={svc.status !== 'running'}>
                <span class="status-dot" class:running={svc.status === 'running'} class:stopped={svc.status !== 'running'}></span>
                {svc.status === 'running' ? '运行中' : '已停止'}
              </span>
              <div class="svc-actions">
                {#if svc.status === 'running'}
                  <button class="btn btn-sm btn-success" on:click={() => api.restartService(svc.id)}>重启</button>
                  <button class="btn btn-sm btn-danger" on:click={() => toggleService(svc)}>停止</button>
                {:else}
                  <button class="btn btn-sm btn-primary" on:click={() => toggleService(svc)} disabled={svc.status === 'starting' || svc.status === 'stopping'}>
                    {svc.status === 'starting' ? '启动中...' : '启动'}
                  </button>
                {/if}
                <button class="btn btn-sm btn-outline" on:click={() => toggleExpand(svc.type)}>
                  {expandedService === svc.type ? '收起' : '版本'}
                </button>
              </div>
            </div>
          </div>

          {#if expandedService === svc.type}
            <div class="version-panel">
              <div class="vp-header">
                <h4>{svc.name} 可用版本</h4>
                <span class="vp-hint">点击安装或切换版本</span>
              </div>
              {#if availableVersions[svc.type]}
                <div class="vp-list">
                  {#each availableVersions[svc.type] as ver}
                    <div class="vp-item" class:current={ver.version === svc.version}>
                      <div class="vp-left">
                        <span class="vp-ver">{ver.version}</span>
                        {#if ver.version === svc.version}
                          <span class="vp-current">当前使用</span>
                        {/if}
                      </div>
                      <div class="vp-right">
                        {#if ver.version === svc.version}
                          <span class="vp-curr-label">当前版本</span>
                        {:else if svc.installed}
                          <button class="btn btn-sm btn-outline" on:click={() => switchVersion(svc, ver.version)}>切换到此版本</button>
                        {:else}
                          <button class="btn btn-sm btn-primary" on:click={() => installVersion(svc.type, ver.version, ver.url)} disabled={installing[`${svc.type}-${ver.version}`]}>
                            {installing[`${svc.type}-${ver.version}`] ? '安装中...' : '安装'}
                          </button>
                        {/if}
                      </div>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="vp-loading">加载版本列表中...</div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .services-page { max-width: 1000px; }
  .svc-list { display: flex; flex-direction: column; gap: 8px; }
  .svc-item {
    background: var(--bg-card);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .svc-item.expanded { border-color: var(--border-light); }

  .svc-main {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 16px;
  }

  .svc-left { display: flex; align-items: center; gap: 14px; }
  .svc-icon { font-size: 24px; }
  .svc-name { font-size: 15px; font-weight: 600; }
  .svc-meta { display: flex; gap: 6px; margin-top: 4px; flex-wrap: wrap; }
  .ver-badge, .port-badge, .installed-badge, .missing-badge {
    font-size: 11px;
    padding: 1px 7px;
    border-radius: 3px;
    font-family: var(--font-mono);
  }
  .ver-badge { background: var(--bg-active); color: var(--text-muted); }
  .port-badge { background: var(--bg-active); color: var(--text-secondary); }
  .installed-badge { background: var(--green-bg); color: var(--green); }
  .missing-badge { background: var(--yellow-bg); color: var(--yellow); }

  .svc-right { display: flex; align-items: center; gap: 12px; }
  .status-indicator {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 20px;
  }
  .status-indicator.on { background: var(--green-bg); color: var(--green); }
  .status-indicator.off { background: var(--border); color: var(--text-muted); }

  .svc-actions { display: flex; gap: 6px; }

  .version-panel {
    border-top: 1px solid var(--border);
    padding: 14px 16px;
    background: var(--bg-inset);
  }
  .vp-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
  .vp-header h4 { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
  .vp-hint { font-size: 11px; color: var(--text-muted); }
  .vp-list { display: flex; flex-direction: column; gap: 4px; }
  .vp-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    transition: background 0.1s;
  }
  .vp-item:hover { background: var(--bg-hover); }
  .vp-item.current { background: var(--accent-glow); }
  .vp-left { display: flex; align-items: center; gap: 8px; }
  .vp-ver { font-size: 13px; font-family: var(--font-mono); font-weight: 500; }
  .vp-current {
    font-size: 10px;
    background: var(--accent-glow);
    color: var(--accent);
    padding: 1px 6px;
    border-radius: 3px;
  }
  .vp-curr-label {
    font-size: 12px;
    color: var(--accent);
    font-weight: 500;
  }

  .loading, .vp-loading { color: var(--text-muted); padding: 40px; text-align: center; }
  .vp-loading { padding: 10px; font-size: 13px; }
</style>
