<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let services = []
  let selectedId = 'nginx'
  let loading = true
  let nginxConfig = {}
  let configLoading = false

  const bodySizeOptions = ['1M', '8M', '16M', '32M', '64M', '128M', '256M', '512M', '1024M']

  const recommendedConfig = {
    workerConnections: '2048',
    clientMaxBodySize: '64m',
    gzipEnabled: 'true',
    staticCacheDays: '7',
    keepaliveTimeout: '65',
    serverTokens: 'false',
  }

  async function load() {
    try {
      const all = await api.getServices()
      services = all.filter(s => s.category === 'webserver' && s.installed)
      if (services.length > 0) selectedId = services[0].id
      await loadConfig()
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function loadConfig() {
    configLoading = true
    try {
      nginxConfig = await api.getServiceConfig('nginx')
    } catch (e) {
      console.error(e)
    } finally {
      configLoading = false
    }
  }

  function getSvc(id) { return services.find(s => s.id === id) }

  function boolVal(key) {
    return nginxConfig[key] === 'true'
  }

  function intVal(key) {
    return parseInt(nginxConfig[key]) || 0
  }

  function getRecommendedHint(key, label) {
    const val = recommendedConfig[key]
    if (!val) return ''
    return `推荐值: ${val}${label ? ` · ${label}` : ''}`
  }

  function loadRecommended() {
    nginxConfig = { ...recommendedConfig }
    addToast('已加载推荐配置')
  }

  async function saveNginxConfig() {
    try {
      const vals = {
        workerConnections: String(intVal('workerConnections')),
        clientMaxBodySize: nginxConfig.clientMaxBodySize || '64m',
        gzipEnabled: String(boolVal('gzipEnabled')),
        staticCacheDays: String(intVal('staticCacheDays')),
        keepaliveTimeout: String(intVal('keepaliveTimeout')),
        serverTokens: String(boolVal('serverTokens')),
      }
      await api.saveServiceConfig('nginx', vals)
      await api.applyServiceConfig('nginx')
      addToast('Nginx 配置已保存并生效')
      await load()
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
          on:click={() => { selectedId = svc.id; loadConfig() }}
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
        <h2>{current.name} {current.version}</h2>
        <div class="header-actions">
          <button
            class="btn btn-sm {current.status === 'running' ? 'btn-danger' : 'btn-success'}"
            on:click={() => toggle(current.id)}
          >
            {current.status === 'running' ? '停止' : '启动'}
          </button>
        </div>
      </div>

      <div class="status-bar">
        <span class="status-dot" class:running={current.status === 'running'}></span>
        <span class="status-text">{current.status === 'running' ? '运行中' : '已停止'}</span>
        {#if current.status === 'running' && current.uptime}
          <span class="uptime">运行 {current.uptime}</span>
        {/if}
        {#if current.port}
          <span class="port-tag">端口 {current.port}</span>
        {/if}
      </div>

      {#if configLoading}
        <div class="loading-hint">加载配置...</div>
      {:else}
        <div class="section">
          <div class="section-header">
            <h3>性能配置</h3>
            <button class="btn btn-sm btn-outline" on:click={loadRecommended}>加载推荐值</button>
          </div>
          <div class="config-grid">
            <div class="row">
              <label class="form-label">
                <span>最大并发连接数</span>
                <input class="form-input" type="number" min="1024" max="8192" step="512"
                  value={intVal('workerConnections')}
                  on:change={e => nginxConfig.workerConnections = String(e.target.value)} />
                <span class="hint">{getRecommendedHint('workerConnections', 'CPU核数 × 1024')}</span>
              </label>
              <label class="form-label">
                <span>Keepalive 超时(秒)</span>
                <input class="form-input" type="number" min="10" max="120"
                  value={intVal('keepaliveTimeout')}
                  on:change={e => nginxConfig.keepaliveTimeout = String(e.target.value)} />
                <span class="hint">{getRecommendedHint('keepaliveTimeout', '长连接保持时间')}</span>
              </label>
            </div>
            <div class="row">
              <label class="form-label">
                <span>上传文件最大大小</span>
                <select class="form-input" bind:value={nginxConfig.clientMaxBodySize}>
                  {#each bodySizeOptions as opt}
                    <option value={opt.toLowerCase()}>{opt}</option>
                  {/each}
                </select>
                <span class="hint">{getRecommendedHint('clientMaxBodySize')}</span>
              </label>
              <label class="form-label">
                <span>静态资源缓存(天)</span>
                <input class="form-input" type="number" min="0" max="365"
                  value={intVal('staticCacheDays')}
                  on:change={e => nginxConfig.staticCacheDays = String(e.target.value)} />
                <span class="hint">{getRecommendedHint('staticCacheDays', '0 = 不缓存')}</span>
              </label>
            </div>
          </div>
        </div>

        <div class="section">
          <h3>功能开关</h3>
          <div class="toggle-grid">
            <label class="toggle-label">
              <input type="checkbox" checked={boolVal('gzipEnabled')}
                on:change={e => nginxConfig.gzipEnabled = String(e.target.checked)} />
              <span>Gzip 压缩</span>
              <span class="hint">{getRecommendedHint('gzipEnabled', '减少传输大小 60-80%')}</span>
            </label>
            <label class="toggle-label">
              <input type="checkbox" checked={boolVal('serverTokens')}
                on:change={e => nginxConfig.serverTokens = String(e.target.checked)} />
              <span>显示服务器版本</span>
              <span class="hint">{getRecommendedHint('serverTokens', '建议关闭')}</span>
            </label>
          </div>
        </div>

        <div class="section">
          <h3>文件信息</h3>
          <div class="info-grid">
            <label class="form-label">
              <span>配置文件</span>
              <input class="form-input" value="nginx/{current.version}/conf/nginx.conf" readonly />
            </label>
            <label class="form-label">
              <span>默认网站根目录</span>
              <input class="form-input" value="www/" readonly />
            </label>
          </div>
        </div>

        <div class="actions">
          <button class="btn btn-primary" on:click={saveNginxConfig}>保存并生效</button>
          <span class="action-hint">保存后自动重新生成配置文件并重启 Nginx</span>
        </div>
      {/if}
    {:else}
      <div class="empty-state">
        <div class="icon">⚡</div>
        <p>未找到 Web 服务器</p>
        <p style="font-size:12px;margin-top:4px;color:var(--text-muted)">请前往「软件包」页面安装 Nginx</p>
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
    margin-bottom: 16px;
  }
  .detail-header h2 { font-size: 20px; font-weight: 600; }

  .status-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    margin-bottom: 24px;
    font-size: 13px;
  }

  .status-dot {
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--text-muted);
  }
  .status-dot.running { background: var(--green); }
  .status-text { color: var(--text-secondary); }
  .uptime { color: var(--text-muted); }
  .port-tag {
    margin-left: auto;
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 3px;
    background: var(--accent-light);
    color: var(--accent);
  }

  .section {
    background: var(--bg-main, #fff);
    border-radius: var(--radius);
    padding: 20px;
    margin-bottom: 16px;
    border: 1px solid var(--border);
  }
  .section-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 14px;
  }
  .section h3 {
    font-size: 14px;
    font-weight: 600;
    color: var(--accent);
    margin: 0;
  }

  .config-grid {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .form-label {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-label span:first-child {
    font-size: 13px;
    color: var(--text-secondary);
  }
  .form-label .hint {
    font-size: 11px;
    color: var(--text-muted);
  }

  .toggle-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    color: var(--text-primary);
  }
  .toggle-label input[type="checkbox"] {
    width: 18px; height: 18px;
    accent-color: var(--accent);
  }
  .toggle-label .hint {
    font-size: 12px;
    color: var(--text-muted);
    margin-left: auto;
  }

  .info-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 24px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
  .action-hint {
    font-size: 12px;
    color: var(--text-muted);
  }

  .loading-hint {
    color: var(--text-muted);
    padding: 40px;
    text-align: center;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-muted);
    font-size: 14px;
  }
  .empty-state .icon { font-size: 48px; margin-bottom: 12px; opacity: 0.5; }
</style>
