<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let categories = [
    { id: 'services', label: '基础服务', icon: '⚙' },
    { id: 'webserver', label: 'Web 服务器', icon: '🌐' },
    { id: 'tools', label: '工具', icon: '🔧' },
    { id: 'languages', label: '语言', icon: '💻' },
  ]

  let selectedCategory = 'tools'

  let allPackages = [
    { id: 'phpmyadmin', name: 'phpMyAdmin', version: '5.2.3', category: 'tools', installed: false, type: 'tool' },
    { id: 'adminer', name: 'Adminer', version: '5.4.2', category: 'tools', installed: false, type: 'tool' },
    { id: 'git', name: 'Git', version: '2.54.0', category: 'tools', installed: false, type: 'tool' },
    { id: 'composer', name: 'Composer 2.2 LTS', version: '2.2.27', category: 'tools', installed: false, type: 'tool' },
    { id: 'composer2', name: 'Composer', version: '2.9.7', category: 'tools', installed: false, type: 'tool' },
    { id: 'nginx', name: 'Nginx', version: '1.26.0', category: 'webserver', installed: false, type: 'service' },
    { id: 'caddy', name: 'Caddy', version: '2.9.1', category: 'webserver', installed: false, type: 'service' },
    { id: 'mysql', name: 'MySQL', version: '8.4', category: 'services', installed: false, type: 'service' },
    { id: 'mariadb', name: 'MariaDB', version: '11.4', category: 'services', installed: false, type: 'service' },
    { id: 'postgresql', name: 'PostgreSQL', version: '16', category: 'services', installed: false, type: 'service' },
    { id: 'redis', name: 'Redis', version: '7.4.0', category: 'services', installed: false, type: 'service' },
    { id: 'mongodb', name: 'MongoDB', version: '7.0', category: 'services', installed: false, type: 'service' },
    { id: 'memcached', name: 'Memcached', version: '1.6.32', category: 'services', installed: false, type: 'service' },
    { id: 'php', name: 'PHP', version: '8.4', category: 'languages', installed: false, type: 'language' },
    { id: 'php83', name: 'PHP', version: '8.3', category: 'languages', installed: false, type: 'language' },
    { id: 'php82', name: 'PHP', version: '8.2', category: 'languages', installed: false, type: 'language' },
    { id: 'node', name: 'Node.js', version: '22', category: 'languages', installed: false, type: 'language' },
    { id: 'node20', name: 'Node.js', version: '20', category: 'languages', installed: false, type: 'language' },
    { id: 'node18', name: 'Node.js', version: '18', category: 'languages', installed: false, type: 'language' },
    { id: 'bun', name: 'Bun', version: '1.3', category: 'languages', installed: false, type: 'language' },
    { id: 'deno', name: 'Deno', version: '2.1', category: 'languages', installed: false, type: 'language' },
    { id: 'python', name: 'Python', version: '3.13', category: 'languages', installed: false, type: 'language' },
    { id: 'python312', name: 'Python', version: '3.12', category: 'languages', installed: false, type: 'language' },
    { id: 'go', name: 'Go', version: '1.24', category: 'languages', installed: false, type: 'language' },
    { id: 'java', name: 'OpenJDK', version: '21', category: 'languages', installed: false, type: 'language' },
    { id: 'ruby', name: 'Ruby', version: '3.4', category: 'languages', installed: false, type: 'language' },
  ]

  let installing = {}

  async function load() {
    try {
      const services = await api.getServices()
      for (const svc of services) {
        const pkg = allPackages.find(p => p.id === svc.id)
        if (pkg) {
          pkg.installed = true
          pkg.running = svc.status === 'running'
          pkg.pid = svc.pid || null
        }
      }
      allPackages = [...allPackages]
    } catch (e) {
      console.error(e)
    }
  }

  async function install(pkg) {
    installing[pkg.id] = true
    allPackages = [...allPackages]
    try {
      await api.downloadService(pkg.id, pkg.version)
      pkg.installed = true
    } catch (e) {
      alert(`安装失败: ${e.message || e}`)
    } finally {
      installing[pkg.id] = false
      allPackages = [...allPackages]
    }
  }

  async function uninstall(pkg) {
    if (!confirm(`确定卸载 ${pkg.name} ${pkg.version}？`)) return
    pkg.installed = false
    allPackages = [...allPackages]
  }

  async function toggleService(pkg) {
    try {
      if (pkg.running) {
        await api.stopService(pkg.id)
        pkg.running = false
      } else {
        await api.startService(pkg.id)
        pkg.running = true
      }
      allPackages = [...allPackages]
    } catch (e) {
      alert(e.message || e)
    }
  }

  $: filteredPackages = allPackages.filter(p => p.category === selectedCategory)

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
          class:active={selectedCategory === cat.id}
          on:click={() => selectedCategory = cat.id}
        >
          <span class="cat-icon">{cat.icon}</span>
          <span>{cat.label}</span>
        </button>
      {/each}
    </div>
  </div>

  <div class="pkg-right">
    <div class="detail-header">
      <h2>{categories.find(c => c.id === selectedCategory)?.label || ''}</h2>
    </div>

    <table class="pkg-table">
      <thead>
        <tr>
          <th>包名称</th>
          <th>版本</th>
          <th>状态</th>
          <th>PID</th>
          <th>控制</th>
        </tr>
      </thead>
      <tbody>
        {#each filteredPackages as pkg}
          <tr>
            <td class="pkg-name">{pkg.name}</td>
            <td class="pkg-ver">{pkg.version}</td>
            <td>
              {#if pkg.installed}
                <span class="badge badge-success">Installed</span>
              {:else if installing[pkg.id]}
                <span class="badge badge-warning">安装中...</span>
              {:else}
                <span class="badge badge-muted">Not Installed</span>
              {/if}
            </td>
            <td class="pkg-pid">{pkg.pid || '-'}</td>
            <td class="pkg-actions">
              {#if pkg.installed}
                {#if pkg.type === 'service' || pkg.type === 'language'}
                  <button
                    class="ctrl-btn {pkg.running ? 'stop' : 'start'}"
                    on:click={() => toggleService(pkg)}
                    title={pkg.running ? '停止' : '启动'}
                  >
                    {pkg.running ? '⏹' : '▶'}
                  </button>
                {/if}
                <button class="ctrl-btn uninstall" on:click={() => uninstall(pkg)} title="卸载">
                  🗑
                </button>
              {:else if installing[pkg.id]}
                <span class="spinner-sm"></span>
              {:else}
                <button class="ctrl-btn install" on:click={() => install(pkg)} title="安装">
                  ⬇
                </button>
              {/if}
            </td>
          </tr>
        {/each}
        {#if filteredPackages.length === 0}
          <tr>
            <td colspan="5" class="empty-row">暂无软件包</td>
          </tr>
        {/if}
      </tbody>
    </table>
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
  .detail-header h2 { font-size: 18px; font-weight: 600; display: flex; align-items: center; gap: 8px; }

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
  .pkg-table tr:last-child td { border-bottom: none; }

  .pkg-name { font-weight: 500; }
  .pkg-ver { font-family: var(--font-mono); color: var(--text-secondary); }
  .pkg-pid { font-family: var(--font-mono); font-size: 12px; color: var(--text-muted); }

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
  .ctrl-btn.start { background: var(--green-bg); color: var(--green); }
  .ctrl-btn.stop { background: var(--yellow-bg); color: var(--yellow); }
  .ctrl-btn.install { background: #dbeafe; color: #2563eb; }
  .ctrl-btn.uninstall { background: var(--red-bg); color: var(--red); }

  .spinner-sm {
    width: 18px; height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .empty-row {
    text-align: center;
    color: var(--text-muted);
    padding: 40px 24px !important;
  }
</style>
