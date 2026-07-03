<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let services = []
  let selectedId = 'nginx'
  let loading = true

  async function load() {
    try {
      const all = await api.getServices()
      services = all.filter(s => s.category === 'webserver' && s.installed)
      if (services.length > 0) selectedId = services[0].id
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  function getSvc(id) { return services.find(s => s.id === id) }

  async function saveConfig(svc) {
    const vals = {}
    const port = document.getElementById('nginx-http-port')
    if (port) vals['listen'] = port.value
    try {
      await api.saveServiceConfig(svc.id, vals)
      addToast('保存成功')
    } catch (e) {
      addToast('保存失败: ' + (e.message || e), 'error')
    }
  }

  async function toggle(id) {
    try {
      const svc = getSvc(id)
      if (svc && svc.status === 'running') {
        await api.stopService(id)
      } else {
        await api.startService(id)
      }
      await load()
    } catch (e) {
      addToast(e.message || e, 'error')
    }
  }

  $: current = selectedId ? services.find(s => s.id === selectedId) : null

  onMount(load)
</script>

<div class="ws-page">
  <div class="ws-left">
    <div class="list-header">
      <h3>Web 服务器</h3>
    </div>
    <div class="ws-list">
      {#each services as svc}
        <button
          class="ws-item"
          class:active={selectedId === svc.id}
          on:click={() => selectedId = svc.id}
        >
          <span class="ws-icon">⚡</span>
          <div class="ws-info">
            <span class="ws-name">{svc.name}</span>
            <span class="ws-ver">v{svc.version}</span>
          </div>
          <span class="ws-status {svc.status}"></span>
        </button>
      {/each}
    </div>
  </div>

  <div class="ws-right">
    {#if current}
      <div class="detail-header">
        <h2>{current.name} {current.version} 配置</h2>
        <div class="header-actions">
          <button
            class="btn btn-sm {current.status === 'running' ? 'btn-danger' : 'btn-success'}"
            on:click={() => toggle(current.id)}
          >
            {current.status === 'running' ? '停止' : '启动'}
          </button>
        </div>
      </div>

      <div class="config-grid">
        <div class="config-row">
            <label class="form-label">
              <span>HTTP 端口</span>
              <input id="nginx-http-port" class="form-input" type="number" value="80" />
            </label>
            <label class="form-label">
              <span>HTTPS 端口</span>
              <input class="form-input" type="number" value="443" />
            </label>
        </div>

        <label class="form-label">
          <span>配置文件路径</span>
          <input class="form-input" value="install/nginx/{current.version}/conf/nginx.conf" readonly />
        </label>

        <label class="form-label">
          <span>网站根目录</span>
          <input class="form-input" value="www/" readonly />
        </label>

        <label class="form-label">
          <span>worker 进程数</span>
          <select class="form-input">
            <option>auto</option>
            <option>1</option>
            <option>2</option>
            <option>4</option>
          </select>
        </label>

        <label class="form-label">
          <span>worker 连接数</span>
          <input class="form-input" type="number" value="1024" />
        </label>

        <div class="toggle-row">
          <label class="toggle-label">
            <input type="checkbox" checked />
            <span>Gzip 压缩</span>
          </label>
          <label class="toggle-label">
            <input type="checkbox" checked />
            <span>sendfile</span>
          </label>
          <label class="toggle-label">
            <input type="checkbox" />
            <span>SSL Session Cache</span>
          </label>
        </div>
      </div>

      <div class="actions">
        <button class="btn btn-primary" on:click={() => saveConfig(current)}>保存设置</button>
      </div>
    {:else}
      <div class="empty-state">
        <div class="icon">⚡</div>
        <p>未找到 Web 服务器</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .ws-page {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .ws-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
  }

  .list-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .ws-list { flex: 1; padding: 8px; overflow-y: auto; }

  .ws-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    transition: background 0.1s;
    border: none;
  }
  .ws-item:hover { background: var(--bg-inset); }
  .ws-item.active { background: var(--accent-light); }

  .ws-icon { font-size: 18px; }

  .ws-info { flex: 1; min-width: 0; }
  .ws-name { display: block; font-size: 13px; font-weight: 500; color: var(--text-primary); }
  .ws-ver { display: block; font-size: 11px; color: var(--text-muted); }

  .ws-status {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .ws-status.running { background: var(--green); }
  .ws-status.stopped { background: var(--text-muted); }

  .ws-right {
    overflow-y: auto;
    padding: 24px 32px;
  }

  .detail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 24px;
  }
  .detail-header h2 { font-size: 20px; font-weight: 600; }

  .config-grid {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 700px;
  }

  .config-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .toggle-row {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .toggle-label input[type="checkbox"] {
    width: 16px; height: 16px;
    accent-color: var(--accent);
  }

  .actions {
    margin-top: 24px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
</style>
