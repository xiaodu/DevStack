<script>
  import { createEventDispatcher } from 'svelte'

  export let currentView = 'dashboard'
  const dispatch = createEventDispatcher()

  const navItems = [
    { id: 'dashboard', label: '仪表盘', icon: '◻' },
    { id: 'sites', label: '网站', icon: '🌐' },
    { id: 'database', label: '数据库', icon: '🗄' },
    { id: 'languages', label: '语言', icon: '<>' },
    { id: 'webserver', label: 'Web 服务器', icon: '☰' },
    { id: 'logs', label: '日志', icon: '📄' },
  ]

  const bottomItems = [
    { id: 'packages', label: '软件包', icon: '📦' },
    { id: 'settings', label: '设置', icon: '⚙' },
  ]
</script>

<aside class="sidebar">
  <div class="logo">
    <svg class="logo-icon" width="32" height="32" viewBox="0 0 32 32" fill="none">
      <rect width="32" height="32" rx="8" fill="rgba(255,255,255,0.15)"/>
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
        <span class="nav-icon">{item.icon}</span>
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
        <span class="nav-icon">{item.icon}</span>
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
    border-bottom: 1px solid rgba(255,255,255,0.1);
  }

  .logo-icon { flex-shrink: 0; }

  .logo-text {
    font-size: 18px;
    font-weight: 700;
    color: #fff;
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
    border-top: 1px solid rgba(255,255,255,0.1);
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
    border-top: 1px solid rgba(255,255,255,0.1);
  }

  .user-info {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .avatar {
    width: 34px; height: 34px;
    border-radius: 50%;
    background: rgba(255,255,255,0.2);
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 14px; font-weight: 600; flex-shrink: 0;
  }

  .user-details { display: flex; flex-direction: column; min-width: 0; }
  .user-name { color: #fff; font-size: 13px; font-weight: 500; }
  .user-plan { color: rgba(255,255,255,0.5); font-size: 11px; }
</style>
