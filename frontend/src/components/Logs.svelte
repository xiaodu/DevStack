<script lang="ts">
import { onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'

let activeTab = $state('app')
let services = $state<any[]>([])
let selectedService = $state<string | null>(null)
let logEntries = $state<any[]>([])
let logFiles = $state<string[]>([])
let selectedLogFile = $state<string | null>(null)
let autoScroll = $state(true)
let logContainer = $state<HTMLElement>()
let loading = $state(true)
let refreshInterval = $state<number>()
let loadedService = $state<string | null>(null) // 当前已加载日志所属服务，用于区分"切换服务"与"刷新"

const LOG_CAP = 800 // 数据上限，防止长时间追加把内存/数组撑爆
const RENDER_STEP = 300 // 每次"加载更早"新增渲染行数
let renderWindow = $state(200) // 当前渲染的最近行数（底部窗口），限制一次性进 DOM 的节点数

// 底部锚定窗口：只渲染最近 renderWindow 行；更早的行按需加载，避免 800 行全量进 DOM。
let visibleEntries = $derived(
  renderWindow >= logEntries.length ? logEntries : logEntries.slice(logEntries.length - renderWindow),
)
let hasEarlier = $derived(logEntries.length > visibleEntries.length)

function loadEarlier() {
  renderWindow = Math.min(renderWindow + RENDER_STEP, logEntries.length)
}

function toEntries(lines: string[]) {
  return lines.slice(-LOG_CAP).map((line, i) => ({ key: `${i}:${line}`, time: extractLogLevel(line), content: line }))
}

// 与现有列表逐项比对，内容完全一致时保持原数组引用，避免每 3s 无谓重渲染 500+ 行（P2）。
function applyEntries(next: any[]) {
  const prev = logEntries
  if (prev.length === next.length && prev.every((e, i) => e.content === next[i].content)) {
    return
  }
  logEntries = next
}

function extractLogLevel(line: string) {
  const m = line.match(/level=(\w+)/)
  return m ? m[1] : ''
}

async function loadServices() {
  try {
    services = await api.getServices()
    if (!selectedService && services.length > 0) {
      selectedService = services[0].id
    }
  } catch (e) {
    console.error(e)
  }
}

async function loadServiceLogs() {
  if (!selectedService) {
    loading = false
    logEntries = []
    loadedService = null
    return
  }
  // 仅切换服务时先清空；同服务刷新保持旧内容，避免"清空→重填"闪烁（P2）。
  const switching = loadedService !== selectedService
  if (switching) {
    logEntries = []
    renderWindow = 200
  }
  try {
    const logs = await api.getLogs(selectedService)
    const next = (logs || []).slice(-LOG_CAP).map((e: any, i: number) => ({
      key: `${i}:${e?.content ?? ''}`,
      time: e?.time ?? '',
      content: e?.content ?? '',
    }))
    if (switching) logEntries = next
    else applyEntries(next)
    loadedService = selectedService
  } catch (e) {
    console.error(e)
    if (switching) logEntries = []
  } finally {
    loading = false
  }
}

async function loadAppLogs() {
  loading = true
  renderWindow = 200
  try {
    logFiles = await api.getLogFiles()
    if (!selectedLogFile && logFiles.length > 0) selectedLogFile = logFiles[logFiles.length - 1]
    if (selectedLogFile)
      logEntries = toEntries((await api.readLogFile(selectedLogFile)) || [])
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

async function loadRecentAppLogs() {
  try {
    // 轮询走增量语义：内容没变就不重排 DOM（P2）。
    applyEntries(toEntries((await api.getRecentLogLines(LOG_CAP)) || []))
  } catch (e) {
    console.error(e)
  }
}

async function clearLogs() {
  logEntries = []
  try {
    await api.clearLogs()
    await loadAppLogs()
  } catch (e) {
    console.error(e)
  }
}

function switchTab(tab: string) {
  activeTab = tab
  loading = true
  if (tab === 'app') loadAppLogs()
  else loadServices().then(() => loadServiceLogs())
}

$effect(() => {
  // 读取 logEntries 建立依赖：日志内容更新后自动滚动到底部（F10）
  logEntries
  if (autoScroll && logContainer) logContainer.scrollTop = logContainer.scrollHeight
})

function handleScroll() {
  if (!logContainer) return
  const { scrollTop, scrollHeight, clientHeight } = logContainer
  autoScroll = scrollHeight - scrollTop - clientHeight < 30
}

function lineClass(content: string) {
  if (/level=(ERROR|FATAL)/.test(content)) return 'text-red-300'
  if (/level=WARN/.test(content)) return 'text-amber-300'
  if (/level=DEBUG/.test(content)) return 'text-slate-500'
  return 'text-slate-300'
}

onMount(async () => {
  await loadAppLogs()
  refreshInterval = setInterval(() => {
    if (activeTab === 'app') loadRecentAppLogs()
    else loadServiceLogs()
  }, 3000)
})
onDestroy(() => clearInterval(refreshInterval))
</script>

<div class="flex h-full min-h-[520px] flex-col gap-4">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-xl font-bold">实时日志</h1>
      <p class="mt-1 text-sm text-base-content/60">应用与组件输出，每 3 秒自动刷新</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <div class="flex rounded-lg bg-base-200 p-1">
        <button class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors {activeTab === 'app' ? 'bg-base-100 text-base-content shadow-sm' : 'text-base-content/60 hover:text-base-content'}" onclick={() => switchTab('app')}>应用日志</button>
        <button class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors {activeTab === 'service' ? 'bg-base-100 text-base-content shadow-sm' : 'text-base-content/60 hover:text-base-content'}" onclick={() => switchTab('service')}>组件日志</button>
      </div>
      {#if activeTab === 'app'}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedLogFile} onchange={(e) => { selectedLogFile = (e.currentTarget as HTMLSelectElement).value; loadAppLogs() }}>
          {#each logFiles as file}<option value={file}>{file.split('/').pop()}</option>{/each}
        </select>
        <button class="btn btn-sm btn-ghost text-error" onclick={clearLogs}>清除</button>
      {:else}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedService} onchange={(e) => { selectedService = (e.currentTarget as HTMLSelectElement).value; loadServiceLogs() }}>
          {#each services as svc}<option value={svc.id}>{svc.name}</option>{/each}
        </select>
      {/if}
      <button class="btn btn-sm btn-outline" onclick={activeTab === 'app' ? loadAppLogs : loadServiceLogs}>刷新</button>
      <button class="btn btn-sm {autoScroll ? 'btn-outline' : 'btn-ghost text-warning'}" onclick={() => autoScroll = !autoScroll}>{autoScroll ? '自动滚动' : '手动滚动'}</button>
    </div>
  </div>

  <div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-2xl border border-slate-700 bg-slate-900 shadow-sm">
    <div class="flex items-center gap-2 border-b border-slate-700/70 px-4 py-2 text-[11px] text-slate-400">
      <span class="flex gap-1.5">
        <span class="h-2.5 w-2.5 rounded-full bg-red-400/80"></span>
        <span class="h-2.5 w-2.5 rounded-full bg-amber-400/80"></span>
        <span class="h-2.5 w-2.5 rounded-full bg-emerald-400/80"></span>
      </span>
      <span class="ml-2">实时输出</span>
      <span class="ml-auto">{logEntries.length} 行</span>
      <span class="text-slate-500">{autoScroll ? '跟随最新' : '已暂停跟随'}</span>
    </div>
    <div class="flex-1 overflow-y-auto p-4 font-mono text-xs leading-relaxed" bind:this={logContainer} onscroll={handleScroll}>
      {#if loading}
        <div class="py-10 text-center text-slate-500">加载日志中...</div>
      {:else if logEntries.length === 0}
        <div class="py-10 text-center text-slate-500">暂无日志</div>
      {:else}
        {#if hasEarlier}
          <button class="btn btn-xs btn-outline btn-info mb-2 w-full border-slate-600 text-slate-400 hover:bg-slate-700" onclick={loadEarlier}>
            ↑ 加载更早（还有 {logEntries.length - visibleEntries.length} 行未显示）
          </button>
        {/if}
        {#each visibleEntries as entry}
          <div class="flex gap-2.5 hover:bg-slate-800/60">
            {#if entry.time}
              <span class="shrink-0 text-slate-500">[{entry.time}]</span>
            {/if}
            <span class="break-all {lineClass(entry.content)}">{entry.content}</span>
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>
