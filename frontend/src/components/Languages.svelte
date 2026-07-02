<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let services = []
  let selectedId = null
  let loading = true

  const langGroups = [
    { label: 'PHP', ids: ['php'] },
    { label: 'Node.js', ids: ['node'] },
    { label: 'Python', ids: ['python'] },
  ]

  async function load() {
    try {
      const all = await api.getServices()
      services = all.filter(s => s.category === 'language' && s.installed)
      if (!selectedId && services.length > 0) {
        selectLang(services[0].id)
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  function selectLang(id) { selectedId = id }

  async function saveConfig(svc) {
    const vals = {}
    if (svc.id === 'php') {
      const maxExec = document.getElementById('php-max-exec')
      const memLimit = document.getElementById('php-mem-limit')
      const postMax = document.getElementById('php-post-max')
      const uploadMax = document.getElementById('php-upload-max')
      const timezone = document.getElementById('php-timezone')
      if (maxExec) vals['max_execution_time'] = maxExec.value
      if (memLimit) vals['memory_limit'] = memLimit.value
      if (postMax) vals['post_max_size'] = postMax.value
      if (uploadMax) vals['upload_max_filesize'] = uploadMax.value
      if (timezone) vals['date.timezone'] = timezone.value
    }
    try {
      await api.saveServiceConfig(svc.id, vals)
      alert('保存成功')
    } catch (e) {
      alert('保存失败: ' + (e.message || e))
    }
  }

  function getLang(id) { return services.find(s => s.id === id) }

  async function toggle(id) {
    try {
      const svc = getLang(id)
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

  $: current = selectedId ? getLang(selectedId) : null

  onMount(load)
</script>

<div class="lang-page">
  <div class="lang-left">
    <div class="list-header">
      <h3>语言</h3>
    </div>
    <div class="lang-list">
      {#each langGroups as group}
        <div class="group-label">{group.label}</div>
        {#each services.filter(s => group.ids.includes(s.id)) as svc}
          <button
            class="lang-item"
            class:active={selectedId === svc.id}
            on:click={() => selectLang(svc.id)}
          >
            <span class="lang-icon">{svc.id === 'php' ? '🐘' : svc.id === 'node' ? '⬢' : '🐍'}</span>
            <div class="lang-info">
              <span class="lang-name">{svc.name} {svc.version}</span>
            </div>
            <span class="lang-status running"></span>
          </button>
        {/each}
      {/each}
    </div>
  </div>

  <div class="lang-right">
    {#if current}
      <div class="detail-header">
        <h2>{current.name} {current.version} 服务配置</h2>
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
        {#if current.id === 'php'}
          <div class="config-row">
            <label class="form-label">
              <span>最大执行时间 (秒)</span>
              <input id="php-max-exec" class="form-input" type="number" value="300" />
            </label>
            <label class="form-label">
              <span>最大输入时间 (秒)</span>
              <input class="form-input" type="number" value="60" />
            </label>
          </div>

          <div class="config-row">
            <label class="form-label">
              <span>内存限制</span>
              <select id="php-mem-limit" class="form-input">
                <option>64M</option>
                <option>128M</option>
                <option selected>256M</option>
                <option>512M</option>
                <option>1G</option>
              </select>
            </label>
            <label class="form-label">
              <span>最大 POST 大小</span>
              <select id="php-post-max" class="form-input">
                <option>8M</option>
                <option>16M</option>
                <option selected>128M</option>
                <option>256M</option>
              </select>
            </label>
          </div>

          <div class="config-row">
            <label class="form-label">
              <span>最大上传文件大小</span>
              <select id="php-upload-max" class="form-input">
                <option>2M</option>
                <option>8M</option>
                <option>16M</option>
                <option selected>128M</option>
              </select>
            </label>
            <label class="form-label">
              <span>最大文件上传数</span>
              <input class="form-input" type="number" value="20" />
            </label>
          </div>

            <label class="form-label">
              <span>时区</span>
              <select id="php-timezone" class="form-input">
                <option>UTC</option>
                <option selected>Asia/Shanghai</option>
                <option>America/New_York</option>
                <option>Europe/London</option>
              </select>
            </label>

          <div class="toggle-row">
            <label class="toggle-label">
              <input type="checkbox" checked />
              <span>暴露 PHP</span>
            </label>
            <label class="toggle-label">
              <input type="checkbox" />
              <span>短标签</span>
            </label>
            <label class="toggle-label">
              <input type="checkbox" checked />
              <span>允许 URL fopen</span>
            </label>
            <label class="toggle-label">
              <input type="checkbox" />
              <span>允许 URL include</span>
            </label>
          </div>

        {:else if current.id === 'node'}
          <div class="info-block">
            <p>Node.js 作为版本管理器运行。当前安装版本: <strong>{current.version}</strong></p>
            <p style="margin-top:8px;color:var(--text-muted);font-size:13px;">
              安装目录: install/node/{current.version}/
            </p>
          </div>

        {:else if current.id === 'python'}
          <div class="info-block">
            <p>Python 作为版本管理器运行。当前安装版本: <strong>{current.version}</strong></p>
            <p style="margin-top:8px;color:var(--text-muted);font-size:13px;">
              安装目录: install/python/{current.version}/
            </p>
          </div>
        {/if}
      </div>

      <div class="actions">
        <button class="btn btn-primary" on:click={() => saveConfig(current)}>保存设置</button>
      </div>
    {:else}
      <div class="empty-state">
        <div class="icon">💻</div>
        <p>选择左侧语言进行配置</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .lang-page {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 0;
    height: calc(100vh - 48px);
    margin: -24px -32px;
    background: var(--bg-card);
  }

  .lang-left {
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
  }

  .list-header {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .list-header h3 { font-size: 16px; font-weight: 600; }

  .lang-list { flex: 1; padding: 8px; overflow-y: auto; }

  .group-label {
    font-size: 16px;
    font-weight: 700;
    color: var(--text-primary);
    padding: 16px 12px 8px;
    border-bottom: 1px solid var(--border);
    margin: 8px 0 4px;
  }
  .group-label:first-of-type { margin-top: 0; padding-top: 12px; }

  .lang-item {
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
  .lang-item:hover { background: var(--bg-inset); }
  .lang-item.active { background: var(--accent-light); }

  .lang-icon { font-size: 18px; }

  .lang-info { flex: 1; min-width: 0; }
  .lang-name { font-size: 13px; font-weight: 500; color: var(--text-primary); }

  .lang-status {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .lang-status.running { background: var(--green); }
  .lang-status.stopped { background: var(--text-muted); }

  .lang-right {
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

  .toggle-row {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .toggle-label input[type="checkbox"] {
    width: 16px; height: 16px;
    accent-color: var(--accent);
  }

  .info-block {
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    padding: 16px;
    font-size: 14px;
    color: var(--text-secondary);
  }

  .actions {
    margin-top: 24px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
</style>
