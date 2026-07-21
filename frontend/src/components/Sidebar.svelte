<script>
  import { createEventDispatcher } from 'svelte'

  export let currentView = 'dashboard'
  const dispatch = createEventDispatcher()

  const navItems = [
    { id: 'dashboard', label: '仪表盘', cls: 'icon-web_home' },
    { id: 'sites', label: '项目', cls: '' },

    { id: 'webserver', label: 'Web 服务器', cls: 'icon-webfuwuqi' },
    { id: 'database', label: '数据库', cls: '' },
    { id: 'runtime', label: '运行环境', cls: '' },
    { id: 'logs', label: '日志', cls: '' },
  ]

  const bottomItems = [
    { id: 'packages', label: '软件包', cls: 'icon-OSruanjianbao' },
    { id: 'settings', label: '设置', cls: '' },
  ]

  const fallbackIcons = {
    dashboard: '◻',
    sites: '🌐',
    database: '🗄',
    software: '📦',
    webserver: '☰',
    runtime: '⚙',
    logs: '📄',
    packages: '📦',
    settings: '⚙',
  }
</script>

<aside class="sidebar">
  <div class="logo">
    <svg class="logo-icon" width="32" height="32" viewBox="0 0 32 32" fill="none">
      <rect width="32" height="32" rx="8" fill="var(--accent)"/>
      <path d="M10 16h12M16 10v12" stroke="#fff" stroke-width="2.5" stroke-linecap="round"/>
    </svg>
    <span class="logo-text">DevStack</span>
  </div>

  <nav class="nav">
    {#each navItems as item}
      <button
        class="nav-item"
        class:active={currentView === item.id}
        on:click={() => dispatch('navigate', item.id)}
      >
        <span class="nav-icon">
          {#if item.cls}
            <span class="iconfont {item.cls}"></span>
          {:else}
            {fallbackIcons[item.id]}
          {/if}
        </span>
        <span class="nav-label">{item.label}</span>
      </button>
    {/each}
  </nav>

  <div class="nav-bottom">
    {#each bottomItems as item}
      <button
        class="nav-item"
        class:active={currentView === item.id}
        on:click={() => dispatch('navigate', item.id)}
      >
        <span class="nav-icon">
          {#if item.cls}
            <span class="iconfont {item.cls}"></span>
          {:else}
            {fallbackIcons[item.id]}
          {/if}
        </span>
        <span class="nav-label">{item.label}</span>
      </button>
    {/each}
  </div>

  <div class="sidebar-footer">
    <div class="user-info">
      <div class="avatar">D</div>
      <div class="user-details">
        <span class="user-name">DevStack User</span>
        <span class="user-plan">Community Edition</span>
      </div>
    </div>
  </div>
</aside>

<style>
  .sidebar {
    width: 220px;
    background: var(--sidebar-bg);
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    user-select: none;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 20px 18px;
    border-bottom: 1px solid var(--sidebar-border);
  }

  .logo-icon { flex-shrink: 0; }

  .logo-text {
    font-size: 18px;
    font-weight: 700;
    color: var(--sidebar-logo);
  }

  .nav {
    flex: 1;
    padding: 12px 10px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .nav-bottom {
    padding: 8px 10px;
    border-top: 1px solid var(--sidebar-border);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--sidebar-text);
    transition: all 0.12s;
    text-align: left;
    width: 100%;
    font-size: 13.5px;
  }

  .nav-item:hover {
    background: var(--sidebar-hover);
    color: var(--sidebar-text-active);
  }

  .nav-item.active {
    background: var(--sidebar-active);
    color: var(--sidebar-text-active);
    font-weight: 500;
  }

  .nav-icon {
    font-size: 15px;
    width: 22px;
    text-align: center;
    opacity: 0.8;
  }
  .nav-item.active .nav-icon { opacity: 1; }

  .sidebar-footer {
    padding: 14px 16px;
    border-top: 1px solid var(--sidebar-border);
  }

  .user-info {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .avatar {
    width: 34px; height: 34px;
    border-radius: 50%;
    background: var(--sidebar-avatar-bg);
    display: flex; align-items: center; justify-content: center;
    color: var(--sidebar-avatar-text); font-size: 14px; font-weight: 600; flex-shrink: 0;
  }

  .user-details { display: flex; flex-direction: column; min-width: 0; }
  .user-name { color: var(--sidebar-user-name); font-size: 13px; font-weight: 500; }
  .user-plan { color: var(--sidebar-user-plan); font-size: 11px; }
</style>
