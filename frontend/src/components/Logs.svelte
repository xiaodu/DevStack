<script lang="ts">
import { afterUpdate, onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'

let activeTab = 'app'
let services = []
let selectedService = null
let logEntries = []
let logFiles = []
let selectedLogFile = null
let autoScroll = true
let logContainer
let loading = true
let refreshInterval

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
    return
  }
  logEntries = []
  try {
    logEntries = await api.getLogs(selectedService)
  } catch (e) {
    console.error(e)
    logEntries = []
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
      logEntries = (await api.readLogFile(selectedLogFile)).map((line) => ({
        time: extractLogLevel(line),
        content: line,
      }))
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

async function loadRecentAppLogs() {
  try {
    logEntries = (await api.getRecentLogLines(500)).map((line) => ({ time: extractLogLevel(line), content: line }))
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

afterUpdate(() => {
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
        <button class="join-item btn btn-sm" class:btn-active={activeTab === 'app'} on:click={() => switchTab('app')}>应用日志</button>
        <button class="join-item btn btn-sm" class:btn-active={activeTab === 'service'} on:click={() => switchTab('service')}>服务日志</button>
      </div>
      {#if activeTab === 'app'}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedLogFile} on:change={(e) => { selectedLogFile = e.target.value; loadAppLogs() }}>
          {#each logFiles as file}<option value={file}>{file.split('/').pop()}</option>{/each}
        </select>
        <button class="btn btn-sm btn-ghost text-error" on:click={clearLogs}>清除</button>
      {:else}
        <select class="select select-bordered select-sm w-auto" bind:value={selectedService} on:change={(e) => { selectedService = e.target.value; loadServiceLogs() }}>
          {#each services as svc}<option value={svc.id}>{svc.name}</option>{/each}
        </select>
      {/if}
      <button class="btn btn-sm btn-outline" on:click={activeTab === 'app' ? loadAppLogs : loadServiceLogs}>刷新</button>
      <button class="btn btn-sm btn-outline" on:click={() => autoScroll = !autoScroll}>{autoScroll ? '自动滚动' : '手动滚动'}</button>
    </div>
  </div>

  <div class="flex-1 bg-slate-800 border border-slate-700 rounded-xl p-3.5 font-mono text-xs overflow-y-auto leading-relaxed" bind:this={logContainer} on:scroll={handleScroll}>
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
