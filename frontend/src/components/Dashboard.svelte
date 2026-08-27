<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { subscribeOperationEvents } from '../lib/events'
import { nanoRefresh, nanoSyncInfo, refreshSignal } from '../lib/stores'

let services = $state<any[]>([])
let runtimes = $state<any[]>([])
let sites = $state<any[]>([])
let hasLoaded = $state(false)
let loadError = $state(false)
let envSummary = $state({
  services: { total: 0, running: 0 },
  runtimes: { total: 0, running: 0 },
  projects: { total: 0, enabled: 0 },
})
let lastSnapshot = $state<any>(null)
let ops = $state<any[]>([])
let activeEnv = $state<any>(null)
let profiles = $state<any[]>([])

// 并发去重：进行中不重复发起请求，但不丢弃 —— 记录待刷标记，当前加载结束后补跑一次。
// 否则批量停止时（多个 reactive:service.stopped 密集到达 + onDone 刷新），
// 最后一次刷新会被正在进行的旧加载吞掉，卡片停留在操作中途的过期快照上。
let isLoading = false
let pendingReload = false

const serviceMeta: Record<string, any> = {
  nginx: { label: 'Nginx', cls: 'icon-nginx', color: '#009639' },
  mysql: { label: 'MySQL', cls: 'icon-mysql', color: '#4479A1' },
  redis: { label: 'Redis', cls: 'icon-redis', color: '#DC382D' },
}

// 运行时颜色（用于卡片顶部色条和图标背景）
const runtimeMeta: Record<string, any> = {
  php: { color: '#777BB4', label: 'PHP', cls: 'icon-php1' },
  node: { color: '#339933', label: 'Node.js', cls: 'icon-Nodejs' },
  python: { color: '#3776AB', label: 'Python', cls: 'icon-python' },
}

async function load() {
  if (isLoading) {
    pendingReload = true
    return
  }
  isLoading = true
  loadError = false
  // 不再使用全屏遮罩：首次加载由卡片骨架屏占位，刷新进度统一在状态栏提示
  nanoSyncInfo.set({ syncing: true, msg: '正在同步服务状态…', at: nanoSyncInfo.get().at })

  // 全局超时保护：确保同步状态不会被永久卡住
  let globalTimedOut = false
  const globalTimeout = setTimeout(() => {
    globalTimedOut = true
    console.warn('[Dashboard] load 全局超时(10s)，强制结束加载状态')
    nanoSyncInfo.set({ syncing: false, msg: '同步超时', at: '' })
    loadError = true
    hasLoaded = true
    isLoading = false
    if (pendingReload) {
      pendingReload = false
      void load()
    }
  }, 10000)

  try {
    const timeout = (ms: number) => new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), ms))
    const wrap = (p: Promise<any>, fallback: any) => Promise.race([p, timeout(8000)]).catch(() => fallback)
    const [s, r, st, opEntries, env, snaps, profileList] = await Promise.all([
      wrap(api.getServices(), []),
      wrap(api.getRuntimes(), []),
      wrap(api.getProjects(), []),
      wrap(api.getRecentOps(100), []),
      wrap(api.getEnvironment(), { services: {}, runtimes: {}, projects: {}, activeProfileID: '' }),
      wrap(api.getSnapshots(), []),
      wrap(api.listEnvironments(), []),
    ])

    // 如果全局超时已触发，放弃本次结果
    if (globalTimedOut) {
      return
    }

    services = s
    runtimes = r
    sites = st
    ops = opEntries || []
    activeEnv = env
    profiles = profileList || []
    const svcEntries = Object.entries((env as any)?.services || {})
    const rtEntries = Object.entries((env as any)?.runtimes || {})
    const projEntries = Object.entries((env as any)?.projects || {})
    envSummary = {
      services: {
        total: svcEntries.length,
        running: svcEntries.filter(([, v]: [string, any]) => v.state === 'running').length,
      },
      runtimes: {
        total: rtEntries.length,
        running: rtEntries.filter(([, v]: [string, any]) => v.state === 'running').length,
      },
      projects: { total: projEntries.length, enabled: projEntries.filter(([, v]: [string, any]) => v.enabled).length },
    }
    lastSnapshot = snaps?.length > 0 ? snaps[snaps.length - 1] : null
    hasLoaded = true
  } catch (e) {
    console.error('[Dashboard] load failed:', e)
    loadError = true
    hasLoaded = true
  } finally {
    clearTimeout(globalTimeout)
    if (!globalTimedOut) {
      isLoading = false
      nanoSyncInfo.set({
        syncing: false,
        msg: loadError ? '同步失败' : '',
        at: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
      })
    }
    // 加载期间又有新刷新请求进来 → 补跑一次，拿到最终状态
    if (pendingReload) {
      pendingReload = false
      void load()
    }
  }
}

// 根据当前激活的环境模板，构建需要展示的卡片列表
// 环境模板（profile.units）声明了组件清单，实际运行状态从服务/运行时列表匹配
function buildActiveCards(env: any, profileList: any[], svcList: any[], rtList: any[]) {
  const cards: any[] = []
  if (!env || !env.activeProfileID || !profileList) return cards

  const profile = profileList.find((p: any) => p.id === env.activeProfileID)
  if (!profile) return cards

  const units = Array.isArray(profile.units) ? profile.units : []
  for (const u of units) {
    const id = `${u.kind}@${u.version}`
    if (u.kind === 'php' || u.kind === 'node' || u.kind === 'python') {
      const rt = rtList.find((r: any) => r.id === id)
      const meta = runtimeMeta[u.kind] || {}
      const name = meta.label ? `${meta.label} ${u.version}` : u.name || `${u.kind} ${u.version}`
      cards.push({
        name,
        initial: name.charAt(0).toUpperCase(),
        iconCls: (runtimeMeta[u.kind] || {}).cls || '',
        version: u.version,
        status: rt?.status || (rt?.running ? 'running' : 'stopped'),
        installed: rt?.installed ?? false,
        color: (runtimeMeta[u.kind] || {}).color || 'var(--color-primary)',
      })
    } else {
      const svc = svcList.find((s: any) => s.id === id)
      const name = svc?.name || u.name || u.kind
      cards.push({
        name,
        initial: name.charAt(0).toUpperCase(),
        iconCls: (serviceMeta[u.kind] || {}).cls || '',
        version: svc?.version || u.version,
        status: svc?.status || 'stopped',
        installed: svc?.installed ?? false,
        color: (serviceMeta[u.kind] || {}).color || 'var(--color-primary)',
        error: svc?.error || '',
      })
    }
  }

  return cards
}

// 响应式：当 activeEnv/profiles/services/runtimes 变化时重新计算卡片
let activeCards = $derived(buildActiveCards(activeEnv, profiles, services, runtimes))

function statusLabel(s: string) {
  if (s === 'running') return '运行中'
  if (s === 'stopped') return '已停止'
  if (s === 'starting') return '启动中...'
  if (s === 'stopping') return '停止中...'
  if (s === 'error') return '错误'
  return '未知'
}

function statusDotCls(s: string) {
  if (s === 'running') return 'bg-success'
  if (s === 'starting' || s === 'stopping') return 'bg-warning animate-pulse'
  if (s === 'error') return 'bg-error'
  return 'bg-base-content/40'
}

function statusTextCls(s: string) {
  if (s === 'running') return 'text-success'
  if (s === 'starting' || s === 'stopping') return 'text-warning'
  if (s === 'error') return 'text-error'
  return 'text-base-content/40'
}

function cardBorderCls(s: string) {
  if (s === 'running') return 'border-success'
  if (s === 'starting' || s === 'stopping') return 'border-warning'
  if (s === 'error') return 'border-error'
  return ''
}

const unsub = refreshSignal.subscribe(() => load())
let lastOpRefresh = 0
const unsubOp = subscribeOperationEvents((name, data) => {
  if (name === 'step' && data?.state === 'done') {
    const now = Date.now()
    if (now - lastOpRefresh > 800) {
      lastOpRefresh = now
      refreshSignal.set(nanoRefresh.get() + 1)
    }
  }
})
onMount(() => {
  load()
  return () => {
    unsub()
    unsubOp()
  }
})
</script>

<div class="flex flex-col gap-5 h-full overflow-hidden">
  <!-- Cards row -->
  <div class="grid grid-cols-1 gap-5">
    <!-- Service mini cards - 与当前激活环境联动，纯展示 -->
    <div class="card bg-base-100 border border-base-300 overflow-hidden">
      <div class="flex justify-between items-center px-4 py-3.5 border-b border-base-300">
        <h3 class="text-sm font-semibold">服务</h3>
        {#if hasLoaded && activeEnv?.activeProfileID}
          {@const activeProfile = profiles.find((p) => p.id === activeEnv.activeProfileID)}
          {#if activeProfile}
            <span class="badge badge-sm badge-ghost text-base-content/70 border border-base-300">{activeProfile.name}</span>
          {/if}
        {/if}
      </div>
      <div class="grid grid-cols-3 gap-2 p-3">
        {#if !hasLoaded}
          {#each [1, 2, 3] as _}
            <div class="bg-base-200 rounded-lg border border-base-300 border-dashed p-2.5">
              <div class="h-2 w-3/5 rounded bg-base-300 mb-1"></div>
              <div class="h-2 w-2/5 rounded bg-base-300"></div>
            </div>
          {/each}
        {:else if activeCards.length === 0}
          <div class="col-span-3 flex flex-col items-center justify-center py-6 text-base-content/60 text-sm">
            <p>当前未激活环境</p>
            <p class="text-xs mt-1">前往「环境管理」激活一个环境模板</p>
          </div>
        {:else}
          {#each activeCards as card}
            <div class="relative bg-base-200 rounded-lg border border-base-300 overflow-hidden {cardBorderCls(card.status)}" title={card.error || ''}>
              <div class="h-0.5 w-full" style="background:{card.color}"></div>
              <div class="flex items-center gap-3 p-3">
                <div class="w-9 h-9 rounded-full flex items-center justify-center text-white shrink-0" style="background:{card.color}">
                  {#if card.iconCls}
                    <span class="iconfont text-lg {card.iconCls}"></span>
                  {:else}
                    <span class="text-sm font-bold">{card.initial}</span>
                  {/if}
                </div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-semibold truncate">{card.name}</div>
                  <div class="flex items-center gap-1.5 text-xs mt-1">
                    <span class="w-1.5 h-1.5 rounded-full {statusDotCls(card.status)}"></span>
                    <span class="font-mono text-base-content/60">v{card.version}</span>
                    {#if !card.installed}
                      <span class="badge badge-xs badge-ghost text-base-content/40 ml-auto">未安装</span>
                    {:else}
                      <span class="text-base-content/40">·</span>
                      <span class="{statusTextCls(card.status)}">{statusLabel(card.status)}</span>
                    {/if}
                  </div>
                  {#if card.status === 'error' && card.error}
                    <div class="text-xs text-error/80 mt-1 truncate" title={card.error}>{card.error}</div>
                  {/if}
                </div>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Environment overview -->
    <div class="card bg-base-100 border border-base-300 overflow-hidden">
      <div class="flex justify-between items-center px-4 py-3.5 border-b border-base-300"><h3 class="text-sm font-semibold">环境概览</h3></div>
      <div class="grid grid-cols-4 gap-0 p-4 stat-grid">
        <div class="flex flex-col items-center gap-1 px-3 py-3 border-base-300">
          <div class="text-2xl font-bold font-mono">{hasLoaded ? envSummary.services.running : '-'}/{hasLoaded ? envSummary.services.total : '-'}</div>
          <div class="text-xs text-base-content/60">服务运行中</div>
        </div>
        <div class="flex flex-col items-center gap-1 px-3 py-3 border-base-300">
          <div class="text-2xl font-bold font-mono">{hasLoaded ? envSummary.runtimes.running : '-'}/{hasLoaded ? envSummary.runtimes.total : '-'}</div>
          <div class="text-xs text-base-content/60">运行时运行中</div>
        </div>
        <div class="flex flex-col items-center gap-1 px-3 py-3 border-base-300">
          <div class="text-2xl font-bold font-mono">{hasLoaded ? envSummary.projects.enabled : '-'}/{hasLoaded ? envSummary.projects.total : '-'}</div>
          <div class="text-xs text-base-content/60">项目已启用</div>
        </div>
        <div class="flex flex-col items-center gap-1 px-3 py-3">
          <div class="text-base font-bold">{hasLoaded ? (lastSnapshot ? '有' : '无') : '-'}</div>
          <div class="text-xs text-base-content/60">最近快照</div>
        </div>
      </div>
    </div>
  </div>

  <!-- Sites table card -->
  <div class="card bg-base-100 border border-base-300 overflow-hidden">
    <div class="flex justify-between items-center px-4 py-3.5 border-b border-base-300">
      <h3 class="text-sm font-semibold">项目列表</h3>
      <span class="badge badge-sm">{sites.length} 个</span>
    </div>
    {#if !hasLoaded}
      <div class="flex items-center justify-center py-8 text-base-content/60 text-sm">加载项目中...</div>
    {:else if loadError}
      <div class="flex flex-col items-center justify-center py-8 text-base-content/60 text-sm gap-2">
        <p>数据加载失败或超时</p>
        <button class="btn btn-xs btn-ghost text-primary" onclick={() => load()}>重试</button>
      </div>
    {:else if sites.length === 0}
      <div class="flex flex-col items-center justify-center py-8 text-base-content/60 text-sm">
        <p>暂无项目</p>
        <p class="text-xs mt-1">前往「项目」页面添加您的第一个站点</p>
      </div>
    {:else}
      <table class="table">
        <thead>
          <tr><th>名称</th><th>域名</th><th>类型</th><th>根目录</th><th>状态</th></tr>
        </thead>
        <tbody>
          {#each sites as site}
            <tr>
              <td class="font-medium">{site.name}</td>
              <td><code class="bg-base-200 px-1.5 py-0.5 rounded text-xs font-mono">{site.domain}</code></td>
              <td><span class="text-xs px-1.5 py-0.5 rounded bg-base-200 text-base-content/70 font-mono">{site.type}</span></td>
              <td class="text-xs text-base-content/60 font-mono">{site.root || '-'}</td>
              <td>
                <span class="badge badge-sm {site.enabled ? 'badge-success' : 'badge-error'}">{site.enabled ? '已启用' : '已禁用'}</span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <!-- Bottom panel: Ops（占满整行） -->
  <div class="card bg-base-100 border border-base-300 overflow-hidden flex flex-col flex-1 min-h-0">
    <div class="flex justify-between items-center px-4 py-3.5 border-b border-base-300 shrink-0">
      <h3 class="text-sm font-semibold">操作日志</h3>
      <span class="badge badge-sm">{ops.length} 条</span>
    </div>
    <div class="flex-1 overflow-y-auto py-2 min-h-0">
      {#if !hasLoaded}
        <div class="flex items-center justify-center py-5 text-base-content/60 text-sm">加载操作日志中...</div>
      {:else if ops.length === 0}
        <div class="flex items-center justify-center py-5 text-base-content/60 text-sm">暂无操作记录</div>
      {:else}
        {#each ops as op}
          <div class="grid grid-cols-[80px_140px_1fr_52px] items-center gap-1.5 px-4 py-1.5 text-xs border-b border-base-200 last:border-none {op.status === '失败' ? 'bg-error/5' : ''} hover:bg-base-200">
            <span class="font-mono text-base-content/60">{op.time}</span>
            <span class="font-semibold">{op.action} {op.service}</span>
            <span class="text-base-content/70 truncate">{op.detail}</span>
            {#if op.status === '成功'}
              <span class="text-right px-1.5 py-0.5 rounded bg-base-200 text-success">成功</span>
            {:else if op.status === '失败'}
              <span class="text-right px-1.5 py-0.5 rounded bg-base-200 text-error">失败</span>
            {:else if op.status === '警告'}
              <span class="text-right px-1.5 py-0.5 rounded bg-base-200 text-warning">警告</span>
            {:else}
              <span class="text-right px-1.5 py-0.5 rounded bg-base-200 text-base-content/60">{op.status}</span>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .stat-grid > :not(:last-child) {
    border-right: 1px solid var(--fallback-bc, oklch(0.5 0 0) / 0.2);
  }
</style>
