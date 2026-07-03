<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { addToast } from '../lib/stores.js'

  let phpVersions = []
  let nodeVersions = []
  let pythonVersions = []
  let selectedId = null
  let selectedType = null
  let loading = true
  let phpExtensions = []
  let selectedExtensions = []
  let savingExts = false
  let configValues = {}

  async function load() {
    try {
      const all = await api.getServices()
      const langSvcs = all.filter(s => s.category === 'language')

      try {
        const registry = await api.getPHPRegistry()
        if (registry && registry.length > 0) {
          phpVersions = registry.map(v => ({ version: v.version, port: v.port }))
        }
      } catch (e) {}

      if (phpVersions.length === 0) {
        try {
          const installed = await api.getInstalledVersions('php')
          phpVersions = (installed || []).map(v => ({ version: v, port: 0 }))
        } catch (e) {}
      }

      if (phpVersions.length === 0) {
        const phpSvc = langSvcs.find(s => s.id === 'php')
        if (phpSvc) {
          phpVersions = [{ version: phpSvc.version, port: phpSvc.port || 9000 }]
        }
      }

      try {
        const installed = await api.getInstalledVersions('node')
        nodeVersions = (installed || []).map(v => ({ version: v }))
      } catch (e) {}

      if (nodeVersions.length === 0) {
        const nodeSvc = langSvcs.find(s => s.id === 'node')
        if (nodeSvc) {
          nodeVersions = [{ version: nodeSvc.version }]
        }
      }

      try {
        const installed = await api.getInstalledVersions('python')
        pythonVersions = (installed || []).map(v => ({ version: v }))
      } catch (e) {}

      if (pythonVersions.length === 0) {
        const pySvc = langSvcs.find(s => s.id === 'python')
        if (pySvc) {
          pythonVersions = [{ version: pySvc.version }]
        }
      }

      if (!selectedId) {
        if (phpVersions.length > 0) {
          selectLang(phpVersions[0].version, 'php')
        } else if (nodeVersions.length > 0) {
          selectLang(nodeVersions[0].version, 'node')
        } else if (pythonVersions.length > 0) {
          selectLang(pythonVersions[0].version, 'python')
        }
      }
    } catch (e) {
      console.error('[Lang] load error:', e)
    } finally {
      loading = false
    }
  }

  function selectLang(id, type) {
    selectedId = id
    selectedType = type
    phpExtensions = []
    selectedExtensions = []
    configValues = {}
    if (type === 'php') {
      loadExtensions(id)
    }
  }

  async function loadExtensions(version) {
    try {
      const exts = await api.getPHPExtensions(version)
      phpExtensions = exts || []
      selectedExtensions = phpExtensions.filter(e => e.enabled).map(e => e.name)
    } catch (e) {
      console.error('[Lang] loadExtensions error:', e)
      phpExtensions = []
    }
  }

  function toggleExt(name) {
    if (selectedExtensions.includes(name)) {
      selectedExtensions = selectedExtensions.filter(n => n !== name)
    } else {
      selectedExtensions = [...selectedExtensions, name]
    }
  }

  async function saveExtensions() {
    savingExts = true
    try {
      await api.savePHPExtensions(selectedId, selectedExtensions)
      await loadExtensions(selectedId)
      const all = await api.getServices()
      const phpSvc = all.find(s => s.id === 'php')
      if (phpSvc && phpSvc.status === 'running') {
        await api.restartService('php')
        addToast('扩展配置已保存，PHP 已自动重启')
      } else {
        addToast('扩展配置保存成功（重启 PHP 后生效）')
      }
    } catch (e) {
      addToast('保存扩展失败: ' + (e.message || e), 'error')
    } finally {
      savingExts = false
    }
  }

  async function saveConfig() {
    const vals = {}
    const fields = [
      ['php-max-exec', 'max_execution_time'],
      ['php-mem-limit', 'memory_limit'],
      ['php-post-max', 'post_max_size'],
      ['php-upload-max', 'upload_max_filesize'],
      ['php-timezone', 'date.timezone'],
    ]
    for (const [id, key] of fields) {
      const el = document.getElementById(id)
      if (el) vals[key] = el.value
    }
    try {
      await api.saveServiceConfig('php', vals)
      addToast('保存成功')
    } catch (e) {
      addToast('保存失败: ' + (e.message || e), 'error')
    }
  }

  async function toggleService(id, version) {
    try {
      const svcId = id === 'php' ? 'php-' + version : id
      const all = await api.getServices()
      const svc = all.find(s => s.id === id)
      if (svc && svc.status === 'running') {
        await api.stopService(svcId)
      } else {
        await api.startService(svcId)
      }
    } catch (e) {
      addToast(e.message || e, 'error')
    }
  }

  $: currentPHPVer = selectedType === 'php' ? phpVersions.find(v => v.version === selectedId) : null

  onMount(load)
</script>

<div class="lang-page">
  <div class="lang-left">
    <div class="list-header">
      <h3>语言</h3>
    </div>
    <div class="lang-list">
      <div class="group-label">PHP</div>
      {#each phpVersions as pv}
        <button
          class="lang-item"
          class:active={selectedId === pv.version && selectedType === 'php'}
          on:click={() => selectLang(pv.version, 'php')}
        >
          <span class="lang-icon">🐘</span>
          <div class="lang-info">
            <span class="lang-name">PHP {pv.version}</span>
            <span class="lang-port">端口 {pv.port || '?'}</span>
          </div>
        </button>
      {/each}

      <div class="group-label">Node.js</div>
      {#each nodeVersions as nv}
        <button
          class="lang-item"
          class:active={selectedId === nv.version && selectedType === 'node'}
          on:click={() => selectLang(nv.version, 'node')}
        >
          <span class="lang-icon">⬢</span>
          <div class="lang-info">
            <span class="lang-name">Node.js {nv.version}</span>
          </div>
        </button>
      {/each}

      <div class="group-label">Python</div>
      {#each pythonVersions as pv}
        <button
          class="lang-item"
          class:active={selectedId === pv.version && selectedType === 'python'}
          on:click={() => selectLang(pv.version, 'python')}
        >
          <span class="lang-icon">🐍</span>
          <div class="lang-info">
            <span class="lang-name">Python {pv.version}</span>
          </div>
        </button>
      {/each}
    </div>
  </div>

  <div class="lang-right">
    {#if selectedType === 'php' && currentPHPVer}
      <div class="detail-header">
        <h2>PHP {currentPHPVer.version} 设置</h2>
        <div class="header-actions">
          <button class="btn btn-sm btn-success" on:click={() => toggleService('php', currentPHPVer.version)}>
            启动
          </button>
        </div>
      </div>

      <div class="ini-path">
        <span class="ini-path-label">php.ini 路径：</span>
        <code class="ini-path-value">install/php/{currentPHPVer.version}/php.ini</code>
      </div>

      <div class="section-title">基本配置</div>

      <div class="config-grid">
        <div class="config-row">
          <label class="form-label">
            <span>端口</span>
            <input class="form-input" type="number" value={currentPHPVer.port} readonly />
          </label>
          <label class="form-label">
            <span>安装路径</span>
            <input class="form-input" value="php/{currentPHPVer.version}" readonly />
          </label>
        </div>
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
      </div>

      <div class="actions">
        <button class="btn btn-primary" on:click={saveConfig}>保存设置</button>
      </div>

      <div class="ext-section">
        <div class="ext-header">
          <h3>PHP 扩展管理</h3>
          <div class="ext-header-right">
            <span class="ext-count">已选 {selectedExtensions.length}/{phpExtensions.length}</span>
            <button class="btn btn-sm btn-outline" on:click={() => loadExtensions(selectedId)} title="刷新扩展列表">
              刷新
            </button>
          </div>
        </div>

        {#if phpExtensions.length === 0}
          <div class="ext-loading">正在加载扩展列表...</div>
        {:else}
          <div class="ext-grid">
            {#each phpExtensions as ext}
              <label class="ext-item" class:enabled={selectedExtensions.includes(ext.name)}>
                <input
                  type="checkbox"
                  checked={selectedExtensions.includes(ext.name)}
                  on:change={() => toggleExt(ext.name)}
                />
                <span class="ext-name">{ext.name}</span>
              </label>
            {/each}
          </div>
        {/if}

        <div class="actions">
          <button class="btn btn-primary" on:click={saveExtensions} disabled={savingExts}>
            {savingExts ? '保存中...' : '保存扩展配置'}
          </button>
        </div>
      </div>

    {:else if selectedType === 'node'}
      <div class="detail-header">
        <h2>Node.js {selectedId}</h2>
      </div>
      <div class="config-grid">
        <div class="info-block">
          <p>当前版本: <strong>{selectedId}</strong></p>
          <p style="margin-top:8px;color:var(--text-muted);font-size:13px;">
            安装目录: install/node/{selectedId}/
          </p>
        </div>
      </div>

    {:else if selectedType === 'python'}
      <div class="detail-header">
        <h2>Python {selectedId}</h2>
      </div>
      <div class="config-grid">
        <div class="info-block">
          <p>当前版本: <strong>{selectedId}</strong></p>
          <p style="margin-top:8px;color:var(--text-muted);font-size:13px;">
            安装目录: install/python/{selectedId}/
          </p>
        </div>
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
    cursor: pointer;
  }
  .lang-item:hover { background: var(--bg-inset); }
  .lang-item.active { background: var(--accent-light); }

  .lang-icon { font-size: 18px; }

  .lang-info { flex: 1; min-width: 0; }
  .lang-name { font-size: 13px; font-weight: 500; color: var(--text-primary); display: block; }
  .lang-port { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }

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

  .ini-path {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 20px;
    padding: 8px 12px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    font-size: 12px;
  }
  .ini-path-label { color: var(--text-muted); white-space: nowrap; }
  .ini-path-value { color: var(--text-secondary); font-family: var(--font-mono); word-break: break-all; }

  .section-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 16px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border);
  }

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

  .ext-section {
    display: block;
    margin-top: 24px;
    padding-top: 20px;
    border-top: 1px solid var(--border);
  }

  .ext-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }
  .ext-header h3 { font-size: 16px; font-weight: 600; }
  .ext-header-right { display: flex; align-items: center; gap: 10px; }

  .ext-count {
    font-size: 12px;
    color: var(--text-muted);
  }

  :global(.btn-outline) {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
  }
  :global(.btn-outline:hover) {
    border-color: var(--accent);
    color: var(--accent);
  }

  .ext-loading {
    color: var(--text-muted);
    font-size: 13px;
    padding: 12px 0;
  }

  .ext-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 6px;
    max-height: 360px;
    overflow-y: auto;
    padding: 4px 0;
  }

  .ext-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.1s;
    user-select: none;
  }
  .ext-item:hover { background: var(--bg-inset); }
  .ext-item.enabled { color: var(--text-primary); font-weight: 500; }

  .ext-item input[type="checkbox"] {
    width: 15px;
    height: 15px;
    accent-color: var(--accent);
    cursor: pointer;
    flex-shrink: 0;
  }

  .ext-name {
    word-break: break-all;
    font-family: var(--font-mono);
    font-size: 12px;
  }

  .info-block {
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    padding: 16px;
    font-size: 14px;
    color: var(--text-secondary);
  }

  .actions {
    margin-top: 16px;
  }
</style>
