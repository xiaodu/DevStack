<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let services = []
  let selectedId = null
  let loading = true
  let mysqlRootHasPassword = null
  let checkingAuth = false

  const dbTypes = [
    { id: 'mysql', label: 'MySQL', icon: '🐬' },
    { id: 'redis', label: 'Redis', icon: '🔴' },
  ]

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
    }
  }

  async function saveConfig(svc) {
    const vals = {}
    if (svc.id === 'mysql') {
      const bindIp = document.getElementById('mysql-bind')
      const port = document.getElementById('mysql-port')
      const password = document.getElementById('mysql-password')
      if (bindIp) vals['bind-address'] = bindIp.value
      if (port) vals['port'] = port.value
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
          <h2>{current.name} {current.version} 设置</h2>
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

      <div class="config-grid">
        {#if current.id === 'mysql'}
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
              <input id="mysql-bind" class="form-input" value="127.0.0.1" />
            </label>
            <label class="form-label">
              <span>端口</span>
              <input id="mysql-port" class="form-input" type="number" value="3306" />
            </label>
          </div>

          <label class="form-label">
            <span>数据目录</span>
            <input class="form-input" value="data/mysql/{current.version}" readonly />
          </label>

          <div class="config-row">
            <label class="form-label">
              <span>Key Buffer Size</span>
              <select class="form-input">
                <option>256M</option>
                <option>512M</option>
                <option selected>1024M</option>
              </select>
            </label>
            <label class="form-label">
              <span>Sort Buffer Size</span>
              <select class="form-input">
                <option selected>16M</option>
                <option>32M</option>
                <option>64M</option>
              </select>
            </label>
          </div>

          <div class="config-row">
            <label class="form-label">
              <span>Read Buffer Size</span>
              <select class="form-input">
                <option selected>16M</option>
                <option>32M</option>
              </select>
            </label>
            <label class="form-label">
              <span>Max Allowed Packet</span>
              <select class="form-input">
                <option>256M</option>
                <option selected>1024M</option>
              </select>
            </label>
          </div>

        {:else if current.id === 'redis'}
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

          <div class="config-row">
            <label class="form-label">
              <span>最大内存</span>
              <select class="form-input">
                <option>256M</option>
                <option selected>512M</option>
                <option>1G</option>
                <option>2G</option>
              </select>
            </label>
            <label class="form-label">
              <span>持久化</span>
              <select class="form-input">
                <option value="rdb">RDB</option>
                <option value="aof">AOF</option>
                <option value="both">RDB + AOF</option>
                <option value="none">关闭</option>
              </select>
            </label>
          </div>

          <label class="form-label">
            <span>数据目录</span>
            <input class="form-input" value="data/redis" readonly />
          </label>

          <label class="form-label">
            <span>密码</span>
            <input class="form-input" type="password" placeholder="留空则无密码" />
          </label>
        {/if}
      </div>

      <div class="actions">
        <button class="btn btn-primary" on:click={() => saveConfig(current)}>保存设置</button>
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
    margin-bottom: 24px;
  }
  .detail-header h2 { font-size: 20px; font-weight: 600; }
  .header-actions { display: flex; gap: 8px; }

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

  .actions {
    margin-top: 24px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }

  .auth-status {
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    margin-bottom: 8px;
  }

  .status-text {
    font-size: 13px;
    font-weight: 500;
  }

  .status-text.muted { color: var(--text-muted); }
  .status-text.loading { color: var(--text-muted); }
  .status-text.has-password { color: var(--green); }
  .status-text.no-password { color: var(--orange); }
</style>
