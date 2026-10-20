<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'

let settings = $state({ installDir: '', startOnBoot: false, minimizeToTray: true, theme: 'light', language: 'zh-CN' })
let systemInfo = $state({})
let loading = $state(true)
let configPath = $state('')
let saveTimer: ReturnType<typeof setTimeout> | null = null
const infoLabels: Record<string, string> = {
  platform: '操作系统',
  arch: '架构',
  appVersion: '版本',
  installDir: '安装目录',
}

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

function applyTheme(theme: string) {
  document.documentElement.setAttribute('data-theme', theme)
}

function scheduleSave() {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(async () => {
    try {
      await api.saveSettings(settings)
    } catch (e) {
      console.error('自动保存失败:', e)
    }
  }, 500)
}

function handleThemeChange() {
  applyTheme(settings.theme)
  scheduleSave()
}

function handleChange() {
  scheduleSave()
}

onMount(load)
</script>

<div class="flex flex-col gap-5">
  {#if loading}
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {#each [1, 2, 3, 4] as _}
        <div class="rounded-2xl border border-base-300 bg-base-100 p-5">
          <div class="h-4 w-24 rounded bg-base-200"></div>
          <div class="mt-4 space-y-2">
            <div class="h-10 rounded-lg bg-base-200"></div>
            <div class="h-10 rounded-lg bg-base-200"></div>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold">偏好设置</h1>
        <p class="mt-1 text-sm text-base-content/60">调整安装目录、启动行为与界面主题</p>
      </div>
      <span class="inline-flex items-center gap-1.5 rounded-full bg-success/10 px-2.5 py-1 text-xs text-success">
        <span class="h-1.5 w-1.5 rounded-full bg-success"></span>
        修改自动保存
      </span>
    </div>

    <div class="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
      <section class="rounded-2xl border border-base-300 bg-base-100 p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2.5">
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">基本设置</h2>
            <p class="text-xs text-base-content/50">组件与运行时的安装位置</p>
          </div>
        </div>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">安装目录</span></div>
          <input class="input input-bordered w-full font-mono text-sm" bind:value={settings.installDir} oninput={handleChange} placeholder="组件安装路径" />
        </label>
      </section>

      <section class="rounded-2xl border border-base-300 bg-base-100 p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2.5">
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-success/10 text-success">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.14.6.73 1 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">通用设置</h2>
            <p class="text-xs text-base-content/50">启动与窗口行为</p>
          </div>
        </div>
        <div class="flex flex-col gap-3">
          <div class="flex items-center justify-between rounded-xl border border-base-200 bg-base-200/40 px-3.5 py-3">
            <div>
              <p class="text-sm">开机自启</p>
              <p class="mt-0.5 text-xs text-base-content/50">登录 Windows 后自动启动 DevStack</p>
            </div>
            <input type="checkbox" class="toggle toggle-primary" bind:checked={settings.startOnBoot} onchange={handleChange} />
          </div>
          <div class="flex items-center justify-between rounded-xl border border-base-200 bg-base-200/40 px-3.5 py-3">
            <div>
              <p class="text-sm">最小化到系统托盘</p>
              <p class="mt-0.5 text-xs text-base-content/50">关闭窗口时隐藏到托盘，可从图标恢复或退出</p>
            </div>
            <input type="checkbox" class="toggle toggle-primary" bind:checked={settings.minimizeToTray} onchange={handleChange} />
          </div>
        </div>
      </section>

      <section class="rounded-2xl border border-base-300 bg-base-100 p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2.5">
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-warning/10 text-warning">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">界面</h2>
            <p class="text-xs text-base-content/50">主题与显示语言</p>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <label class="form-control w-full">
            <div class="label"><span class="label-text">主题</span></div>
            <select class="select select-bordered w-full" bind:value={settings.theme} onchange={handleThemeChange}>
              <option value="light">Light</option>
              <option value="corporate">Corporate</option>
              <option value="dim">Dim</option>
              <option value="night">Night</option>
              <option value="business">Business</option>
              <option value="dark">Dark</option>
            </select>
          </label>
          <label class="form-control w-full">
            <div class="label"><span class="label-text">语言</span></div>
            <select class="select select-bordered w-full" bind:value={settings.language} onchange={handleChange}>
              <option value="zh-CN">简体中文</option>
              <option value="en">English</option>
            </select>
          </label>
        </div>
      </section>

      <section class="rounded-2xl border border-base-300 bg-base-100 p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2.5">
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-base-200 text-base-content/60">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M8 2v4M16 2v4M3 10h18"/></svg>
          </span>
          <div>
            <h2 class="text-sm font-semibold">系统信息</h2>
            <p class="text-xs text-base-content/50">运行环境与配置文件位置</p>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          {#each Object.entries(systemInfo) as [key, val]}
            <div class="rounded-xl border border-base-200 bg-base-200/40 px-3.5 py-2.5">
              <span class="text-xs text-base-content/50">{infoLabels[key] || key}</span>
              <div class="mt-0.5 break-all text-sm">{val}</div>
            </div>
          {/each}
        </div>
        {#if configPath}
          <div class="mt-3 rounded-xl border border-base-200 px-3.5 py-2.5">
            <span class="text-xs text-base-content/50">配置文件路径</span>
            <div class="mt-0.5 break-all font-mono text-xs text-base-content/70">{configPath}</div>
          </div>
        {/if}
      </section>
    </div>
  {/if}
</div>
