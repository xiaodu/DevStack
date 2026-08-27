<script lang="ts">
import { createEventDispatcher, onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

const dispatch = createEventDispatcher()
let step = $state(0)
let installDir = $state('')

// 预设定义（与后端 internal/environment/manager.go 保持一致）
const presets = [
  {
    id: 'builtin-lnmp-php74',
    label: '经典 LNMP (PHP 7.4)',
    icon: '🐘',
    description: '兼容绝大多数国内 PHP 开源系统（WordPress、DedeCMS、ThinkPHP 5 等）',
    tags: ['推荐', '建站', '兼容性'],
    units: [
      { kind: 'nginx', version: '1.20.2', name: 'Nginx' },
      { kind: 'mysql', version: '5.7.44', name: 'MySQL' },
      { kind: 'php', version: '7.4.33', name: 'PHP' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
    ],
  },
  {
    id: 'builtin-modern-php8',
    label: '现代 PHP 全栈 (PHP 8.3)',
    icon: '🚀',
    description: '面向 Laravel / Symfony / ThinkPHP 8 等现代框架的高性能组合',
    tags: ['PHP8', 'Laravel', '高性能'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
      { kind: 'mysql', version: '8.0.39', name: 'MySQL' },
      { kind: 'php', version: '8.3.12', name: 'PHP' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
    ],
  },
  {
    id: 'builtin-node-web',
    label: 'Node.js 全栈 (Node 20)',
    icon: '⬢',
    description: '适用于 Vue/Next/Nuxt SSR、Koa/Nest/Egg 服务端项目',
    tags: ['Node', 'SSR', 'Vue', 'React'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
      { kind: 'node', version: '20.18.0', name: 'Node.js' },
    ],
  },
  {
    id: 'builtin-python-web',
    label: 'Python Web (Python 3.11)',
    icon: '🐍',
    description: '适用于 Django / Flask / FastAPI / Tornado 等 Python Web 框架',
    tags: ['Python', 'Django', 'FastAPI'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
      { kind: 'python', version: '3.11.8', name: 'Python' },
    ],
  },
  {
    id: 'builtin-static-nginx',
    label: '纯静态站点 (Nginx)',
    icon: '🌐',
    description: '无后端依赖，仅用 Nginx 托管纯静态 HTML、打包后的前端产物',
    tags: ['静态', '纯前端', '轻量'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
    ],
  },
]

let selectedPreset = $state<string | null>(null)
let showCustom = $state(false)

let packages = $state([
  { id: 'nginx', name: 'Nginx', checked: true, versions: [], selected: '', port: 80 },
  { id: 'mysql', name: 'MySQL', checked: true, versions: [], selected: '', port: 3306 },
  { id: 'redis', name: 'Redis', checked: true, versions: [], selected: '', port: 6379 },
  { id: 'php', name: 'PHP', checked: true, versions: [], selected: '', port: 9000 },
  { id: 'node', name: 'Node.js', checked: false, versions: [], selected: '', port: 0 },
  { id: 'python', name: 'Python', checked: false, versions: [], selected: '', port: 0 },
])

let portConflicts = $state<Record<string, { port: number; inUse: boolean; message: string }>>({})
let checkingPorts = $state(false)

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
  // 默认选中推荐预设
  selectedPreset = 'builtin-modern-php8'
  applyPreset(selectedPreset)
})

function applyPreset(presetId: string) {
  const preset = presets.find((p) => p.id === presetId)
  if (!preset) return

  // 重置所有包
  packages.forEach((p) => {
    p.checked = false
    p.selected = ''
  })

  // 应用预设的 units
  for (const unit of preset.units) {
    const pkg = packages.find((p) => p.id === unit.kind)
    if (pkg) {
      pkg.checked = true
      // 如果该版本在可用列表中则选中，否则用最新版
      if (pkg.versions.includes(unit.version)) {
        pkg.selected = unit.version
      } else if (pkg.versions.length > 0) {
        pkg.selected = pkg.versions[pkg.versions.length - 1]
      }
    }
  }

  // 预设应用后自动检查端口
  checkPorts()
}

function selectPreset(presetId: string) {
  selectedPreset = presetId
  showCustom = false
  applyPreset(presetId)
}

function switchToCustom() {
  showCustom = true
  selectedPreset = null
}

function togglePackage(pkgId: string) {
  const pkg = packages.find((p) => p.id === pkgId)
  if (pkg) {
    pkg.checked = !pkg.checked
    if (pkg.checked && !pkg.selected && pkg.versions.length > 0) {
      pkg.selected = pkg.versions[pkg.versions.length - 1]
    }
    // 手动修改后标记为自定义模式
    if (selectedPreset) {
      showCustom = true
    }
  }
  checkPorts()
}

function changeVersion(pkgId: string, version: string) {
  const pkg = packages.find((p) => p.id === pkgId)
  if (pkg) {
    pkg.selected = version
    if (selectedPreset) {
      showCustom = true
    }
  }
}

async function pickInstallDir() {
  try {
    const path = await api.selectInstallDir(installDir || undefined)
    if (path) {
      installDir = path
      await checkPorts()
    }
  } catch (e) {
    console.error('选择目录失败:', e)
  }
}

async function checkPorts() {
  const toCheck = packages.filter((p) => p.checked && p.port > 0).map((p) => p.id)
  if (toCheck.length === 0) return
  checkingPorts = true
  try {
    const conflicts = await api.checkSetupPorts(toCheck)
    const map: Record<string, { port: number; inUse: boolean; message: string }> = {}
    for (const c of conflicts) {
      map[c.service] = { port: c.port, inUse: c.inUse, message: c.message }
    }
    portConflicts = map
  } catch (e) {
    console.error('端口检查失败:', e)
  } finally {
    checkingPorts = false
  }
}

let installProgress = $state<{ name: string; status: 'pending' | 'installing' | 'done' | 'error'; progress?: number }[]>([])
let installing = $state(false)
let currentInstall = $state('')

async function startInstall() {
  if (!installDir) {
    addToast('请先选择安装目录', 'error')
    return
  }
  const toInstall = packages.filter((p) => p.checked)
  if (toInstall.length === 0) {
    addToast('请至少选择一个组件', 'error')
    return
  }
  await checkPorts()
  const hasBlockingConflict = Object.values(portConflicts).some((c) => c.inUse)
  if (hasBlockingConflict) {
    addToast('存在端口冲突，请解决后再继续', 'error')
    return
  }

  step = 2
  installing = true
  installProgress = toInstall.map((p) => ({ name: `${p.name} ${p.selected}`, status: 'pending' as const }))

  try {
    await safeInvoke(
      async () => {
        const services = toInstall.map((p) => p.id)
        await api.completeSetup({ installDir, services })
      },
      {
        successMsg: '初始化完成',
        errorPrefix: '初始化失败',
        showSuccessToast: false,
      },
    )
    installProgress = installProgress.map((p) => ({ ...p, status: 'done' as const }))
    step = 3
  } catch (e) {
    console.error('安装失败:', e)
    installProgress = installProgress.map((p) => (p.status === 'installing' ? { ...p, status: 'error' as const } : p))
  } finally {
    installing = false
  }
}

onMount(() => {
  api.debugLog('SetupWizard onMount 触发, step=' + step)
  const unsub = api.onSetupDownloadProgress((data: any) => {
    const idx = installProgress.findIndex((p) => p.name.includes(data.service) && p.name.includes(data.version))
    if (idx >= 0) {
      installProgress[idx] = {
        ...installProgress[idx],
        status: 'installing',
        progress: data.total > 0 ? Math.round((data.downloaded / data.total) * 100) : 0,
      }
      installProgress = [...installProgress]
    }
  })
  return () => unsub()
})

function finish() {
  dispatch('complete')
}
</script>

<div class="fixed inset-0 z-[9999] h-screen flex flex-col bg-white" data-theme="light">
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
      <button class="btn btn-primary btn-lg min-w-[160px]" onclick={() => step = 1}>下一步</button>
    </div>

  {:else if step === 1}
    <div class="flex-1 overflow-y-auto px-12 py-8">
      <div class="grid grid-cols-[280px_1fr] gap-6 min-h-full">
        <!-- 左侧：安装目录 + 端口检查 -->
        <div>
          <h3 class="text-lg font-semibold mb-2 text-base-content">让我们开始安装 DevStack</h3>
          <p class="text-base-content/60 text-sm mb-4">选择安装目录并检查端口占用。</p>
          
          <div class="mb-6">
            <div class="flex gap-2">
              <label class="block text-xs text-base-content/60 mb-1 flex-1">
                安装路径
                <input 
                  class="input input-bordered font-mono text-sm mt-1" 
                  value={installDir} 
                  readonly 
                  placeholder={installDir ? '' : '点击右侧按钮选择目录'} 
                />
              </label>
              <button class="btn btn-outline btn-sm self-end" onclick={pickInstallDir} disabled={checkingPorts}>
                {checkingPorts ? '检查中...' : '浏览…'}
              </button>
            </div>
            <p class="text-xs text-base-content/60 leading-relaxed mt-1">建议：独立盘符根目录（如 D:\DevStack），避免权限与中文路径问题</p>
          </div>

          <div class="mb-3">
            <span class="block text-xs text-base-content/60 mb-1">端口占用检查</span>
            <div class="flex flex-wrap gap-1">
              {#each packages.filter(p => p.port > 0) as pkg}
                <span class="px-2 py-1 text-xs rounded border {portConflicts[pkg.id]?.inUse ? 'border-error bg-error/10 text-error' : 'border-base-300 text-base-content/70'}">
                  {pkg.name}:{pkg.port}
                  {#if portConflicts[pkg.id]?.inUse} ⚠️{/if}
                </span>
              {/each}
            </div>
            {#if Object.keys(portConflicts).length > 0}
              <div class="mt-2 text-xs text-base-content/70 space-y-1">
                {#each Object.entries(portConflicts) as [svc, info]}
                  <span class="block {info.inUse ? 'text-warning' : 'text-success'}">{info.message}</span>
                {/each}
              </div>
            {/if}
          </div>
        </div>

        <!-- 右侧：预设选择 + 组件微调 -->
        <div class="flex flex-col">
          <h4 class="text-sm font-semibold mb-2.5 text-base-content">选择技术栈预设</h4>
          
          <!-- 预设卡片网格 -->
          <div class="grid grid-cols-2 gap-3 mb-4">
            {#each presets as preset}
              <button 
                class="p-3 border rounded-lg text-left transition-all {selectedPreset === preset.id && !showCustom ? 'border-primary bg-primary/5 shadow-sm' : 'border-base-300 hover:border-primary'}"
                onclick={() => selectPreset(preset.id)}
              >
                <div class="flex items-start gap-2">
                  <span class="text-2xl mt-0.5">{preset.icon}</span>
                  <div class="flex-1 min-w-0">
                    <div class="font-medium text-sm truncate">{preset.label}</div>
                    <div class="text-xs text-base-content/60 mt-0.5 line-clamp-1">{preset.description}</div>
                    <div class="flex flex-wrap gap-1 mt-1.5">
                      {#each preset.tags as tag}
                        <span class="px-1.5 py-0.5 text-[10px] bg-base-200 rounded">{tag}</span>
                      {/each}
                    </div>
                  </div>
                </div>
                <div class="mt-2 text-xs text-base-content/60 flex flex-wrap gap-1">
                  {#each preset.units as u}
                    <span class="px-1.5 py-0.5 bg-base-100 rounded">{u.kind}@{u.version}</span>
                  {/each}
                </div>
              </button>
            {/each}
            
            <!-- 自定义组装卡片 -->
            <button 
              class="p-3 border rounded-lg text-left transition-all flex flex-col items-center justify-center gap-2 {showCustom ? 'border-primary bg-primary/5' : 'border-dashed border-base-300 hover:border-primary'}"
              onclick={switchToCustom}
            >
              <span class="text-3xl">⚙️</span>
              <span class="font-medium">自定义组装</span>
              <span class="text-xs text-base-content/50">自由选择每个组件及版本</span>
            </button>
          </div>

          <!-- 当前预设提示 + 改为自定义按钮 -->
          {#if selectedPreset && !showCustom}
            <div class="mb-4 p-3 bg-info/10 border border-info/30 rounded text-sm">
              <span class="font-medium">当前预设：</span>
              {presets.find(p => p.id === selectedPreset)?.label}
              <button class="ml-4 text-primary underline" onclick={switchToCustom}>改为自定义组装</button>
            </div>
          {/if}

          <!-- 组件列表（预设预填，可微调） -->
          <div class="flex-1 flex flex-col gap-1.5 overflow-y-auto">
            <div class="flex items-center gap-2 text-xs text-base-content/60 px-1">
              <span class="w-8">组件</span>
              <span class="w-10">端口</span>
              <span class="flex-1">版本</span>
              <span class="w-24">状态</span>
            </div>
            {#each packages as pkg}
              <label class="flex items-center gap-2.5 py-1.5 px-2 rounded hover:bg-base-100 transition-colors">
                <input 
                  type="checkbox" 
                  class="checkbox checkbox-sm checkbox-primary" 
                  checked={pkg.checked}
                  onchange={() => togglePackage(pkg.id)}
                />
                <span class="text-sm min-w-[80px] font-medium">{pkg.name}</span>
                {#if pkg.port > 0}
                  <span class="text-xs text-base-content/50 w-10 text-center">:{pkg.port}</span>
                {:else}
                  <span class="w-10"></span>
                {/if}
                {#if pkg.versions.length > 0}
                  <select 
                    class="select select-bordered select-xs flex-1 max-w-xs" 
                    value={pkg.selected}
                    onchange={(e) => changeVersion(pkg.id, (e.target as HTMLSelectElement).value)}
                    disabled={!pkg.checked}
                  >
                    {#each pkg.versions as v}
                      <option value={v}>{v}</option>
                    {/each}
                  </select>
                {:else}
                  <span class="text-xs text-base-content/50 flex-1">无可用版本</span>
                {/if}
                {#if portConflicts[pkg.id]?.inUse}
                  <span class="text-xs text-warning w-24">⚠ 端口冲突</span>
                {:else if pkg.checked}
                  <span class="text-xs text-success w-24">✓ 就绪</span>
                {:else}
                  <span class="w-24"></span>
                {/if}
              </label>
            {/each}
          </div>

          <div class="pt-4 text-right border-t border-base-200">
            <button class="btn btn-primary btn-lg min-w-[160px]" onclick={startInstall} disabled={installing || !installDir}>
              {installing ? '安装中...' : '开始安装'}
            </button>
          </div>
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
            {#if item.progress !== undefined && item.status === 'installing'}
              <div class="w-32 h-1.5 bg-base-200 rounded-full overflow-hidden">
                <div class="h-full bg-primary transition-all" style="width: {item.progress}%"></div>
              </div>
              <span class="text-xs text-base-content/60 w-10 text-right">{item.progress}%</span>
            {/if}
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
      <button class="btn btn-primary btn-lg min-w-[160px]" onclick={finish}>完成</button>
    </div>
  {/if}
</div>