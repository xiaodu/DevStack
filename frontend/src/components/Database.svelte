<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let services = []
  let selectedId = null
  let loading = true

  const dbTypes = [
    { id: 'mysql', label: 'MySQL', icon: '🐬' },
    { id: 'redis', label: 'Redis', icon: '🔴' },
  ]

  async function load() {
    try {
      const all = await api.getServices()
      services = all.filter(s => s.type === 'database' || s.id === 'mysql' || s.id === 'redis')
      if (!selectedId && services.length > 0) {
        selectDb(services[0].id)
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  function selectDb(id) { selectedId = id }

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
      alert(e.message || e)
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
        {#each services.filter(s => s.id === dbType.id || s.type === dbType.id) as svc}
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
          <div class="config-row">
            <label class="form-label">
              <span>账号</span>
              <input class="form-input" value="root" readonly />
            </label>
            <label class="form-label">
              <span>密码</span>
              <input class="form-input" type="password" value="root" />
            </label>
          </div>

          <div class="config-row">
            <label class="form-label">
              <span>绑定 IP</span>
              <input class="form-input" value="127.0.0.1" />
            </label>
            <label class="form-label">
              <span>端口</span>
              <input class="form-input" type="number" value="3306" />
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
              <input class="form-input" value="127.0.0.1" />
            </label>
            <label class="form-label">
              <span>端口</span>
              <input class="form-input" type="number" value="6379" />
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
        <button class="btn btn-primary" on:click={() => alert('保存功能开发中')}>保存设置</button>
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
</style>
