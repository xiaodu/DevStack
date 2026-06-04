<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let sites = []
  let selectedId = null
  let editing = null
  let loading = true
  let searchQuery = ''

  async function load() {
    try {
      sites = await api.getSites()
      if (!selectedId && sites.length > 0) {
        selectSite(sites[0].id)
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  function selectSite(id) {
    selectedId = id
    const s = sites.find(x => x.id === id)
    editing = s ? { ...s } : null
  }

  function newSite() {
    editing = { id: '', name: '', domain: '', documentRoot: '', enabled: true, type: 'static', port: 80 }
    selectedId = '__new__'
  }

  async function save() {
    if (!editing) return
    try {
      if (editing.id && editing.id !== '__new__') {
        await api.updateSite(editing)
      } else {
        const { id, ...rest } = editing
        await api.createSite(rest)
      }
      await load()
    } catch (e) {
      alert(e.message || e)
    }
  }

  async function remove() {
    if (!editing || !editing.id || editing.id === '__new__') return
    if (!confirm('确定删除此站点？')) return
    try {
      await api.deleteSite(editing.id)
      selectedId = null
      editing = null
      await load()
    } catch (e) {
      alert(e.message || e)
    }
  }

  async function toggleSite(id) {
    try {
      await api.toggleSite(id)
      await load()
    } catch (e) {
      alert(e.message || e)
    }
  }

  $: filteredSites = sites.filter(s =>
    s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    s.domain.toLowerCase().includes(searchQuery.toLowerCase())
  )

  onMount(load)
</script>

<div class="sites-page">
  <div class="sites-left">
    <div class="list-header">
      <h3>网站</h3>
      <button class="btn-add" on:click={newSite} title="添加网站">+</button>
    </div>
    <div class="search-box">
      <input class="form-input" placeholder="搜索..." bind:value={searchQuery} />
    </div>
    <div class="site-list">
      {#each filteredSites as site}
        <button
          class="site-item"
          class:active={selectedId === site.id}
          on:click={() => selectSite(site.id)}
        >
          <div class="site-info">
            <span class="site-dot {site.enabled ? 'on' : 'off'}"></span>
            <div>
              <div class="site-name">{site.name}</div>
              <div class="site-domain">{site.domain}</div>
            </div>
          </div>
          <div class="site-actions">
            <button class="mini-btn" on:click|stopPropagation={() => toggleSite(site.id)} title={site.enabled ? '禁用' : '启用'}>
              {site.enabled ? '⏸' : '▶'}
            </button>
          </div>
        </button>
      {/each}
      {#if filteredSites.length === 0}
        <div class="empty-list">暂无网站</div>
      {/if}
    </div>
  </div>

  <div class="sites-right">
    {#if editing}
      <div class="detail-header">
        <h2>{editing.id === '__new__' ? '新建网站' : editing.name}</h2>
        <div class="header-actions">
          {#if editing.id && editing.id !== '__new__'}
            <button class="btn-icon red" on:click={remove} title="删除">🗑</button>
          {/if}
          <button class="btn-icon green" on:click={save} title="保存">✓</button>
        </div>
      </div>

      <div class="form-grid">
        <label class="form-label">
          <span>名字</span>
          <input class="form-input" bind:value={editing.name} />
        </label>

        <label class="form-label">
          <span>域名</span>
          <input class="form-input" bind:value={editing.domain} placeholder="example.dev" />
        </label>

        <div class="form-row-3">
          <label class="form-label">
            <span>Web 协议</span>
            <select class="form-input" bind:value={editing.protocol}>
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
              <option value="both">HTTP & HTTPS</option>
            </select>
          </label>
          <label class="form-label">
            <span>HTTP Port</span>
            <input class="form-input" type="number" bind:value={editing.port} />
          </label>
          <label class="form-label">
            <span>HTTPS Port</span>
            <input class="form-input" type="number" bind:value={editing.httpsPort} placeholder="443" />
          </label>
        </div>

        <div class="form-row-3">
          <label class="form-label">
            <span>网站类型</span>
            <select class="form-input" bind:value={editing.type}>
              <option value="static">静态</option>
              <option value="php">PHP</option>
              <option value="proxy">反向代理</option>
            </select>
          </label>
          <label class="form-label">
            <span>PHP 版本</span>
            <select class="form-input" bind:value={editing.phpVersion}>
              <option value="">不使用</option>
              <option value="8.2">PHP 8.2</option>
              <option value="8.3">PHP 8.3</option>
              <option value="8.4">PHP 8.4</option>
            </select>
          </label>
          <label class="form-label">
            <span>Rewrite 规则</span>
            <select class="form-input" bind:value={editing.rewrite}>
              <option value="default">Default</option>
              <option value="laravel">Laravel</option>
              <option value="thinkphp">ThinkPHP</option>
              <option value="custom">自定义</option>
            </select>
          </label>
        </div>

        <label class="form-label">
          <span>网站根目录</span>
          <div class="input-row">
            <input class="form-input" bind:value={editing.documentRoot} placeholder="E:\projects\my-site" />
            <button class="btn btn-outline btn-sm">浏览</button>
          </div>
        </label>

        <label class="toggle-label">
          <input type="checkbox" bind:checked={editing.enabled} />
          <span>启用站点</span>
        </label>
      </div>
    {:else}
      <div class="empty-state">
        <div class="icon">🌐</div>
        <p>选择左侧网站进行编辑</p>
        <p style="font-size:12px;margin-top:4px;">或点击 + 创建新网站</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .sites-page {
    display: grid;
    grid-template-columns: 300px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .sites-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .list-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .btn-add {
    width: 28px; height: 28px;
    border-radius: 50%;
    background: var(--accent);
    color: #fff;
    font-size: 16px;
    display: flex; align-items: center; justify-content: center;
    transition: background 0.12s;
  }
  .btn-add:hover { background: var(--accent-hover); }

  .search-box { padding: 12px 16px; border-bottom: 1px solid var(--border); }

  .site-list {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
  }

  .site-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    transition: background 0.1s;
    border: none;
  }
  .site-item:hover { background: var(--bg-inset); }
  .site-item.active { background: var(--accent-light); }

  .site-info { display: flex; align-items: center; gap: 10px; min-width: 0; }

  .site-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .site-dot.on { background: var(--green); }
  .site-dot.off { background: var(--text-muted); }

  .site-name { font-size: 13px; font-weight: 500; color: var(--text-primary); }
  .site-domain { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }

  .mini-btn {
    width: 24px; height: 24px; border-radius: 4px;
    background: transparent; font-size: 11px;
    display: flex; align-items: center; justify-content: center;
    color: var(--text-muted); transition: all 0.1s;
  }
  .mini-btn:hover { background: var(--bg-secondary); color: var(--text-primary); }

  .empty-list { padding: 24px; text-align: center; color: var(--text-muted); font-size: 13px; }

  .sites-right {
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

  .btn-icon {
    width: 36px; height: 36px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 16px; transition: transform 0.1s;
  }
  .btn-icon:hover { transform: scale(1.1); }
  .btn-icon.green { background: var(--green); color: #fff; }
  .btn-icon.red { background: var(--red-bg); color: var(--red); }

  .form-grid {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 700px;
  }
  .form-row-3 {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 12px;
  }
  .input-row { display: flex; gap: 8px; }
  .input-row .form-input { flex: 1; }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .toggle-label input[type="checkbox"] {
    width: 18px; height: 18px;
    accent-color: var(--accent);
  }
</style>
