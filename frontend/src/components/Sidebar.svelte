<script lang="ts">
import { createEventDispatcher } from 'svelte'
import UserProfile from './UserProfile.svelte'

interface Props {
  currentView?: string
}

let { currentView = 'dashboard' }: Props = $props()
const dispatch = createEventDispatcher()
let profileOpen = $state(false)

const navItems = [
  { id: 'dashboard', label: '控制面板', icon: 'dashboard' },
  { id: 'environment', label: '环境管理', icon: 'environment' },
  { id: 'sites', label: '项目与网站', icon: 'sites' },
  { id: 'software', label: '已安装软件', icon: 'software' },
  { id: 'logs', label: '日志监控', icon: 'logs' },
  { id: 'snapshots', label: '快照备份', icon: 'snapshots' },
]
const bottomItems = [
  { id: 'packages', label: '软件仓库', icon: 'packages' },
  { id: 'settings', label: '系统设置', icon: 'settings' },
]
</script>

<aside class="sidebar">
  <div class="logo">
    <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
      <rect width="32" height="32" rx="8" fill="var(--color-accent)"/>
      <path d="M10 16h12M16 10v12" stroke="#fff" stroke-width="2.5" stroke-linecap="round"/>
    </svg>
    <span class="logo-text">DevStack</span>
  </div>

  <nav class="nav">
    {#each navItems as item}
      <button class="nav-item" class:active={currentView === item.id} onclick={() => dispatch('navigate', item.id)}>
        <span class="nav-icon">
          {#if item.icon === 'dashboard'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
          {:else if item.icon === 'sites'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 21h18M5 21V7l8-4v18M19 21V11l-6-4"/><path d="M9 9v.01M9 12v.01M9 15v.01M9 18v.01"/></svg>
          {:else if item.icon === 'software'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>
          {:else if item.icon === 'environment'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"/></svg>
          {:else if item.icon === 'logs'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg>
          {:else if item.icon === 'snapshots'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8V5a2 2 0 0 1 2-2h3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M21 16v3a2 2 0 0 1-2 2h-3"/><circle cx="12" cy="12" r="3.5"/></svg>
          {/if}
        </span>
        <span class="nav-label">{item.label}</span>
      </button>
    {/each}
  </nav>

  <div class="nav-bottom">
    {#each bottomItems as item}
      <button class="nav-item" class:active={currentView === item.id} onclick={() => dispatch('navigate', item.id)}>
        <span class="nav-icon">
          {#if item.icon === 'packages'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16.5 9.4 7.5 4.21M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>
          {:else if item.icon === 'settings'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
          {/if}
        </span>
        <span class="nav-label">{item.label}</span>
      </button>
    {/each}
  </div>

  <div class="sidebar-footer">
    <button class="user-info" onclick={() => (profileOpen = true)} title="个人资料">
      <div class="avatar">D</div>
      <div class="user-details">
        <span class="user-name">DevStack User</span>
        <span class="user-plan">Community Edition</span>
      </div>
    </button>
  </div>
</aside>

<UserProfile open={profileOpen} onclose={() => (profileOpen = false)} />

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
    border-radius: 8px;
    background: transparent;
    color: var(--sidebar-text);
    transition: all 0.12s;
    text-align: left;
    width: 100%;
    font-size: 13.5px;
    border: none;
    cursor: pointer;
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
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    opacity: 0.7;
  }
  .nav-icon svg {
    width: 18px;
    height: 18px;
  }
  .nav-item.active .nav-icon {
    opacity: 1;
  }
  .sidebar-footer {
    padding: 14px 16px;
    border-top: 1px solid var(--sidebar-border);
  }
  .user-info {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 6px;
    margin: -6px;
    border: none;
    border-radius: 8px;
    background: transparent;
    text-align: left;
    cursor: pointer;
    transition: background 0.12s;
  }
  .user-info:hover {
    background: var(--sidebar-hover);
  }
  .avatar {
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: var(--sidebar-avatar-bg);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--sidebar-avatar-text);
    font-size: 14px;
    font-weight: 600;
    flex-shrink: 0;
  }
  .user-details {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .user-name {
    color: var(--sidebar-user-name);
    font-size: 13px;
    font-weight: 500;
  }
  .user-plan {
    color: var(--sidebar-user-plan);
    font-size: 11px;
  }
</style>
