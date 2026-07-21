<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let settings = { installDir: '', startOnBoot: false, minimizeToTray: true, theme: 'light', language: 'zh-CN' }
  let systemInfo = {}
  let loading = true
  let saved = false
  let saveError = ''
  let configPath = ''

  async function load() {
    try {
      const [s, info, path] = await Promise.all([api.getSettings(), api.getSystemInfo(), api.getConfigPath()])
      settings = { ...settings, ...s }
      systemInfo = info
      configPath = path
    } catch (e) {
      console.error(e)
    } finally {
      loading = false
    }
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme || 'light')
  }

  function handleThemeChange() {
    applyTheme(settings.theme)
  }

  async function save() {
    saveError = ''
    try {
      await api.saveSettings(settings)
      const verified = await api.getSettings()
      if (verified.theme !== settings.theme) {
        saveError = `保存验证失败：期望主题 "${settings.theme}"，实际 "${verified.theme}"`
        return
      }
      applyTheme(settings.theme)
      saved = true
      setTimeout(() => saved = false, 2000)
    } catch (e) {
      saveError = '保存失败: ' + (e.message || e)
      console.error(e)
    }
  }

  onMount(load)
</script>

<div class="settings-page">
  <h1 class="page-title">系统设置</h1>
  <p class="page-desc">配置 DevStack 运行参数</p>

  {#if loading}
    <div class="loading">加载中...</div>
  {:else}
    <div class="section">
      <h3>基本设置</h3>
      <div class="fields">
        <label class="form-label">
          <span>安装目录</span>
          <input class="form-input" bind:value={settings.installDir} placeholder="服务安装路径" />
        </label>
      </div>
    </div>

    <div class="section">
      <h3>通用设置</h3>
      <div class="fields">
        <label class="toggle-label">
          <input type="checkbox" bind:checked={settings.startOnBoot} />
          <span>开机自启</span>
        </label>
        <label class="toggle-label">
          <input type="checkbox" bind:checked={settings.minimizeToTray} />
          <span>最小化到系统托盘</span>
        </label>
      </div>
    </div>

    <div class="section">
      <h3>界面</h3>
      <div class="fields cols-2">
        <label class="form-label">
          <span>主题</span>
          <select class="form-input" bind:value={settings.theme} on:change={handleThemeChange}>
            <option value="light">浅色</option>
            <option value="dark">深色</option>
          </select>
        </label>
        <label class="form-label">
          <span>语言</span>
          <select class="form-input" bind:value={settings.language}>
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
        </label>
      </div>
    </div>

    <div class="section">
      <h3>系统信息</h3>
      <div class="info-grid">
        {#each Object.entries(systemInfo) as [key, val]}
          <div class="info-row">
            <span class="label">{key}</span>
            <span class="value">{val}</span>
          </div>
        {/each}
      </div>
      {#if configPath}
        <div class="info-row" style="margin-top:10px">
          <span class="label">配置文件路径</span>
          <span class="value config-path">{configPath}</span>
        </div>
      {/if}
    </div>

    <div class="save-bar">
      <button class="btn btn-primary btn-lg" on:click={save}>保存设置</button>
      {#if saved}
        <span class="saved-msg">已保存</span>
      {/if}
      {#if saveError}
        <span class="error-msg">{saveError}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .settings-page { max-width: 680px; }
  .section {
    background: var(--bg-card);
    border-radius: var(--radius);
    padding: 20px;
    margin-bottom: 12px;
    border: 1px solid var(--border);
  }
  .section h3 {
    font-size: 14px;
    font-weight: 600;
    margin-bottom: 14px;
    color: var(--accent);
  }
  .fields { display: flex; flex-direction: column; gap: 12px; }
  .cols-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    color: var(--text-primary);
  }
  .toggle-label input[type="checkbox"] {
    width: 18px; height: 18px;
    accent-color: var(--accent);
  }

  .info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .info-row { display: flex; flex-direction: column; gap: 2px; }
  .info-row .label { font-size: 12px; color: var(--text-muted); }
  .info-row .value { font-size: 13px; }
  .config-path { font-family: var(--font-mono); font-size: 12px; color: var(--text-secondary); word-break: break-all; }

  .save-bar { display: flex; align-items: center; gap: 12px; margin-top: 18px; }
  .saved-msg { color: var(--green); font-size: 14px; font-weight: 500; }
  .error-msg { color: var(--red); font-size: 14px; }
  .loading { color: var(--text-muted); padding: 40px; text-align: center; }
</style>
