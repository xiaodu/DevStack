<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let services = []
  let sites = []
  let systemInfo = {}
  let loading = true

  async function load() {
    try {
      const [s, si, info] = await Promise.all([api.getServices(), api.getSites(), api.getSystemInfo()])
      services = s
      sites = si
      systemInfo = info
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  async function toggle(id) {
    try {
      const svc = services.find(s => s.id === id)
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

  function statusColor(s) {
    if (s === 'running') return 'green'
    if (s === 'stopped') return 'muted'
    if (s === 'starting' || s === 'stopping') return 'yellow'
    return 'red'
  }

  onMount(load)
</script>

{#if loading}
  <div class="loading">加载中...</div>
{:else}
  <div class="dashboard">
    <div class="top-section">
      <div class="service-cards">
        {#each services as svc}
          <button class="service-card" on:click={() => toggle(svc.id)}>
            <div class="svc-header">
              <span class="svc-name">{svc.name}</span>
              {#if svc.version}
                <span class="svc-ver">v{svc.version}</span>
              {/if}
            </div>
            <div class="svc-body">
              <div class="svc-icon {statusColor(svc.status)}">
                {#if svc.status === 'running'}
                  ✓
                {:else if svc.status === 'stopped'}
                  ■
                {:else if svc.status === 'starting' || svc.status === 'stopping'}
                  ⏳
                {:else}
                  ✕
                {/if}
              </div>
              {#if svc.port}
                <span class="svc-port">:{svc.port}</span>
              {/if}
            </div>
          </button>
        {/each}
      </div>

      <div class="sys-panel">
        <div class="sys-item">
          <span class="sys-label">平台</span>
          <span class="sys-val">{systemInfo.platform || '-'} / {systemInfo.arch || '-'}</span>
        </div>
        <div class="sys-item">
          <span class="sys-label">Go 版本</span>
          <span class="sys-val">{systemInfo.goVersion || '-'}</span>
        </div>
        <div class="sys-item">
          <span class="sys-label">应用版本</span>
          <span class="sys-val">{systemInfo.appVersion || '-'}</span>
        </div>
      </div>
    </div>

    <div class="site-section card">
      <div class="section-header">
        <h3>网站列表</h3>
        <span class="badge badge-info">{sites.length} 个站点</span>
      </div>
      {#if sites.length === 0}
        <div class="empty-state">
          <div class="icon">🌐</div>
          <p>暂无网站</p>
          <p style="font-size:12px;margin-top:4px;">前往「网站」页面添加您的第一个站点</p>
        </div>
      {:else}
        <table class="site-table">
          <thead>
            <tr>
              <th>网站名称</th>
              <th>域名</th>
              <th>根目录</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {#each sites as site}
              <tr>
                <td>{site.name}</td>
                <td><code>{site.domain}</code></td>
                <td class="path">{site.root || site.documentRoot || '-'}</td>
                <td>
                  <span class="badge {site.enabled ? 'badge-success' : 'badge-danger'}">
                    {site.enabled ? '已启用' : '已禁用'}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </div>
{/if}

<style>
  .dashboard { display: flex; flex-direction: column; gap: 20px; }

  .top-section {
    display: grid;
    grid-template-columns: 1fr 260px;
    gap: 16px;
    align-items: start;
  }

  .service-cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 12px;
  }

  .service-card {
    background: linear-gradient(135deg, #1e40af 0%, #2563eb 100%);
    border-radius: var(--radius);
    padding: 16px;
    color: #fff;
    text-align: left;
    transition: transform 0.12s, box-shadow 0.12s;
    cursor: pointer;
    border: none;
  }
  .service-card:hover {
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(37,99,235,0.3);
  }

  .svc-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 14px;
  }
  .svc-name { font-size: 14px; font-weight: 600; }
  .svc-ver { font-size: 11px; opacity: 0.7; }

  .svc-body {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .svc-icon {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 18px;
    font-weight: 700;
  }
  .svc-icon.green { background: #16a34a; }
  .svc-icon.muted { background: rgba(255,255,255,0.2); }
  .svc-icon.yellow { background: #d97706; }
  .svc-icon.red { background: #dc2626; }

  .svc-port { font-size: 12px; opacity: 0.6; font-family: var(--font-mono); }

  .sys-panel {
    background: var(--bg-card);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .sys-item { display: flex; flex-direction: column; gap: 2px; }
  .sys-label { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; }
  .sys-val { font-size: 13px; color: var(--text-primary); font-family: var(--font-mono); }

  .site-section { padding: 0; overflow: hidden; }
  .section-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
  }
  .section-header h3 { font-size: 15px; font-weight: 600; }

  .site-table { width: 100%; border-collapse: collapse; }
  .site-table th {
    text-align: left;
    padding: 10px 20px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border);
    background: var(--bg-inset);
  }
  .site-table td {
    padding: 12px 20px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
  }
  .site-table tr:last-child td { border-bottom: none; }
  .site-table tr:hover td { background: var(--bg-card-hover); }
  .site-table .path { color: var(--text-muted); font-size: 12px; font-family: var(--font-mono); }
  .site-table code { background: var(--bg-input); padding: 2px 6px; border-radius: 4px; font-size: 12px; }

  .loading { color: var(--text-muted); padding: 40px; text-align: center; }
</style>
