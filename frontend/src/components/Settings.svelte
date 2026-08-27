<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'

let settings = $state({ installDir: '', startOnBoot: false, minimizeToTray: true, theme: 'light', language: 'zh-CN' })
let systemInfo = $state({})
let loading = $state(true)
let configPath = $state('')
let saveTimer: ReturnType<typeof setTimeout> | null = null
const infoLabels: Record<string, string> = { platform: '操作系统', arch: '架构', appVersion: '版本', installDir: '安装目录' }

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

<div class="flex flex-col gap-4">
  {#if loading}
    <div class="text-center py-10 text-base-content/60">加载设置中...</div>
  {:else}
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div class="card bg-base-100 shadow-sm p-5">
        <h3 class="text-sm font-semibold mb-3.5 text-primary">基本设置</h3>
        <div class="flex flex-col gap-3">
          <label class="form-control w-full">
            <div class="label"><span class="label-text">安装目录</span></div>
            <input class="input input-bordered w-full" bind:value={settings.installDir} oninput={handleChange} placeholder="服务安装路径" />
          </label>
        </div>
      </div>

      <div class="card bg-base-100 shadow-sm p-5">
        <h3 class="text-sm font-semibold mb-3.5 text-primary">通用设置</h3>
        <div class="flex flex-col gap-3">
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-primary" bind:checked={settings.startOnBoot} onchange={handleChange} />
            <span class="text-sm">开机自启</span>
          </label>
          <p class="text-xs text-base-content/50 -mt-1">登录 Windows 后自动启动 DevStack</p>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-primary" bind:checked={settings.minimizeToTray} onchange={handleChange} />
            <span class="text-sm">最小化到系统托盘</span>
          </label>
          <p class="text-xs text-base-content/50 -mt-1">关闭窗口时最小化到系统托盘，可从托盘图标恢复或退出</p>
        </div>
      </div>

      <div class="card bg-base-100 shadow-sm p-5">
        <h3 class="text-sm font-semibold mb-3.5 text-primary">界面</h3>
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
      </div>

      <div class="card bg-base-100 shadow-sm p-5">
        <h3 class="text-sm font-semibold mb-3.5 text-primary">系统信息</h3>
        <div class="grid grid-cols-2 gap-2.5">
          {#each Object.entries(systemInfo) as [key, val]}
            <div class="flex flex-col gap-0.5">
              <span class="text-xs text-base-content/60">{infoLabels[key] || key}</span>
              <span class="text-sm break-all">{val}</span>
            </div>
          {/each}
        </div>
        {#if configPath}
          <div class="flex flex-col gap-0.5 mt-2.5">
            <span class="text-xs text-base-content/60">配置文件路径</span>
            <span class="text-xs font-mono text-base-content/70 break-all">{configPath}</span>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
