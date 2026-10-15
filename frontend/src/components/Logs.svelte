<script lang="ts">
import { onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'

let activeTab = $state('app')
let services = $state([])
let selectedService = $state(null)
let logEntries = $state([])
let logFiles = $state([])
let selectedLogFile = $state(null)
let autoScroll = $state(true)
let logContainer = $state()
let loading = $state(true)
let refreshInterval = $state()
let loadedService = $state(null) // 当前已加载日志所属服务，用于区分"切换服务"与"刷新"

const LOG_CAP = 800 // 上限，防止长时间追加把 DOM 撑爆

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

function extractLogLevel(line) {
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
  if (switching) logEntries = []
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

function switchTab(tab) {
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

onMount(async () => {
  await loadAppLogs()
  refreshInterval = setInterval(() => {
    if (activeTab === 'app') loadRecentAppLogs()
    else loadServiceLogs()
  }, 3000)
})
onDestroy(() => clearInterval(refreshInterval))
</script>

<div class="flex flex-col h-full">
  <div class="flex justify-between items-center mb-3 shrink-0">
    <div>
      <h1 class="text-xl font-bold">日志查看</h1>
      <p class="text-sm text-base-content/60">实时监控服务和应用日志</p>
    </div>
    <div class="flex gap-2 items-center">
      <div class="join">
        <button class="join-item btn btn-sm" class:btn-active={activeTab === 'app'} onclick={() => switchTab('app')}>应用日志</button>
        <button class="join-item btn btn-sm" class:btn-active={activeTab === 'service'} onclick={() => switchTab('service')}>服务日志</button>
      </div>
      {#if activeTab === 'app'}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedLogFile} onchange={(e) => { selectedLogFile = e.target.value; loadAppLogs() }}>
          {#each logFiles as file}<option value={file}>{file.split('/').pop()}</option>{/each}
        </select>
        <button class="btn btn-sm btn-ghost text-error" onclick={clearLogs}>清除</button>
      {:else}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedService} onchange={(e) => { selectedService = e.target.value; loadServiceLogs() }}>
          {#each services as svc}<option value={svc.id}>{svc.name}</option>{/each}
        </select>
      {/if}
      <button class="btn btn-sm btn-outline" onclick={activeTab === 'app' ? loadAppLogs : loadServiceLogs}>刷新</button>
      <button class="btn btn-sm btn-outline" onclick={() => autoScroll = !autoScroll}>{autoScroll ? '自动滚动' : '手动滚动'}</button>
    </div>
  </div>

  <div class="flex-1 bg-slate-800 border border-slate-700 rounded-xl p-3.5 font-mono text-xs overflow-y-auto leading-relaxed" bind:this={logContainer} onscroll={handleScroll}>
    {#if loading}
      <div class="text-center py-10 text-slate-500">加载日志中...</div>
    {:else if logEntries.length === 0}
      <div class="text-center py-10 text-slate-500">暂无日志</div>
    {:else}
      {#each logEntries as entry}
        <div class="flex gap-2.5">
          {#if entry.time}
            <span class="text-slate-500 shrink-0">[{entry.time}]</span>
          {/if}
          <span class="text-slate-300 break-all">{entry.content}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>
