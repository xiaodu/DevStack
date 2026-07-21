<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let packages = []
  let selected = 'all'
  let installing = {}
  let uninstalling = {}
  let loading = true

  const categories = [
    { id: 'all', label: '全部', icon: '📦' },
    { id: 'webserver', label: 'Web 服务器', icon: '🌐' },
    { id: 'database', label: '数据库', icon: '🗄️' },
    { id: 'runtime', label: '运行环境', icon: '💻' },
    { id: 'service', label: '基础服务', icon: '⚙' },
  ]

  const serviceNames = {
    nginx: 'Nginx', apache: 'Apache',
    mysql: 'MySQL', postgresql: 'PostgreSQL', mongodb: 'MongoDB',
    redis: 'Redis', memcached: 'Memcached',
    php: 'PHP', node: 'Node.js', python: 'Python', composer: 'Composer',
  }
  async function load() {
    loading = true
    packages = []
    try {
      const services = await api.getServices()
      for (const svc of services) {
        const releases = await api.getAvailableVersions(svc.id) || []
        for (const rel of releases) {
          const installed = await api.checkVersionInstalled(svc.id, rel.version)
          packages.push({
            id: svc.id,
            name: serviceNames[svc.id] || svc.name,
            version: rel.version,
            category: svc.category,
            installed,
            isCurrent: svc.version === rel.version && svc.installed,
          })
        }
      }
      packages = [...packages]
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function install(pkg) {
    const key = `${pkg.id}-${pkg.version}`
    installing[key] = true
    packages = [...packages]
    try {
      await api.downloadService(pkg.id, pkg.version)
      await load()
      addToast(`${pkg.name} ${pkg.version} 安装成功`)
    } catch (e) {
      addToast(`安装失败: ${e.message || e}`, 'error')
    } finally {
      installing[key] = false
      packages = [...packages]
    }
  }

  async function uninstall(pkg) {
    if (!confirm(`确定卸载 ${pkg.name} ${pkg.version}？`)) return
    const key = `${pkg.id}-${pkg.version}`
    uninstalling[key] = true
    packages = [...packages]
    try {
      await api.uninstallVersion(pkg.id, pkg.version)
      await load()
      addToast(`${pkg.name} ${pkg.version} 卸载成功`)
    } catch (e) {
      addToast(`卸载失败: ${e.message || e}`, 'error')
    } finally {
      uninstalling[key] = false
      packages = [...packages]
    }
  }

  $: filtered = selected === 'all'
    ? packages
    : packages.filter(p => p.category === selected)

  onMount(load)
</script>

<div class="pkg-page">
  <div class="pkg-left">
    <div class="list-header">
      <h3>软件包</h3>
    </div>
    <div class="cat-list">
      {#each categories as cat}
        <button
          class="cat-item"
          class:active={selected === cat.id}
          on:click={() => selected = cat.id}
        >
          <span class="cat-icon">{cat.icon}</span>
          <span>{cat.label}</span>
        </button>
      {/each}
    </div>
  </div>

  <div class="pkg-right">
    <div class="detail-header">
      <h2>{categories.find(c => c.id === selected)?.label || ''}</h2>
    </div>

    {#if loading}
      <div class="loading">加载中...</div>
    {:else if filtered.length === 0}
      <div class="empty-row">暂无软件包</div>
    {:else}
      <table class="pkg-table">
        <thead>
          <tr>
            <th>包名称</th>
            <th>版本</th>
            <th>状态</th>
            <th>控制</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as pkg}
            {@const key = `${pkg.id}-${pkg.version}`}
            <tr class:current={pkg.isCurrent}>
              <td class="pkg-name">{pkg.name}</td>
              <td class="pkg-ver">
                {pkg.version}
                {#if pkg.isCurrent}
                  <span class="current-tag">当前使用</span>
                {/if}
              </td>
              <td>
                {#if pkg.installed}
                  <span class="badge badge-success">已安装</span>
                {:else if installing[key]}
                  <span class="badge badge-warning">安装中...</span>
                {:else if uninstalling[key]}
                  <span class="badge badge-warning">卸载中...</span>
                {:else}
                  <span class="badge badge-muted">未安装</span>
                {/if}
              </td>
              <td class="pkg-actions">
                {#if pkg.installed}
                  {#if uninstalling[key]}
                    <span class="spinner-sm"></span>
                  {:else}
                    <button class="ctrl-btn uninstall" on:click={() => uninstall(pkg)} title="卸载">
                      🗑
                    </button>
                  {/if}
                {:else if installing[key]}
                  <span class="spinner-sm"></span>
                {:else}
                  <button class="ctrl-btn install" on:click={() => install(pkg)} title="安装">
                    ⬇
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

<style>
  .pkg-page {
    display: grid;
    grid-template-columns: 220px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .pkg-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
  }

  .list-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .cat-list { flex: 1; padding: 8px; overflow-y: auto; }

  .cat-item {
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
    font-size: 13.5px;
    color: var(--text-secondary);
  }
  .cat-item:hover { background: var(--bg-inset); color: var(--text-primary); }
  .cat-item.active { background: var(--accent); color: #fff; font-weight: 500; }

  .cat-icon { font-size: 16px; width: 20px; text-align: center; }

  .pkg-right {
    overflow-y: auto;
    padding: 0;
  }

  .detail-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
  }
  .detail-header h2 { font-size: 18px; font-weight: 600; }

  .pkg-table {
    width: 100%;
    border-collapse: collapse;
  }
  .pkg-table th {
    text-align: left;
    padding: 10px 24px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border);
    background: var(--bg-inset);
  }
  .pkg-table td {
    padding: 12px 24px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
  }
  .pkg-table tr:hover td { background: var(--bg-card-hover); }

  .pkg-name { font-weight: 500; }
  .pkg-ver { font-family: var(--font-mono); color: var(--text-secondary); display: flex; align-items: center; gap: 8px; }

  .current-tag {
    font-size: 10px;
    background: var(--accent-light);
    color: var(--accent);
    padding: 1px 6px;
    border-radius: 3px;
    font-family: var(--font);
  }

  .badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 10px;
    border-radius: 20px;
    font-size: 12px;
    font-weight: 500;
  }
  .badge-success { background: var(--green-bg); color: var(--green); }
  .badge-warning { background: var(--yellow-bg); color: var(--yellow); }
  .badge-muted { background: var(--bg-inset); color: var(--text-muted); }

  .pkg-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .ctrl-btn {
    width: 30px; height: 30px;
    border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 13px;
    transition: transform 0.1s, opacity 0.1s;
    border: none;
  }
  .ctrl-btn:hover { transform: scale(1.1); }
  .ctrl-btn.install { background: #dbeafe; color: var(--accent); }
  .ctrl-btn.uninstall { background: var(--red-bg); color: var(--red); }

  .spinner-sm {
    width: 18px; height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .loading { color: var(--text-muted); padding: 40px; text-align: center; }
  .empty-row {
    text-align: center;
    color: var(--text-muted);
    padding: 40px 24px !important;
  }
</style>
