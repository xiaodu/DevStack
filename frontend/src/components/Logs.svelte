<script>
  import { onMount, onDestroy, afterUpdate } from 'svelte'
  import { api } from '../lib/api.js'

  let services = []
  let selectedService = null
  let logEntries = []
  let autoScroll = true
  let logContainer
  let loading = true
  let refreshInterval

  async function loadServices() {
    try {
      services = await api.getServices()
      if (!selectedService && services.length > 0) {
        selectedService = services[0].id
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function loadLogs() {
    if (!selectedService) return
    try {
      logEntries = await api.getLogs(selectedService)
    } catch (e) {
      console.error(e)
    }
  }

  function selectService(id) {
    selectedService = id
    loadLogs()
  }

  afterUpdate(() => {
    if (autoScroll && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight
    }
  })

  function handleScroll() {
    if (!logContainer) return
    const { scrollTop, scrollHeight, clientHeight } = logContainer
    autoScroll = scrollHeight - scrollTop - clientHeight < 30
  }

  onMount(async () => {
    await loadServices()
    await loadLogs()
    refreshInterval = setInterval(loadLogs, 3000)
  })
  onDestroy(() => clearInterval(refreshInterval))
</script>

<div class="logs-page">
  <div class="page-header">
    <div>
      <h1 class="page-title">日志查看</h1>
      <p class="page-desc">实时监控服务日志</p>
    </div>
    <div class="log-controls">
      <select bind:value={selectedService} on:change={(e) => selectService(e.target.value)}>
        {#each services as svc}
          <option value={svc.id}>{svc.name}</option>
        {/each}
      </select>
      <button class="btn btn-sm btn-outline" on:click={loadLogs}>刷新</button>
      <button class="btn btn-sm btn-outline" on:click={() => autoScroll = !autoScroll}>
        {autoScroll ? '自动滚动' : '手动滚动'}
      </button>
    </div>
  </div>

  <div class="log-viewer" bind:this={logContainer} on:scroll={handleScroll}>
    {#if loading}
      <div class="loading">加载中...</div>
    {:else if logEntries.length === 0}
      <div class="empty">暂无日志</div>
    {:else}
      {#each logEntries as entry}
        <div class="log-line">
          <span class="log-time">[{entry.time}]</span>
          <span class="log-msg">{entry.content}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .logs-page { display: flex; flex-direction: column; height: calc(100vh - 48px); }
  .page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-shrink: 0; }
  .log-controls { display: flex; gap: 8px; align-items: center; }
  .log-controls select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    color: var(--text-primary);
    font-size: 13px;
  }
  .log-viewer {
    flex: 1;
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: var(--radius);
    padding: 14px;
    font-family: var(--font-mono);
    font-size: 12px;
    overflow-y: auto;
    line-height: 1.8;
  }
  .log-line { display: flex; gap: 10px; }
  .log-time { color: #64748b; flex-shrink: 0; }
  .log-msg { color: #cbd5e1; word-break: break-all; }
  .log-msg:empty { display: none; }
  .loading, .empty { color: var(--text-muted); text-align: center; padding: 40px; }
</style>
