<script lang="ts">
import { createEventDispatcher, onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

const dispatch = createEventDispatcher()
let step = 0
let installDir = ''
let selectedRole = 'web'

let packages = [
  { id: 'nginx', name: 'Nginx', checked: true, versions: [], selected: '' },
  { id: 'mysql', name: 'MySQL', checked: true, versions: [], selected: '' },
  { id: 'redis', name: 'Redis', checked: true, versions: [], selected: '' },
  { id: 'php', name: 'PHP', checked: true, versions: [], selected: '' },
  { id: 'node', name: 'Node.js', checked: false, versions: [], selected: '' },
  { id: 'python', name: 'Python', checked: false, versions: [], selected: '' },
]

const roles = [
  { id: 'web', label: 'Web 开发者', icon: '🌐' },
  { id: 'fullstack', label: '全栈开发者', icon: '⚡' },
  { id: 'php', label: 'PHP 开发者', icon: '🐘' },
  { id: 'node', label: 'Node.js 开发者', icon: '⬢' },
  { id: 'python', label: 'Python 开发者', icon: '🐍' },
  { id: 'custom', label: '自定义选择', icon: '⚙' },
]

onMount(async () => {
  const settings = await api.getSettings()
  if (settings.installDir) installDir = settings.installDir
  for (const pkg of packages) {
    const releases = await api.getAvailableVersions(pkg.id)
    if (releases?.length > 0) {
      pkg.versions = releases.map((r) => r.version)
      pkg.selected = releases[releases.length - 1].version
    }
  }
})

function selectRole(id) {
  selectedRole = id
  if (id === 'web') packages.forEach((p) => (p.checked = ['nginx', 'mysql', 'redis', 'php'].includes(p.id)))
  else if (id === 'fullstack') packages.forEach((p) => (p.checked = true))
  else if (id === 'php') packages.forEach((p) => (p.checked = ['nginx', 'mysql', 'redis', 'php'].includes(p.id)))
  else if (id === 'node') packages.forEach((p) => (p.checked = ['nginx', 'mysql', 'redis', 'node'].includes(p.id)))
  else if (id === 'python') packages.forEach((p) => (p.checked = ['nginx', 'mysql', 'redis', 'python'].includes(p.id)))
}

let installProgress = []
let installing = false
let currentInstall = ''

async function startInstall() {
  step = 2
  installing = true
  const settings = await api.getSettings()
  settings.installDir = installDir
  await api.saveSettings(settings)
  const toInstall = packages.filter((p) => p.checked)
  for (let i = 0; i < toInstall.length; i++) {
    const pkg = toInstall[i]
    currentInstall = `${pkg.name} ${pkg.selected}`
    installProgress = [...installProgress, { name: `${pkg.name} ${pkg.selected}`, status: 'installing' }]
    await safeInvoke(
      async () => {
        await api.downloadService(pkg.id, pkg.selected)
        installProgress[i].status = 'done'
      },
      {
        successMsg: `${pkg.name} ${pkg.selected} 安装成功`,
        errorPrefix: `${pkg.name} 安装失败`,
        showSuccessToast: true,
      },
    )
    installProgress = [...installProgress]
  }
  installing = false
  step = 3
}

function finish() {
  dispatch('complete')
}
</script>

<div class="h-screen flex flex-col bg-white" data-theme="light">
  <div class="bg-gradient-to-r from-slate-900 via-indigo-950 to-indigo-600 px-12 py-8">
    <div class="flex items-center gap-3.5">
      <svg width="40" height="40" viewBox="0 0 32 32" fill="none"><rect width="32" height="32" rx="8" fill="rgba(255,255,255,0.2)"/><path d="M10 16h12M16 10v12" stroke="#fff" stroke-width="2.5" stroke-linecap="round"/></svg>
      <h1 class="text-white text-3xl font-bold">DevStack</h1>
      <p class="text-white/70 text-sm ml-auto italic">The Essential Tool For Web Developers</p>
    </div>
  </div>

  {#if step === 0}
    <div class="flex-1 overflow-y-auto px-12 py-8 flex flex-col items-center justify-center text-center">
      <h2 class="text-2xl font-bold mb-2 text-base-content">欢迎使用 DevStack</h2>
      <p class="text-base-content/60 text-sm mb-6">让我们开始设置您的开发环境</p>
      <div class="text-left mb-8 max-w-sm">
        <p class="font-medium mb-2 text-base-content">DevStack 为您提供：</p>
        <ul class="list-disc pl-5 text-base-content/70 space-y-1 text-sm">
          <li>一键安装开发环境</li>
          <li>多版本预配置的开发者工具</li>
          <li>自动配置和启动服务</li>
          <li>完整的本地开发工具栈</li>
        </ul>
      </div>
      <button class="btn btn-primary btn-lg min-w-[160px]" on:click={() => step = 1}>下一步</button>
    </div>

  {:else if step === 1}
    <div class="flex-1 overflow-y-auto px-12 py-8">
      <div class="grid grid-cols-[200px_220px_1fr] gap-6 min-h-full">
        <div>
          <h3 class="text-lg font-semibold mb-2 text-base-content">让我们开始安装 DevStack</h3>
          <p class="text-base-content/60 text-sm mb-4">点击按钮，初始化你的 DevStack。</p>
          <div class="mb-3"><span class="block text-xs text-base-content/60 mb-1">安装路径:</span><span class="text-sm font-mono text-base-content">{installDir}</span></div>
          <p class="text-xs text-base-content/60 leading-relaxed">所有软件包均安装到此目录，不影响系统现有软件，请放心安装。</p>
        </div>

        <div>
          <h4 class="text-sm font-semibold mb-2.5 text-base-content">请选择你的角色</h4>
          <div class="flex flex-col gap-1">
            {#each roles as role}
              <button class="flex items-center gap-2 px-3 py-2 rounded-lg text-left text-sm w-full transition-colors border-none {selectedRole === role.id ? 'bg-primary text-primary-content' : 'text-base-content/70 hover:bg-base-200'}"
                on:click={() => selectRole(role.id)}>
                <span>{role.icon}</span><span>{role.label}</span>
              </button>
            {/each}
          </div>
        </div>

        <div class="flex flex-col">
          <div class="mb-4"><label class="block text-xs text-base-content/60 mb-1">默认 Web 服务器</label><select class="select select-bordered select-sm max-w-[200px]"><option>Nginx</option></select></div>
          <div class="flex-1 flex flex-col gap-1.5">
            {#each packages as pkg}
              <label class="flex items-center gap-2.5 py-1 cursor-pointer">
                <input type="checkbox" class="checkbox checkbox-sm checkbox-primary" bind:checked={pkg.checked} />
                <span class="text-sm min-w-[80px]">{pkg.name}</span>
                <select class="select select-bordered select-xs" bind:value={pkg.selected}>
                  {#each pkg.versions as v}<option>{v}</option>{/each}
                </select>
              </label>
            {/each}
          </div>
          <div class="pt-4 text-right"><button class="btn btn-primary btn-lg min-w-[160px]" on:click={startInstall}>开始安装</button></div>
        </div>
      </div>
    </div>

  {:else if step === 2}
    <div class="flex-1 overflow-y-auto px-12 py-8 text-center pt-12">
      <h2 class="text-xl font-bold mb-2 text-base-content">正在安装软件包</h2>
      <p class="text-base-content/60 text-sm mb-6">{currentInstall}</p>
      <div class="max-w-md mx-auto text-left">
        {#each installProgress as item}
          <div class="flex items-center gap-2.5 py-2 border-b border-base-200 last:border-none">
            <span class="w-5 text-center text-sm">
              {#if item.status === 'done'}✅
              {:else if item.status === 'installing'}⏳
              {:else if item.status === 'error'}❌
              {:else}○
              {/if}
            </span>
            <span class="flex-1 text-sm text-base-content">{item.name}</span>
          </div>
        {/each}
      </div>
    </div>

  {:else if step === 3}
    <div class="flex-1 overflow-y-auto px-12 py-8 flex flex-col items-center justify-center text-center">
      <h2 class="text-2xl font-bold text-success mb-2">安装完成！</h2>
      <p class="text-base-content/60 text-sm mb-6">您的开发环境已准备就绪</p>
      <div class="text-left mb-6 max-w-sm">
        <p class="font-medium mb-2 text-base-content">接下来您可以：</p>
        <ol class="list-decimal pl-5 text-base-content/70 space-y-1 text-sm">
          <li>访问仪表盘 - 查看系统状态</li>
          <li>管理网站 - 添加您的第一个项目</li>
          <li>配置服务 - 调整服务设置</li>
        </ol>
      </div>
      <button class="btn btn-primary btn-lg min-w-[160px]" on:click={finish}>完成</button>
    </div>
  {/if}
</div>
