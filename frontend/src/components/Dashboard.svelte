<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { refreshSignal, globalLoading, addToast } from '../lib/stores.js'

  let services = []
  let runtimes = []
  let sites = []
  let systemInfo = {}
  let hasLoaded = false

  const serviceMeta = {
    nginx:  { label: 'Nginx',  cls: 'icon-nginx',  color: '#009639' },
    mysql:  { label: 'MySQL',  cls: 'icon-mysql',  color: '#4479A1' },
    redis:  { label: 'Redis',  cls: 'icon-redis',  color: '#DC382D' },
  }

  let ops = []

  async function load() {
    globalLoading.set(true)
    try {
      const [s, r, st, info, opEntries] = await Promise.all([
        api.getServices(), api.getRuntimes(), api.getSites(), api.getSystemInfo(), api.getRecentOps(100),
      ])
      services = s
      runtimes = r
      sites = st
      systemInfo = info
      ops = opEntries || []
      hasLoaded = true
    } catch (e) {
      console.error(e)
    } finally {
      globalLoading.set(false)
    }
  }

  async function toggleService(id) {
    try {
      const svc = services.find(s => s.id === id)
      if (svc && svc.status === 'running') {
        await api.stopService(id)
      } else {
        await api.startService(id)
      }
    } catch (e) {
      addToast(e.message || e, 'error')
    }
    await load()
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
  <div class="card-grid">
    <div class="section card">
      <div class="section-header"><h3>服务</h3></div>
      <div class="mini-cards">
        {#each (hasLoaded ? services : [null,null,null]) as _, i}
          {@const svc = hasLoaded ? services[i] : null}
          {@const meta = svc ? (serviceMeta[svc.id] || { cls: '', color: 'var(--accent)' }) : { cls: '', color: 'var(--border)' }}
          <button
            class="mini-card"
            class:running={svc?.status === 'running'}
            class:placeholder={!svc}
            style={svc ? '--card-color: ' + meta.color + ';' : ''}
            on:click={() => svc && svc.installed && toggleService(svc.id)}
            disabled={!svc || !svc.installed}
          >
            <div class="mini-accent" style="background: {meta.color};"></div>
            <div class="mini-body">
              {#if svc}
                <span class="iconfont {meta.cls}" style="color: {meta.color};font-size:28px"></span>
                <div class="mini-info">
                  <div class="mini-name">{svc.name}</div>
                  <div class="mini-meta">
                    <span class="mini-dot" class:on={svc.status === 'running'}></span>
                    {svc.installed ? statusLabel(svc.status) : '未安装'}
                  </div>
                </div>
              {:else}
                <span class="icon-placeholder"></span>
                <div class="mini-info"><div class="placeholder-line w-60"></div><div class="placeholder-line w-40"></div></div>
              {/if}
            </div>
          </button>
        {/each}
      </div>
    </div>

  </div>

  <div class="site-section card">
    <div class="section-header">
      <h3>项目列表</h3>
      <span class="badge badge-info">{sites.length} 个</span>
    </div>
    {#if !hasLoaded}
      <div class="empty-state"><p style="color:var(--text-muted)">加载中...</p></div>
    {:else if sites.length === 0}
      <div class="empty-state">
        <p>暂无项目</p>
        <p style="font-size:12px;margin-top:4px;">前往「项目」页面添加您的第一个站点</p>
      </div>
    {:else}
      <table class="site-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>域名</th>
            <th>类型</th>
            <th>根目录</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          {#each sites as site}
            <tr>
              <td>{site.name}</td>
              <td><code>{site.domain}</code></td>
              <td><span class="type-badge">{site.type}</span></td>
              <td class="path">{site.root || '-'}</td>
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

  <div class="bottom-panel">
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
            {#each [...services, ...runtimes] as svc}
              {#if svc.installed}
                <div class="info-item">
                  <span class="info-label">{svc.name}</span>
                  <span class="info-val">
                    {#if svc.status === 'running' || svc.running}
                      PID: {svc.pid || '-'}　{svc.uptime || ''}
                    {:else}
                      {statusLabel(svc.status) || '已停止'}
                    {/if}
                  </span>
                </div>
              {/if}
            {/each}
          {/if}
        </div>
      </div>
    </div>

    <div class="ops-section card">
      <div class="section-header">
        <h3>操作日志</h3>
        <span class="badge badge-info">{ops.length} 条</span>
      </div>
      <div class="ops-list">
        {#if !hasLoaded}
          <div class="empty-state"><p style="color:var(--text-muted);padding:20px">加载中...</p></div>
        {:else if ops.length === 0}
          <div class="empty-state">
            <p>暂无操作记录</p>
          </div>
        {:else}
          {#each ops as op}
            <div class="op-item" class:op-error={op.status && op.status.startsWith('失败')}>
              <span class="op-time">{op.time}</span>
              <span class="op-action">{op.action} {op.service}</span>
              <span class="op-detail">{op.detail}</span>
              <span class="op-status" class:success={op.status === '成功'} class:fail={op.status && op.status.startsWith('失败')}>
                {op.status === '成功' ? '成功' : '失败'}
              </span>
            </div>
          {/each}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .dashboard { 
    display: flex; 
    flex-direction: column; 
    gap: 20px;
    height: 100%;
    overflow: hidden;
  }

  .card-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 20px;
  }

  .mini-cards {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    padding: 12px;
  }

  .mini-card {
    position: relative;
    background: var(--bg-card-hover);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    text-align: left;
    cursor: pointer;
    padding: 0;
    overflow: hidden;
    outline: none;
    width: 100%;
    box-sizing: border-box;
  }
  .mini-card:hover { box-shadow: var(--shadow-sm); }
  .mini-card.running { border-color: var(--card-color); }
  .mini-card:disabled { opacity: 0.5; cursor: default; }
  .mini-card.placeholder { border-style: dashed; }

  .mini-accent { height: 2px; width: 100%; }
  .mini-body {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px;
  }

  .mini-info { flex: 1; min-width: 0; }
  .mini-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .mini-meta { display: flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-muted); margin-top: 2px; }

  .mini-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--text-muted); flex-shrink: 0;
  }
  .mini-dot.on { background: var(--green); }

  .icon-placeholder { width: 28px; height: 28px; border-radius: 6px; background: var(--border); }

  .placeholder-line { height: 8px; border-radius: 4px; background: var(--border); margin-bottom: 4px; }
  .w-60 { width: 60%; }
  .w-40 { width: 40%; }

  .bottom-panel {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 20px;
    flex: 1;
    min-height: 0;
  }

  .site-section { padding: 0; overflow: hidden; }

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

  .type-badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 4px;
    background: var(--bg-inset);
    color: var(--text-secondary);
    font-family: var(--font-mono);
  }

  .section-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
  }
  .section-header h3 { font-size: 14px; font-weight: 600; }

  .section.card { padding: 0; overflow: hidden; }

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

  .ops-section { 
    padding: 0; 
    overflow: hidden; 
    display: flex; 
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .ops-list {
    flex: 1;
    overflow-y: auto;
    padding: 8px 0;
    min-height: 0;
  }

  .op-item {
    display: grid;
    grid-template-columns: 80px 140px 1fr 52px;
    align-items: center;
    gap: 6px;
    padding: 6px 16px;
    font-size: 12px;
    border-bottom: 1px solid var(--border);
  }
  .op-item:last-child { border-bottom: none; }
  .op-item:hover { background: var(--bg-card-hover); }
  .op-item.op-error { background: rgba(239,68,68,0.04); }

  .op-time { font-family: var(--font-mono); font-size: 11px; color: var(--text-muted); white-space: nowrap; }
  .op-action { font-weight: 600; color: var(--text-primary); }
  .op-service { color: var(--accent); font-family: var(--font-mono); font-size: 11px; }
  .op-detail { color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .op-status { font-size: 11px; text-align: right; padding: 1px 6px; border-radius: 4px; background: var(--bg-inset); }
  .op-status.success { color: var(--green); }
  .op-status.fail { color: var(--red); }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 32px 20px;
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
