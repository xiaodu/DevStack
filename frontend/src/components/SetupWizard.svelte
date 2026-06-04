<script>
  import { createEventDispatcher } from 'svelte'
  import { api } from '../lib/api.js'

  const dispatch = createEventDispatcher()

  let step = 0
  let installDir = 'E:\\DevStack'
  let selectedRole = 'web'
  let packages = [
    { id: 'nginx', name: 'Nginx', checked: true, versions: ['1.26.0'], selected: '1.26.0' },
    { id: 'mysql', name: 'MySQL', checked: true, versions: ['8.4', '8.0'], selected: '8.4' },
    { id: 'redis', name: 'Redis', checked: true, versions: ['7.4.0', '7.2.0'], selected: '7.4.0' },
    { id: 'php', name: 'PHP', checked: true, versions: ['8.4', '8.3', '8.2'], selected: '8.4' },
    { id: 'node', name: 'Node.js', checked: false, versions: ['22', '20', '18'], selected: '20' },
    { id: 'python', name: 'Python', checked: false, versions: ['3.13', '3.12', '3.11'], selected: '3.12' },
  ]

  const roles = [
    { id: 'web', label: 'Web 开发者', icon: '🌐' },
    { id: 'fullstack', label: '全栈开发者', icon: '⚡' },
    { id: 'php', label: 'PHP 开发者', icon: '🐘' },
    { id: 'node', label: 'Node.js 开发者', icon: '⬢' },
    { id: 'python', label: 'Python 开发者', icon: '🐍' },
    { id: 'custom', label: '自定义选择', icon: '⚙' },
  ]

  let installProgress = []
  let installing = false
  let installComplete = false
  let currentInstall = ''

  function selectRole(id) {
    selectedRole = id
    if (id === 'web') {
      packages.forEach(p => p.checked = ['nginx','mysql','redis','php'].includes(p.id))
    } else if (id === 'fullstack') {
      packages.forEach(p => p.checked = true)
    } else if (id === 'php') {
      packages.forEach(p => p.checked = ['nginx','mysql','redis','php'].includes(p.id))
    } else if (id === 'node') {
      packages.forEach(p => p.checked = ['nginx','mysql','redis','node'].includes(p.id))
    } else if (id === 'python') {
      packages.forEach(p => p.checked = ['nginx','mysql','redis','python'].includes(p.id))
    }
  }

  async function startInstall() {
    step = 2
    installing = true
    const toInstall = packages.filter(p => p.checked)

    for (let i = 0; i < toInstall.length; i++) {
      const pkg = toInstall[i]
      currentInstall = `${pkg.name} ${pkg.selected}`
      installProgress = [...installProgress, { name: `${pkg.name} ${pkg.selected}`, status: 'installing', progress: 0 }]

      try {
        await api.downloadService(pkg.id, pkg.selected)
        installProgress[i].status = 'done'
        installProgress[i].progress = 100
      } catch (e) {
        installProgress[i].status = 'error'
        installProgress[i].error = e.message || String(e)
      }
      installProgress = [...installProgress]
    }

    installing = false
    installComplete = true
    step = 3
  }

  function finish() {
    dispatch('complete')
  }
</script>

<div class="wizard">
  <div class="wizard-banner">
    <div class="banner-content">
      <svg width="40" height="40" viewBox="0 0 32 32" fill="none">
        <rect width="32" height="32" rx="8" fill="rgba(255,255,255,0.2)"/>
        <path d="M10 16h12M16 10v12" stroke="#fff" stroke-width="2.5" stroke-linecap="round"/>
      </svg>
      <h1>DevStack</h1>
      <p class="tagline">The Essential Tool For Web Developers</p>
    </div>
  </div>

  {#if step === 0}
    <div class="wizard-body welcome-step">
      <h2>欢迎使用 DevStack</h2>
      <p class="subtitle">让我们开始设置您的开发环境</p>
      <div class="features">
        <p>DevStack 为您提供：</p>
        <ul>
          <li>一键安装开发环境</li>
          <li>多版本预配置的开发者工具</li>
          <li>自动配置和启动服务</li>
          <li>完整的本地开发工具栈</li>
        </ul>
      </div>
      <button class="btn btn-primary btn-lg wizard-btn" on:click={() => step = 1}>下一步</button>
    </div>

  {:else if step === 1}
    <div class="wizard-body setup-step">
      <div class="setup-layout">
        <div class="setup-left">
          <h3>让我们开始安装 DevStack</h3>
          <p class="hint">点击按钮，初始化你的 DevStack。</p>
          <div class="install-path">
            <span class="label">安装路径:</span>
            <span class="path">{installDir}</span>
          </div>
          <p class="note">所有软件包均安装到此目录，不影响系统现有软件，请放心安装。</p>
        </div>

        <div class="setup-center">
          <h4>请选择你的角色</h4>
          <div class="role-list">
            {#each roles as role}
              <button
                class="role-item"
                class:active={selectedRole === role.id}
                on:click={() => selectRole(role.id)}
              >
                <span>{role.icon}</span>
                <span>{role.label}</span>
              </button>
            {/each}
          </div>
        </div>

        <div class="setup-right">
          <div class="pkg-section">
            <label>默认 Web 服务器</label>
            <select class="form-input">
              <option>Nginx</option>
            </select>
          </div>
          <div class="pkg-list">
            {#each packages as pkg}
              <label class="pkg-item">
                <input type="checkbox" bind:checked={pkg.checked} />
                <span class="pkg-name">{pkg.name}</span>
                <select class="pkg-ver" bind:value={pkg.selected}>
                  {#each pkg.versions as v}
                    <option>{v}</option>
                  {/each}
                </select>
              </label>
            {/each}
          </div>
          <div class="pkg-footer">
            <button class="btn btn-primary btn-lg wizard-btn" on:click={startInstall}>开始安装</button>
          </div>
        </div>
      </div>
    </div>

  {:else if step === 2}
    <div class="wizard-body installing-step">
      <h2>正在安装软件包</h2>
      <p class="install-status">{currentInstall}</p>
      <div class="install-progress">
        {#each installProgress as item}
          <div class="install-item">
            <span class="item-icon">
              {#if item.status === 'done'}✓
              {:else if item.status === 'installing'}⏳
              {:else if item.status === 'error'}✕
              {:else}○
              {/if}
            </span>
            <span class="item-name">{item.name}</span>
            <span class="item-pct">{item.progress}%</span>
          </div>
        {/each}
      </div>
    </div>

  {:else if step === 3}
    <div class="wizard-body complete-step">
      <h2 class="complete-title">安装完成！</h2>
      <p class="subtitle">您的开发环境已准备就绪</p>
      <div class="next-steps">
        <p>接下来您可以：</p>
        <ol>
          <li>访问仪表盘 - 查看系统状态</li>
          <li>管理网站 - 添加您的第一个项目</li>
          <li>配置服务 - 调整服务设置</li>
        </ol>
      </div>
      <div class="complete-actions">
        <button class="btn btn-primary btn-lg wizard-btn" on:click={finish}>完成</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .wizard {
    height: 100vh;
    display: flex;
    flex-direction: column;
    background: #fff;
  }

  .wizard-banner {
    background: linear-gradient(135deg, #0f172a 0%, #1e3a5f 40%, #0d9488 100%);
    padding: 32px 48px;
  }
  .banner-content {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .banner-content h1 { color: #fff; font-size: 28px; font-weight: 700; }
  .banner-content .tagline {
    color: rgba(255,255,255,0.7);
    font-size: 14px;
    margin-left: auto;
    font-style: italic;
  }

  .wizard-body {
    flex: 1;
    overflow-y: auto;
    padding: 32px 48px;
  }

  .welcome-step {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
  }
  .welcome-step h2 { font-size: 28px; font-weight: 700; margin-bottom: 8px; }
  .welcome-step .subtitle { color: var(--text-muted); font-size: 15px; margin-bottom: 24px; }
  .features { text-align: left; margin-bottom: 32px; max-width: 400px; }
  .features p { font-weight: 500; margin-bottom: 8px; }
  .features ul { list-style: disc; padding-left: 20px; color: var(--text-secondary); }
  .features li { margin-bottom: 4px; }

  .wizard-btn { min-width: 160px; justify-content: center; }

  .setup-layout {
    display: grid;
    grid-template-columns: 200px 220px 1fr;
    gap: 24px;
    min-height: 100%;
  }
  .setup-left h3 { font-size: 18px; font-weight: 600; margin-bottom: 8px; }
  .setup-left .hint { color: var(--text-muted); font-size: 13px; margin-bottom: 16px; }
  .install-path { margin-bottom: 12px; }
  .install-path .label { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
  .install-path .path { font-size: 13px; font-family: var(--font-mono); color: var(--text-primary); }
  .note { font-size: 12px; color: var(--text-muted); line-height: 1.5; }

  .setup-center h4 { font-size: 14px; font-weight: 600; margin-bottom: 10px; }
  .role-list { display: flex; flex-direction: column; gap: 4px; }
  .role-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    font-size: 13px;
    color: var(--text-secondary);
    transition: all 0.1s;
    border: none;
    width: 100%;
  }
  .role-item:hover { background: var(--bg-inset); color: var(--text-primary); }
  .role-item.active { background: var(--accent); color: #fff; font-weight: 500; }

  .setup-right { display: flex; flex-direction: column; }
  .pkg-section { margin-bottom: 16px; }
  .pkg-section label { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
  .pkg-section select { max-width: 200px; }

  .pkg-list { flex: 1; display: flex; flex-direction: column; gap: 6px; }
  .pkg-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
  }
  .pkg-item input[type="checkbox"] { width: 16px; height: 16px; accent-color: var(--accent); }
  .pkg-name { font-size: 13px; min-width: 80px; }
  .pkg-ver {
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 12px;
    background: var(--bg-input);
    color: var(--text-primary);
  }

  .pkg-footer { padding-top: 16px; text-align: right; }

  .installing-step { text-align: center; padding-top: 48px; }
  .installing-step h2 { font-size: 22px; margin-bottom: 8px; }
  .install-status { color: var(--text-muted); font-size: 13px; margin-bottom: 24px; }
  .install-progress {
    max-width: 500px;
    margin: 0 auto;
    text-align: left;
  }
  .install-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border);
  }
  .install-item:last-child { border-bottom: none; }
  .item-icon { width: 20px; text-align: center; font-size: 14px; }
  .item-name { flex: 1; font-size: 13px; }
  .item-pct { font-size: 12px; color: var(--text-muted); font-family: var(--font-mono); }

  .complete-step {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
  }
  .complete-title { font-size: 28px; font-weight: 700; color: var(--green); margin-bottom: 8px; }
  .next-steps { text-align: left; margin: 24px 0; max-width: 400px; }
  .next-steps p { font-weight: 500; margin-bottom: 8px; }
  .next-steps ol { padding-left: 20px; color: var(--text-secondary); }
  .next-steps li { margin-bottom: 4px; }
  .complete-actions { margin-top: 16px; }
</style>
