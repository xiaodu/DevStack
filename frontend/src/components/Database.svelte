<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let services = []
  let selectedId = null
  let loading = true
  let mysqlRootHasPassword = null
  let checkingAuth = false
  let mysqlConfig = {}
  let configLoading = false

  const dbTypes = [
    { id: 'mysql', label: 'MySQL', icon: '🐬' },
    { id: 'redis', label: 'Redis', icon: '🔴' },
  ]

  const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
  const innodbLogOptions = ['32M', '64M', '128M', '256M', '512M']

  const recommendedMySQLConfig = {
    innodbBufferPoolSize: '256M',
    innodbLogFileSize: '64M',
    innodbFlushLogAtTrxCommit: '2',
    maxConnections: '50',
    tmpTableSize: '64M',
    maxHeapTableSize: '64M',
    threadCacheSize: '8',
    tableOpenCache: '400',
    bindAddress: '127.0.0.1',
    port: '3306',
  }

  async function load() {
    try {
      const all = await api.getServices()
      services = all.filter(s => s.category === 'database' && s.installed)
      if (!selectedId && services.length > 0) {
        selectDb(services[0].id)
      }
      await checkMysqlAuth()
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function checkMysqlAuth() {
    const mysql = services.find(s => s.id === 'mysql')
    if (!mysql || mysql.status !== 'running') {
      mysqlRootHasPassword = null
      return
    }
    checkingAuth = true
    try {
      mysqlRootHasPassword = await api.checkMySQLRootAuth()
    } catch (e) {
      console.error(e)
      mysqlRootHasPassword = null
    } finally {
      checkingAuth = false
    }
  }

  function selectDb(id) {
    selectedId = id
    if (id === 'mysql') {
      checkMysqlAuth()
      loadMySQLConfig()
    }
  }

  async function loadMySQLConfig() {
    configLoading = true
    try {
      mysqlConfig = await api.getServiceConfig('mysql')
    } catch (e) {
      console.error(e)
    } finally {
      configLoading = false
    }
  }

  function getRecommendedHint(key, label) {
    const val = recommendedMySQLConfig[key]
    if (!val) return ''
    return `推荐值: ${val}${label ? ` · ${label}` : ''}`
  }

  function loadRecommendedMySQL() {
    mysqlConfig = { ...recommendedMySQLConfig }
    addToast('已加载推荐配置')
  }

  function intVal(key) {
    return parseInt(mysqlConfig[key]) || 0
  }

  async function saveConfig(svc) {
    const vals = {}
    if (svc.id === 'mysql') {
      vals['innodbBufferPoolSize'] = mysqlConfig.innodbBufferPoolSize || '256M'
      vals['innodbLogFileSize'] = mysqlConfig.innodbLogFileSize || '64M'
      vals['innodbFlushLogAtTrxCommit'] = String(intVal('innodbFlushLogAtTrxCommit'))
      vals['maxConnections'] = String(intVal('maxConnections'))
      vals['tmpTableSize'] = mysqlConfig.tmpTableSize || '64M'
      vals['maxHeapTableSize'] = mysqlConfig.maxHeapTableSize || '64M'
      vals['threadCacheSize'] = String(intVal('threadCacheSize'))
      vals['tableOpenCache'] = String(intVal('tableOpenCache'))
      vals['bindAddress'] = mysqlConfig.bindAddress || '127.0.0.1'
      vals['port'] = String(intVal('port'))

      const password = document.getElementById('mysql-password')
      if (password && password.value) vals['password'] = password.value
    } else if (svc.id === 'redis') {
      const bindIp = document.getElementById('redis-bind')
      const port = document.getElementById('redis-port')
      if (bindIp) vals['bind'] = bindIp.value
      if (port) vals['port'] = port.value
    }
    try {
      await api.saveServiceConfig(svc.id, vals)
      addToast('保存成功')
      if (svc.id === 'mysql') {
        await checkMysqlAuth()
        await loadMySQLConfig()
      }
    } catch (e) {
      addToast('保存失败: ' + (e.message || e), 'error')
    }
  }

  function getDb(id) { return services.find(s => s.id === id) }

  async function toggle(id) {
    try {
      const svc = getDb(id)
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

  $: current = selectedId ? getDb(selectedId) : null

  onMount(load)
</script>

<div class="db-page">
  <div class="db-left">
    <div class="list-header">
      <h3>数据库</h3>
    </div>
    <div class="db-list">
      {#each dbTypes as dbType}
        {#each services.filter(s => s.id === dbType.id) as svc}
          <button
            class="db-item"
            class:active={selectedId === svc.id}
            on:click={() => selectDb(svc.id)}
          >
            <span class="db-icon">{dbType.icon}</span>
            <div class="db-info">
              <span class="db-name">{svc.name}</span>
              <span class="db-ver">v{svc.version}</span>
            </div>
            <span class="db-status {svc.status}"></span>
          </button>
        {/each}
      {/each}
    </div>
  </div>

  <div class="db-right">
    {#if current}
      <div class="detail-header">
        <div>
          <h2>{current.name} {current.version}</h2>
        </div>
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

      {#if current.id === 'mysql'}
        {#if configLoading}
          <div class="loading-hint">加载配置...</div>
        {:else}
          <div class="section">
            <div class="auth-status">
              {#if current.status !== 'running'}
                <span class="status-text muted">MySQL 未运行</span>
              {:else if checkingAuth}
                <span class="status-text loading">检查中...</span>
              {:else if mysqlRootHasPassword === true}
                <span class="status-text has-password">root 已设置密码</span>
              {:else if mysqlRootHasPassword === false}
                <span class="status-text no-password">root 无密码（初始状态）</span>
              {:else}
                <span class="status-text muted">无法检测</span>
              {/if}
            </div>
          </div>

          <div class="section">
            <h3>连接设置</h3>
            <div class="config-grid">
              <div class="config-row">
                <label class="form-label">
                  <span>账号</span>
                  <input class="form-input" value="root" readonly />
                </label>
                <label class="form-label">
                  <span>密码</span>
                  <input id="mysql-password" class="form-input" type="password" placeholder="留空则不修改" />
                </label>
              </div>
              <div class="config-row">
                <label class="form-label">
                  <span>绑定 IP</span>
                  <input class="form-input" bind:value={mysqlConfig.bindAddress} />
                  <span class="hint">{getRecommendedHint('bindAddress', '仅限本地')}</span>
                </label>
                <label class="form-label">
                  <span>端口</span>
                  <input class="form-input" type="number" min="1" max="65535"
                    value={intVal('port')}
                    on:change={e => mysqlConfig.port = String(e.target.value)} />
                  <span class="hint">{getRecommendedHint('port')}</span>
                </label>
              </div>
            </div>
          </div>

          <div class="section">
            <div class="section-header">
              <h3>InnoDB 优化</h3>
              <button class="btn btn-sm btn-outline" on:click={loadRecommendedMySQL}>加载推荐值</button>
            </div>
            <div class="config-grid">
              <div class="config-row">
                <label class="form-label">
                  <span>InnoDB 缓冲池大小</span>
                  <select class="form-input" bind:value={mysqlConfig.innodbBufferPoolSize}>
                    {#each memoryOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('innodbBufferPoolSize', '建议内存的50-70%')}</span>
                </label>
                <label class="form-label">
                  <span>InnoDB 日志文件大小</span>
                  <select class="form-input" bind:value={mysqlConfig.innodbLogFileSize}>
                    {#each innodbLogOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('innodbLogFileSize')}</span>
                </label>
              </div>
              <div class="config-row">
                <label class="form-label">
                  <span>InnoDB 刷新日志策略</span>
                  <select class="form-input" bind:value={mysqlConfig.innodbFlushLogAtTrxCommit}>
                    <option value="1">1 - 每次提交刷新（最安全）</option>
                    <option value="2">2 - 每秒刷新（平衡）</option>
                    <option value="0">0 - 不主动刷新（最快）</option>
                  </select>
                  <span class="hint">{getRecommendedHint('innodbFlushLogAtTrxCommit', '2为推荐值')}</span>
                </label>
              </div>
            </div>
          </div>

          <div class="section">
            <h3>连接与缓存</h3>
            <div class="config-grid">
              <div class="config-row">
                <label class="form-label">
                  <span>最大连接数</span>
                  <input class="form-input" type="number" min="10" max="1000"
                    value={intVal('maxConnections')}
                    on:change={e => mysqlConfig.maxConnections = String(e.target.value)} />
                  <span class="hint">{getRecommendedHint('maxConnections')}</span>
                </label>
                <label class="form-label">
                  <span>临时表大小</span>
                  <select class="form-input" bind:value={mysqlConfig.tmpTableSize}>
                    {#each memoryOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('tmpTableSize')}</span>
                </label>
              </div>
              <div class="config-row">
                <label class="form-label">
                  <span>最大堆表大小</span>
                  <select class="form-input" bind:value={mysqlConfig.maxHeapTableSize}>
                    {#each memoryOptions as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                  <span class="hint">{getRecommendedHint('maxHeapTableSize', '与tmp一致')}</span>
                </label>
                <label class="form-label">
                  <span>线程缓存大小</span>
                  <input class="form-input" type="number" min="1" max="100"
                    value={intVal('threadCacheSize')}
                    on:change={e => mysqlConfig.threadCacheSize = String(e.target.value)} />
                  <span class="hint">{getRecommendedHint('threadCacheSize')}</span>
                </label>
              </div>
              <div class="config-row">
                <label class="form-label">
                  <span>表打开缓存</span>
                  <input class="form-input" type="number" min="100" max="4000"
                    value={intVal('tableOpenCache')}
                    on:change={e => mysqlConfig.tableOpenCache = String(e.target.value)} />
                  <span class="hint">{getRecommendedHint('tableOpenCache')}</span>
                </label>
              </div>
            </div>
          </div>

          <div class="section">
            <h3>文件信息</h3>
            <div class="config-grid">
              <label class="form-label">
                <span>数据目录</span>
                <input class="form-input" value="db/mysql/" readonly />
              </label>
              <label class="form-label">
                <span>配置文件</span>
                <input class="form-input" value="mysql/{current.version}/my.ini" readonly />
              </label>
            </div>
          </div>
        {/if}

      {:else if current.id === 'redis'}
        <div class="section">
          <h3>连接设置</h3>
          <div class="config-grid">
            <div class="config-row">
              <label class="form-label">
                <span>绑定 IP</span>
                <input id="redis-bind" class="form-input" value="127.0.0.1" />
              </label>
              <label class="form-label">
                <span>端口</span>
                <input id="redis-port" class="form-input" type="number" value="6379" />
              </label>
            </div>
          </div>
        </div>
        <div class="section">
          <h3>文件信息</h3>
          <div class="config-grid">
            <label class="form-label">
              <span>数据目录</span>
              <input class="form-input" value="data/redis" readonly />
            </label>
          </div>
        </div>
      {/if}

      <div class="actions">
        <button class="btn btn-primary" on:click={() => saveConfig(current)}>保存设置</button>
        <span class="action-hint">MySQL 配置修改后需重启生效</span>
      </div>
    {:else}
      <div class="empty-state">
        <div class="icon">🗄</div>
        <p>选择左侧数据库进行配置</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .db-page {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .db-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
  }

  .list-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .db-list { flex: 1; padding: 8px; overflow-y: auto; }

  .db-item {
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
  .db-item:hover { background: var(--bg-inset); }
  .db-item.active { background: var(--accent-light); }

  .db-icon { font-size: 20px; }

  .db-info { flex: 1; min-width: 0; }
  .db-name { display: block; font-size: 13px; font-weight: 500; color: var(--text-primary); }
  .db-ver { display: block; font-size: 11px; color: var(--text-muted); }

  .db-status {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .db-status.running { background: var(--green); }
  .db-status.stopped { background: var(--text-muted); }
  .db-status.error { background: var(--red); }

  .db-right {
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
  .header-actions { display: flex; gap: 8px; }

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
    max-width: 700px;
  }

  .config-row {
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

  .auth-status {
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
  }

  .status-text.muted { color: var(--text-muted); }
  .status-text.loading { color: var(--text-muted); }
  .status-text.has-password { color: var(--green); }
  .status-text.no-password { color: var(--orange); }

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
