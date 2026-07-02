<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { refreshSignal, globalLoading } from '../lib/stores.js'

  let services = []
  let sites = []
  let systemInfo = {}
  let hasLoaded = false

  const cardMeta = {
    nginx:  { cls: 'icon-nginx',  color: '#009639' },
    mysql:  { cls: 'icon-mysql',  color: '#4479A1' },
    redis:  { cls: 'icon-redis',  color: '#DC382D' },
    php:    { cls: 'icon-php',    color: '#777BB4' },
    node:   { cls: 'icon-Nodejs', color: '#339933' },
    python: { cls: 'icon-python', color: '#3776AB' },
  }

  async function load() {
    globalLoading.set(true)
    try {
      const [s, si, info] = await Promise.all([api.getServices(), api.getSites(), api.getSystemInfo()])
      services = s
      sites = si
      systemInfo = info
      hasLoaded = true
    } catch (e) {
      console.error(e)
    } finally {
      globalLoading.set(false)
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
      console.error(e)
    }
  }

  function statusLabel(s) {
    if (s === 'running') return '运行中'
    if (s === 'stopped') return '已停止'
    if (s === 'starting') return '启动中'
    if (s === 'stopping') return '停止中'
    return '错误'
  }

  const unsub = refreshSignal.subscribe(() => load())
  onMount(() => { load(); return unsub })
</script>

<div class="dashboard">
  <div class="service-cards">
    {#each (hasLoaded ? services : [null,null,null,null,null,null]) as _, i}
      {@const svc = hasLoaded ? services[i] : null}
      {@const meta = svc ? (cardMeta[svc.id] || { cls: 'icon-nginx', color: 'var(--accent)' }) : { cls: '', color: 'var(--border)' }}
      <button
        class="service-card"
        class:running={svc?.status === 'running'}
        class:placeholder={!svc}
        style={svc ? '--card-color: ' + meta.color + ';' : ''}
        on:click={() => svc && toggle(svc.id)}
        disabled={!svc || !svc.installed}
      >
        <div class="svc-accent" style="background: {meta.color};"></div>
        <div class="svc-body">
          <div class="svc-icon">
            {#if svc}
              <span class="iconfont {meta.cls}" style="color: {meta.color};"></span>
            {:else}
              <span class="icon-placeholder"></span>
            {/if}
          </div>
          <div class="svc-info">
            {#if svc}
              <div class="svc-top">
                <div class="svc-name">{svc.name}</div>
                {#if svc.installed}
                  <span class="svc-ver">v{svc.version}</span>
                {/if}
              </div>
              <div class="svc-meta">
                {#if svc.installed}
                  <span class="svc-dot" class:running={svc.status === 'running'}></span>
                  <span class="svc-status">{statusLabel(svc.status)}</span>
                {:else}
                  <span class="svc-dot"></span>
                  <span class="svc-status">未安装</span>
                {/if}
              </div>
            {:else}
              <div class="svc-top"><div class="placeholder-line w-60"></div></div>
              <div class="svc-meta"><div class="placeholder-line w-40"></div></div>
            {/if}
          </div>
        </div>
      </button>
    {/each}
  </div>

  <div class="site-section card">
    <div class="section-header">
      <h3>网站列表</h3>
      <span class="badge badge-info">{sites.length} 个站点</span>
    </div>
    {#if !hasLoaded}
      <div class="empty-state"><p style="color:var(--text-muted)">加载中...</p></div>
    {:else if sites.length === 0}
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

  <div class="info-section card">
    <div class="section-header">
      <h3>系统信息</h3>
    </div>
    <div class="info-grid">
      <div class="info-col">
        <div class="info-item">
          <span class="info-label">平台</span>
          <span class="info-val">{systemInfo.platform || '-'} / {systemInfo.arch || '-'}</span>
        </div>
        <div class="info-item">
          <span class="info-label">Go 版本</span>
          <span class="info-val">{systemInfo.goVersion || '-'}</span>
        </div>
        <div class="info-item">
          <span class="info-label">应用版本</span>
          <span class="info-val">{systemInfo.appVersion || '-'}</span>
        </div>
      </div>
      <div class="info-col">
        {#if !hasLoaded}
          {#each [1,2,3] as _}
            <div class="info-item"><span class="info-label">-</span><span class="info-val">-</span></div>
          {/each}
        {:else}
          {#each services as svc}
            {#if svc.installed}
              <div class="info-item">
                <span class="info-label" style="color: {cardMeta[svc.id]?.color || 'var(--text-primary)'};">{svc.name}</span>
                <span class="info-val">
                  {#if svc.status === 'running'}
                    PID: {svc.pid || '-'}　{svc.uptime || ''}
                  {:else}
                    {statusLabel(svc.status)}
                  {/if}
                </span>
              </div>
            {/if}
          {/each}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .dashboard { display: flex; flex-direction: column; gap: 20px; }

  .service-cards {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    min-height: 120px;
  }

  .service-card {
    position: relative;
    background: var(--bg-card);
    border-radius: var(--radius);
    border: 2px solid var(--border);
    text-align: left;
    cursor: pointer;
    padding: 0;
    overflow: hidden;
    outline: none;
    -webkit-tap-highlight-color: transparent;
    width: 100%;
    box-sizing: border-box;
  }
  .service-card:hover {
    box-shadow: var(--shadow-lg);
  }
  .service-card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .service-card.running {
    border-color: var(--green);
    box-shadow: 0 0 0 1px var(--green), var(--shadow-sm);
  }
  .service-card:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .service-card:disabled:hover {
    box-shadow: var(--shadow-sm);
  }
  .service-card:disabled.running {
    border-color: var(--border);
    box-shadow: var(--shadow-sm);
  }
  .service-card.placeholder {
    border-color: var(--border);
    border-style: dashed;
  }

  .svc-accent {
    height: 4px;
    width: 100%;
  }

  .svc-body {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 18px 18px 16px;
  }

  .svc-icon {
    flex-shrink: 0;
    width: 44px;
    height: 44px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 12px;
    background: var(--bg-inset);
  }
  .svc-icon .iconfont { font-size: 28px; }
  .icon-placeholder { width: 28px; height: 28px; border-radius: 6px; background: var(--border); }

  .svc-info { flex: 1; min-width: 0; }

  .svc-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    margin-bottom: 4px;
  }

  .svc-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }

  .svc-ver {
    flex-shrink: 0;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-secondary);
    background: var(--bg-inset);
    padding: 1px 6px;
    border-radius: 4px;
  }

  .svc-meta { display: flex; align-items: center; gap: 4px; }

  .svc-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-muted);
    flex-shrink: 0;
  }
  .svc-dot.running {
    background: var(--green);
    box-shadow: 0 0 6px rgba(5,150,105,0.4);
  }

  .svc-status { font-size: 11px; color: var(--text-muted); }

  .placeholder-line {
    height: 10px;
    border-radius: 4px;
    background: var(--border);
  }
  .w-60 { width: 60%; }
  .w-40 { width: 40%; }

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

  .info-section { padding: 0; overflow: hidden; }

  .info-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0;
  }
  .info-col {
    padding: 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .info-col:first-child { border-right: 1px solid var(--border); }

  .info-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .info-label { font-size: 12px; color: var(--text-muted); white-space: nowrap; }
  .info-val { font-size: 13px; color: var(--text-primary); font-family: var(--font-mono); text-align: right; }
</style>
