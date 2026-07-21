<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast, refreshSignal } from '../lib/stores.js'

  let runtimes = []
  let selectedId = ''
  let loading = true
  let phpConfig = {}
  let configLoading = false

  const typeMeta = {
    php:    { label: 'PHP',    icon: '🐘', color: '#777BB4' },
    node:   { label: 'Node.js', icon: '💚', color: '#339933' },
    python: { label: 'Python', icon: '🐍', color: '#3776AB' },
  }

  const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
  const uploadOptions = ['2M', '8M', '16M', '32M', '64M', '128M', '256M', '512M']
  const realpathOptions = ['512K', '1024K', '2048K', '4096K', '8192K']

  const recommendedPHPConfig = {
    memoryLimit: '256M',
    maxExecutionTime: '300',
    uploadMaxFilesize: '64M',
    postMaxSize: '64M',
    displayErrors: 'false',
    opcacheEnabled: 'true',
    realpathCacheSize: '4096K',
  }

  async function load() {
    loading = true
    try {
      runtimes = await api.getRuntimes()
      if (runtimes.length > 0 && !selectedId) {
        selectedId = runtimes[0].id
      }
      if (current && current.type === 'php') {
        await loadPHPConfig()
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function loadPHPConfig() {
    configLoading = true
    try {
      phpConfig = await api.getServiceConfig('php')
    } catch (e) {
      console.error(e)
    } finally {
      configLoading = false
    }
  }

  function getRecommendedHint(key, label) {
    const val = recommendedPHPConfig[key]
    if (!val) return ''
    return `推荐值: ${val}${label ? ` · ${label}` : ''}`
  }

  function loadRecommendedPHP() {
    phpConfig = { ...recommendedPHPConfig }
    addToast('已加载推荐配置')
  }

  async function toggle(rt) {
    try {
      if (rt.running || rt.status === 'running') {
        await api.stopRuntime(rt.id)
        addToast(`${rt.name} 已停止`)
      } else {
        await api.startRuntime(rt.id)
        addToast(`${rt.name} 已启动`)
      }
      await load()
    } catch (e) {
      addToast('操作失败: ' + (e.message || e), 'error')
    }
  }

  function boolVal(key) {
    return phpConfig[key] === 'true'
  }

  function intVal(key) {
    return parseInt(phpConfig[key]) || 0
  }

  async function savePHPConfig(rt) {
    try {
      const vals = {
        memoryLimit: phpConfig.memoryLimit || '256M',
        maxExecutionTime: String(intVal('maxExecutionTime')),
        uploadMaxFilesize: phpConfig.uploadMaxFilesize || '64M',
        postMaxSize: phpConfig.postMaxSize || '64M',
        displayErrors: String(boolVal('displayErrors')),
        opcacheEnabled: String(boolVal('opcacheEnabled')),
        realpathCacheSize: phpConfig.realpathCacheSize || '4096K',
      }
      await api.saveServiceConfig(rt.id, vals)
      addToast('PHP 配置已保存，重启后生效')
      await load()
    } catch (e) {
      addToast('保存失败: ' + (e.message || e), 'error')
    }
  }

  function statusLabel(rt) {
    if (rt.running || rt.status === 'running') return '运行中'
    if (rt.status === 'starting') return '启动中'
    if (rt.status === 'stopping') return '停止中'
    return '已停止'
  }

  function selectRuntime(id) {
    selectedId = id
    if (current && current.type === 'php') {
      loadPHPConfig()
    } else {
      phpConfig = {}
      configLoading = false
    }
  }

  $: current = selectedId ? runtimes.find(r => r.id === selectedId) : null
  $: currentMeta = current ? (typeMeta[current.type] || { icon: '⚙', color: 'var(--accent)' }) : null

  const unsub = refreshSignal.subscribe(() => load())
  onMount(() => { load(); return unsub })
</script>

<div class="rt-page">
  <div class="rt-left">
    <div class="list-header">
      <h3>运行环境</h3>
    </div>
    <div class="rt-list">
      {#if loading}
        <div class="empty-hint">加载中...</div>
      {:else if runtimes.length === 0}
        <div class="empty-hint">暂无运行环境</div>
      {:else}
        {#each runtimes as rt}
          {@const meta = typeMeta[rt.type] || { icon: '⚙', color: 'var(--accent)' }}
          <button
            class="rt-item"
            class:active={selectedId === rt.id}
            on:click={() => selectRuntime(rt.id)}
          >
            <span class="rt-icon" style="color: {meta.color}">{meta.icon}</span>
            <div class="rt-info">
              <span class="rt-name">{rt.name}</span>
              <span class="rt-ver">v{rt.version}</span>
            </div>
            <span class="rt-status" class:running={rt.running || rt.status === 'running'}></span>
          </button>
        {/each}
      {/if}
    </div>
  </div>

  <div class="rt-right">
    {#if current}
      <div class="detail-header">
        <h2>
          <span class="detail-icon" style="color: {currentMeta.color}">{currentMeta.icon}</span>
          {current.name}
        </h2>
        <div class="header-actions">
          <button
            class="btn btn-sm {current.running || current.status === 'running' ? 'btn-danger' : 'btn-success'}"
            on:click={() => toggle(current)}
          >
            {current.running || current.status === 'running' ? '停止' : '启动'}
          </button>
        </div>
      </div>

      <div class="status-bar">
        <span class="status-dot" class:running={current.running || current.status === 'running'}></span>
        <span class="status-text">{statusLabel(current)}</span>
        {#if current.active}
          <span class="active-tag">命令行默认</span>
        {/if}
        {#if current.port}
          <span class="port-tag">端口 {current.port}</span>
        {/if}
        {#if (current.running || current.status === 'running') && current.uptime}
          <span class="uptime">运行 {current.uptime}</span>
        {/if}
      </div>

      {#if current.type === 'php'}
        {#if configLoading}
          <div class="loading-hint">加载配置...</div>
        {:else}
          <div class="section">
            <div class="section-header">
              <h3>PHP 配置</h3>
              <button class="btn btn-sm btn-outline" on:click={loadRecommendedPHP}>加载推荐值</button>
            </div>
            <div class="config-grid">
              <div class="row">
                <label class="form-label">
                  <span>内存限制</span>
                  <select class="form-input" bind:value={phpConfig.memoryLimit}>
                    {#each memoryOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('memoryLimit')}</span>
                </label>
                <label class="form-label">
                  <span>最大执行时间(秒)</span>
                  <input class="form-input" type="number" min="30" max="600"
                    value={intVal('maxExecutionTime')}
                    on:change={e => phpConfig.maxExecutionTime = String(e.target.value)} />
                  <span class="hint">{getRecommendedHint('maxExecutionTime')}</span>
                </label>
              </div>
              <div class="row">
                <label class="form-label">
                  <span>上传文件最大大小</span>
                  <select class="form-input" bind:value={phpConfig.uploadMaxFilesize}>
                    {#each uploadOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('uploadMaxFilesize')}</span>
                </label>
                <label class="form-label">
                  <span>POST 数据最大大小</span>
                  <select class="form-input" bind:value={phpConfig.postMaxSize}>
                    {#each uploadOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('postMaxSize')}</span>
                </label>
              </div>
              <div class="row">
                <label class="form-label">
                  <span>Realpath 缓存大小</span>
                  <select class="form-input" bind:value={phpConfig.realpathCacheSize}>
                    {#each realpathOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('realpathCacheSize', 'Windows 性能关键')}</span>
                </label>
                <label class="form-label">
                  <span>配置文件</span>
                  <input class="form-input" value="php/{current.version}/php.ini" readonly />
                </label>
              </div>
            </div>
          </div>

          <div class="section">
            <h3>功能开关</h3>
            <div class="toggle-grid">
              <label class="toggle-label">
                <input type="checkbox" checked={boolVal('displayErrors')}
                  on:change={e => phpConfig.displayErrors = String(e.target.checked)} />
                <span>显示错误信息</span>
                <span class="hint">{getRecommendedHint('displayErrors', '生产环境建议关闭')}</span>
              </label>
              <label class="toggle-label">
                <input type="checkbox" checked={boolVal('opcacheEnabled')}
                  on:change={e => phpConfig.opcacheEnabled = String(e.target.checked)} />
                <span>启用 OPcache</span>
                <span class="hint">{getRecommendedHint('opcacheEnabled', '性能提升 30-50%')}</span>
              </label>
            </div>
          </div>

          <div class="actions">
            <button class="btn btn-primary" on:click={() => savePHPConfig(current)}>保存配置</button>
            <span class="action-hint">保存后重新生成 php.ini，需重启 PHP 生效</span>
          </div>
        {/if}
      {:else}
        <div class="section">
          <h3>{currentMeta.label} 信息</h3>
          <div class="config-grid">
            <label class="form-label">
              <span>类型</span>
              <input class="form-input" value={currentMeta.label || current.type} readonly />
            </label>
            <label class="form-label">
              <span>版本</span>
              <input class="form-input" value={current.version} readonly />
            </label>
            <label class="form-label">
              <span>安装路径</span>
              <input class="form-input" value={current.path || '-'} readonly />
            </label>
            {#if current.type === 'node'}
              <label class="form-label">
                <span>可执行文件</span>
                <input class="form-input" value="node/{current.version}/node.exe" readonly />
              </label>
            {:else if current.type === 'python'}
              <label class="form-label">
                <span>可执行文件</span>
                <input class="form-input" value="python/{current.version}/python.exe" readonly />
              </label>
            {/if}
            {#if current.running || current.status === 'running'}
              <label class="form-label">
                <span>PID</span>
                <input class="form-input" value={current.pid || '-'} readonly />
              </label>
              <label class="form-label">
                <span>运行时长</span>
                <input class="form-input" value={current.uptime || '-'} readonly />
              </label>
            {/if}
          </div>
        </div>
      {/if}
    {:else if !loading}
      <div class="empty-state">
        <div class="icon">⚙</div>
        <p>未找到运行环境</p>
        <p style="font-size:12px;margin-top:4px;color:var(--text-muted)">请前往「软件包」页面安装 PHP / Node.js / Python</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .rt-page {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .rt-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
  }

  .list-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .rt-list { flex: 1; padding: 8px; overflow-y: auto; }

  .empty-hint {
    padding: 24px 12px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  .rt-item {
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
  .rt-item:hover { background: var(--bg-inset); }
  .rt-item.active { background: var(--accent-light); }

  .rt-icon { font-size: 18px; }

  .rt-info { flex: 1; min-width: 0; }
  .rt-name { display: block; font-size: 13px; font-weight: 500; color: var(--text-primary); }
  .rt-ver { display: block; font-size: 11px; color: var(--text-muted); }

  .rt-status {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
    background: var(--text-muted);
  }
  .rt-status.running { background: var(--green); }

  .rt-right {
    overflow-y: auto;
    padding: 24px 32px;
  }

  .detail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
  }
  .detail-header h2 {
    font-size: 20px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .detail-icon { font-size: 24px; }

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

  .active-tag {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 3px;
    background: var(--accent-light);
    color: var(--accent);
  }

  .port-tag {
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
