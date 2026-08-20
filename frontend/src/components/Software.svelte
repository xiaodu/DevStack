<script lang="ts">
import { onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'
import { refreshSignal } from '../lib/stores'
import Database from './Database.svelte'
import Runtime from './Runtime.svelte'
import WebServer from './WebServer.svelte'

const iconMap: Record<string, string> = {
  nginx: 'icon-nginx',
  apache: 'icon-nginx1',
  mysql: 'icon-mysql',
  redis: 'icon-redis',
  postgresql: '',
  mongodb: '',
  php: 'icon-php1',
  node: 'icon-Nodejs',
  python: 'icon-python',
}

const categories = [
  { id: 'webserver', label: 'Web 服务器', icon: 'icon-webfuwuqi' },
  { id: 'database', label: '数据库', icon: 'icon-mysql' },
  { id: 'runtime', label: '运行环境', icon: 'icon-php1' },
]

let serviceItems = $state<any[]>([])
let runtimeItems = $state<any[]>([])
let selected = $state<any>(null)
let expandedCat = $state<string | null>('webserver')
let loading = $state(true)
let loadingNow = false

// 直接引用 serviceItems/runtimeItems，使每个分类下的列表与计数对数据加载保持响应式
let grouped = $derived(
  categories.map((cat) => ({
    ...cat,
    items: cat.id === 'runtime' ? runtimeItems : serviceItems.filter((s) => s.category === cat.id),
  })),
)

function itemsFor(catId: string) {
  return grouped.find((c) => c.id === catId)?.items || []
}

function toItem(x: any) {
  return {
    id: x.id,
    kind: x.kind || x.type,
    type: x.type || x.kind,
    name: x.name,
    version: x.version,
    status: x.status,
    running: !!(x.running ?? x.status === 'running'),
    port: x.port,
    uptime: x.uptime,
    category: x.category || 'runtime',
    source: x.source,
    active: x.active,
    icon: iconMap[x.kind || x.type] || '',
  }
}

async function load() {
  if (loadingNow) return
  loadingNow = true
  try {
    const [all, rts] = await Promise.all([api.getServices().catch(() => []), api.getRuntimes().catch(() => [])])
serviceItems = (all || []).filter((s: any) => s.installed).map((s: any) => toItem({ ...s, source: 'service' }))
    runtimeItems = (rts || []).filter((r: any) => r.installed !== false).map((r: any) => toItem({ ...r, source: 'runtime' }))
  } finally {
    loadingNow = false
    loading = false
  }
}

function ensureSelection() {
  if (!selected) {
    for (const cat of categories) {
      const its = itemsFor(cat.id)
      if (its.length) {
        expandedCat = cat.id
        selected = its[0]
        return
      }
    }
    return
  }
  // 启停后刷新选中项状态
  const pool = [...serviceItems, ...runtimeItems]
  selected = pool.find((i) => i.id === selected.id) || selected
}

function toggleCat(catId: string) {
  expandedCat = expandedCat === catId ? null : catId
}

function select(item: any) {
  selected = item
  expandedCat = item.category === 'runtime' ? 'runtime' : item.category
}

async function handleChange() {
  await load()
  ensureSelection()
}

let service = $derived(selected)

let unsub: (() => void) | undefined
onMount(async () => {
  await load()
  ensureSelection()
  // 进入页面/全局操作后重新拉取数据，保证状态与计数为最新
  unsub = refreshSignal.subscribe(() => {
    load()
    ensureSelection()
  })
})
onDestroy(() => {
  if (unsub) unsub()
})
</script>

<div class="grid grid-cols-[280px_1fr] h-full bg-base-100">
  <div class="border-r border-base-300 flex flex-col">
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">软件管理</h3></div>
    <div class="flex-1 p-2.5 flex flex-col gap-2 overflow-y-auto">
      {#each grouped as cat}
        {@const isOpen = expandedCat === cat.id}
        <div class="rounded-xl border border-base-200 overflow-hidden">
          <button
            type="button"
            class="flex items-center gap-2.5 w-full px-3 py-2.5 text-left text-sm font-medium transition-colors border-none {isOpen ? 'bg-base-200/70' : 'bg-base-100 text-base-content/80 hover:bg-base-200'}"
            onclick={() => toggleCat(cat.id)}>
            <span class="iconfont text-base leading-none {cat.icon}"></span>
            <span class="flex-1 truncate">{cat.label}</span>
            <span class="text-xs text-base-content/40">{loading ? '…' : cat.items.length}</span>
            <span class={`text-base-content/40 ${isOpen ? 'rotate-180' : ''}`}>▾</span>
          </button>
          {#if isOpen}
            <div class="p-1.5 bg-base-200/50 flex flex-col gap-1">
              {#if loading}
                <div class="flex items-center gap-2 text-xs text-base-content/50 px-2.5 py-2">
                  <span class="loading loading-spinner loading-xs"></span>加载中...
                </div>
              {:else if cat.items.length === 0}
                <div class="text-xs text-base-content/40 px-2.5 py-2">暂无可用的{cat.label}软件，请先到「软件仓库」安装</div>
              {:else}
                {#each cat.items as item}
                  <button
                    type="button"
                    class="flex items-center gap-2 w-full px-2.5 py-2 rounded-lg text-left text-sm transition-colors border-none
                      {selected?.id === item.id ? 'bg-primary text-primary-content' : 'text-base-content/80 hover:bg-base-100'}"
                    onclick={() => select(item)}>
                    {#if item.icon}
                      <span class="iconfont text-base leading-none {item.icon}"></span>
                    {:else}
                      <span class="w-5 h-5 shrink-0 rounded-full bg-base-content/20 flex items-center justify-center text-[10px] font-bold">{item.name.charAt(0)}</span>
                    {/if}
                    <span class="flex-1 truncate">{item.name}</span>
                    <span class="text-xs font-mono text-base-content/40">{item.version}</span>
                    <span class="w-2 h-2 rounded-full shrink-0 {item.running ? (selected?.id === item.id ? 'bg-primary-content/80' : 'bg-success') : 'bg-base-content/30'}"></span>
                  </button>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  </div>

  <div class="overflow-y-auto min-w-0">
    {#if loading}
      <div class="flex items-center justify-center h-full text-base-content/60">加载中...</div>
    {:else if selected}
      {#key selected.id}
        {#if selected.category === 'webserver'}
          <WebServer {service} on:change={handleChange} />
        {:else if selected.category === 'database'}
          <Database {service} on:change={handleChange} />
        {:else}
          <Runtime {service} on:change={handleChange} />
        {/if}
      {/key}
    {:else}
      <div class="flex flex-col items-center justify-center h-full text-base-content/60 gap-2">
        <div class="text-4xl opacity-50"><span class="iconfont icon-OSruanjianbao"></span></div>
        <p>未找到已安装的软件</p>
        <p class="text-xs text-base-content/50">请前往「软件仓库」页面安装 Nginx、MySQL、PHP 等组件</p>
      </div>
    {/if}
  </div>
</div>