<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { subscribeOperationEvents } from '../lib/events'
import { nanoRefresh, nanoSyncInfo, refreshSignal } from '../lib/stores'

let services = $state<any[]>([])
let runtimes = $state<any[]>([])
let sites = $state<any[]>([])
let hasLoaded = $state(false)
// 加载失败必须与「没有数据」区分开：wrap() 吞掉的每个数据源都按名字记录在这里（失败不再显示为空列表）
let failedSources = $state<string[]>([])
let timedOut = $state(false)
let loadError = $derived(failedSources.length > 0 || timedOut)
// 项目列表卡片的失败分支：仅当「项目列表」这一路失败（或整体超时）才用错误态替代「暂无项目」
let sitesFailed = $derived(timedOut || failedSources.includes('项目列表'))
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
  failedSources = []
  timedOut = false
  // 不再使用全屏遮罩：首次加载由卡片骨架屏占位，刷新进度统一在状态栏提示
  nanoSyncInfo.set({ syncing: true, msg: '正在同步组件状态…', at: nanoSyncInfo.get().at })

  // 全局超时保护：确保同步状态不会被永久卡住
  let globalTimedOut = false
  const globalTimeout = setTimeout(() => {
    globalTimedOut = true
    console.warn('[Dashboard] load 全局超时(10s)，强制结束加载状态')
    nanoSyncInfo.set({ syncing: false, msg: '同步超时', at: '' })
    timedOut = true
    hasLoaded = true
    isLoading = false
    if (pendingReload) {
      pendingReload = false
      void load()
    }
  }, 10000)

  try {
    const timeout = (ms: number) => new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), ms))
    // 失败不再静默回退成空数组：先记在本次调用的局部列表里，避免超时后迟到的 reject 污染下一次加载
    const failed: string[] = []
    const wrap = (label: string, p: Promise<any>, fallback: any) =>
      Promise.race([p, timeout(8000)]).catch((e) => {
        console.error(`[Dashboard] ${label} 加载失败:`, e)
        failed.push(label)
        return fallback
      })
    const [s, r, st, opEntries, env, snaps, profileList] = await Promise.all([
      wrap('组件状态', api.getServices(), []),
      wrap('运行时状态', api.getRuntimes(), []),
      wrap('项目列表', api.getProjects(), []),
      wrap('操作日志', api.getRecentOps(100), []),
      wrap('环境概览', api.getEnvironment(), { services: {}, runtimes: {}, projects: {}, activeProfileID: '' }),
      wrap('快照列表', api.getSnapshots(), []),
      wrap('环境模板', api.listEnvironments(), []),
    ])

    // 如果全局超时已触发，放弃本次结果
    if (globalTimedOut) {
      return
    }

    failedSources = failed
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
    failedSources = [...failedSources, '整体同步']
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
let activeProfile = $derived(profiles.find((p) => p.id === activeEnv?.activeProfileID) || null)
let runningTotal = $derived(envSummary.services.running + envSummary.runtimes.running)
let totalUnits = $derived(envSummary.services.total + envSummary.runtimes.total)
let runningPct = $derived(totalUnits > 0 ? Math.round((runningTotal / totalUnits) * 100) : 0)
let envStateLabel = $derived(!activeEnv?.activeProfileID ? '未激活' : runningTotal > 0 ? '运行中' : '已停止')
let envStateDot = $derived(!activeEnv?.activeProfileID ? 'bg-base-content/30' : runningTotal > 0 ? 'bg-success' : 'bg-base-content/40')

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

<div class="flex flex-col gap-5">
  <!-- 加载失败横幅：明确指出是取数失败而不是没有数据，并提供重试 -->
  {#if hasLoaded && loadError}
    <div class="alert alert-warning alert-sm flex items-center gap-3 py-2.5">
      <span class="text-sm">
        {#if timedOut}
          数据加载超时，以下信息可能不完整
        {:else}
          部分数据加载失败：{failedSources.join('、')}
        {/if}
      </span>
      <button class="btn btn-sm btn-ghost ml-auto shrink-0" onclick={() => load()}>重试</button>
    </div>
  {/if}

  <!-- 当前环境概览 -->
  <section class="overflow-hidden rounded-2xl border border-base-300 bg-gradient-to-br from-base-100 via-base-100 to-primary/5 p-5">
    <div class="flex flex-wrap items-center gap-4">
      <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-primary to-primary/70 text-primary-content shadow-sm">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-6 w-6"><path d="m12 2 9 5-9 5-9-5 9-5Z"/><path d="m3 12 9 5 9-5"/><path d="m3 17 9 5 9-5"/></svg>
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="text-xs text-base-content/50">当前环境</span>
          <span class="inline-flex items-center gap-1.5 rounded-full bg-base-200/80 px-2 py-0.5 text-[11px] text-base-content/60">
            <span class="h-1.5 w-1.5 rounded-full {envStateDot}"></span>
            {hasLoaded ? envStateLabel : '—'}
          </span>
        </div>
        <h2 class="mt-1 truncate text-lg font-bold">{activeProfile?.name || '尚未激活环境'}</h2>
        <p class="mt-0.5 text-xs text-base-content/55">
          {#if activeProfile}
            {(activeProfile.units?.length || 0)} 个组件 · {runningTotal}/{totalUnits} 运行中
          {:else}
            前往「环境管理」选择一个环境模板并激活
          {/if}
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <span class="rounded-full bg-success/10 px-3 py-1.5 text-xs font-medium text-success">{runningTotal} 运行</span>
        <span class="rounded-full bg-base-200 px-3 py-1.5 text-xs text-base-content/60">{Math.max(totalUnits - runningTotal, 0)} 停止</span>
      </div>
    </div>
    <div class="mt-4 h-1.5 overflow-hidden rounded-full bg-base-200">
      <div class="h-full rounded-full bg-success/80 transition-all duration-500" style="width:{hasLoaded ? runningPct : 0}%"></div>
    </div>
  </section>

  <!-- 指标卡 -->
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <div class="rounded-2xl border border-base-300 bg-base-100 p-4 shadow-sm">
      <div class="flex items-center justify-between">
        <span class="text-xs text-base-content/55">组件运行中</span>
        <span class="h-7 w-7 rounded-lg bg-success/10 text-success flex items-center justify-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><rect x="3" y="4" width="18" height="6" rx="2"/><rect x="3" y="14" width="18" height="6" rx="2"/></svg>
        </span>
      </div>
      <div class="mt-2 font-mono text-2xl font-bold">{hasLoaded ? envSummary.services.running : '-'}<span class="text-sm font-normal text-base-content/40">/{hasLoaded ? envSummary.services.total : '-'}</span></div>
    </div>
    <div class="rounded-2xl border border-base-300 bg-base-100 p-4 shadow-sm">
      <div class="flex items-center justify-between">
        <span class="text-xs text-base-content/55">运行时运行中</span>
        <span class="h-7 w-7 rounded-lg bg-primary/10 text-primary flex items-center justify-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><path d="m8 8-4 4 4 4"/><path d="m16 8 4 4-4 4"/></svg>
        </span>
      </div>
      <div class="mt-2 font-mono text-2xl font-bold">{hasLoaded ? envSummary.runtimes.running : '-'}<span class="text-sm font-normal text-base-content/40">/{hasLoaded ? envSummary.runtimes.total : '-'}</span></div>
    </div>
    <div class="rounded-2xl border border-base-300 bg-base-100 p-4 shadow-sm">
      <div class="flex items-center justify-between">
        <span class="text-xs text-base-content/55">项目已启用</span>
        <span class="h-7 w-7 rounded-lg bg-warning/10 text-warning flex items-center justify-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"/></svg>
        </span>
      </div>
      <div class="mt-2 font-mono text-2xl font-bold">{hasLoaded ? envSummary.projects.enabled : '-'}<span class="text-sm font-normal text-base-content/40">/{hasLoaded ? envSummary.projects.total : '-'}</span></div>
    </div>
    <div class="rounded-2xl border border-base-300 bg-base-100 p-4 shadow-sm">
      <div class="flex items-center justify-between">
        <span class="text-xs text-base-content/55">最近快照</span>
        <span class="h-7 w-7 rounded-lg bg-base-200 text-base-content/60 flex items-center justify-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3.5 w-3.5"><path d="M3 8V5a2 2 0 0 1 2-2h3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M21 16v3a2 2 0 0 1-2 2h-3"/><circle cx="12" cy="12" r="3.5"/></svg>
        </span>
      </div>
      <div class="mt-2 truncate text-sm font-semibold" title={lastSnapshot || ''}>{hasLoaded ? (lastSnapshot || '暂无快照') : '-'}</div>
    </div>
  </div>

  <!-- Cards row -->
  <div class="grid grid-cols-1 gap-5">
    <!-- Service mini cards - 与当前激活环境联动，纯展示 -->
    <div class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
      <div class="flex justify-between items-center border-b border-base-200 px-5 py-4">
        <h3 class="text-sm font-semibold">组件</h3>
        {#if hasLoaded && activeProfile}
          <span class="rounded-full border border-base-300 px-2.5 py-0.5 text-xs text-base-content/60">{activeProfile.name}</span>
        {/if}
      </div>
      <div class="grid grid-cols-3 gap-3 p-4">
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
            <div class="relative overflow-hidden rounded-xl border border-base-300/80 bg-base-200/40 {cardBorderCls(card.status)}" title={card.error || ''}>
              <div class="h-0.5 w-full" style="background:{card.color}"></div>
              <div class="flex items-center gap-3 p-3">
                <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl text-white" style="background:{card.color}">
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
                  {#if card.error && card.status === 'error'}
                    <div class="text-xs text-error/80 mt-1 truncate" title={card.error}>{card.error}</div>
                  {:else if card.error}
                    <!-- “确实停了、但停止过程有异常”（如优雅关闭被拒、实际靠强杀收尾）：
                         状态仍是诚实的 stopped，但风险必须看得见，不能只藏在 title 提示里 -->
                    <div class="text-xs text-warning/80 mt-1 truncate" title={card.error}>⚠ {card.error}</div>
                  {/if}
                </div>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>
  </div>

  <!-- Sites table card -->
  <div class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
    <div class="flex justify-between items-center border-b border-base-200 px-5 py-4">
      <h3 class="text-sm font-semibold">项目列表</h3>
      <span class="rounded-full bg-base-200 px-2.5 py-0.5 text-xs text-base-content/60">{sites.length} 个</span>
    </div>
    {#if !hasLoaded}
      <div class="flex items-center justify-center py-8 text-base-content/60 text-sm">加载项目中...</div>
    {:else if sitesFailed}
      <div class="flex flex-col items-center justify-center py-8 text-base-content/60 text-sm gap-2">
        <p class="text-error">{timedOut ? '项目列表加载超时' : '项目列表加载失败'}</p>
        <button class="btn btn-xs btn-ghost text-primary" onclick={() => load()}>重试</button>
      </div>
    {:else if sites.length === 0}
      <div class="flex flex-col items-center justify-center py-8 text-base-content/60 text-sm">
        <p>暂无项目</p>
        <p class="text-xs mt-1">前往「项目」页面添加您的第一个站点</p>
      </div>
    {:else}
      <table class="table table-sm">
        <thead>
          <tr class="border-base-200 text-xs text-base-content/50">
            <th class="font-medium">名称</th><th class="font-medium">域名</th><th class="font-medium">类型</th><th class="font-medium">根目录</th><th class="font-medium">状态</th>
          </tr>
        </thead>
        <tbody>
          {#each sites as site}
            <tr class="border-base-200 hover:bg-base-200/40">
              <td>
                <div class="flex items-center gap-2">
                  <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-base-200 text-xs font-semibold text-base-content/70">{site.name?.charAt(0).toUpperCase() || '?'}</span>
                  <span class="font-medium">{site.name}</span>
                </div>
              </td>
              <td><code class="rounded-md bg-base-200 px-2 py-0.5 text-xs font-mono">{site.domain}</code></td>
              <td><span class="rounded-md bg-base-200 px-2 py-0.5 text-xs font-mono text-base-content/70">{site.type}</span></td>
              <td class="max-w-[260px] truncate text-xs font-mono text-base-content/55" title={site.root || ''}>{site.root || '-'}</td>
              <td>
                <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs {site.enabled ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/50'}">
                  <span class="h-1.5 w-1.5 rounded-full {site.enabled ? 'bg-success' : 'bg-base-content/30'}"></span>
                  {site.enabled ? '已启用' : '已禁用'}
                </span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <!-- Bottom panel: Ops（占满整行） -->
  <div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
    <div class="flex shrink-0 items-center justify-between border-b border-base-200 px-5 py-4">
      <h3 class="text-sm font-semibold">操作日志</h3>
      <span class="rounded-full bg-base-200 px-2.5 py-0.5 text-xs text-base-content/60">{ops.length} 条</span>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto py-2">
      {#if !hasLoaded}
        <div class="flex items-center justify-center py-5 text-sm text-base-content/60">加载操作日志中...</div>
      {:else if ops.length === 0}
        <div class="flex items-center justify-center py-5 text-sm text-base-content/60">暂无操作记录</div>
      {:else}
        {#each ops as op}
          <div class="grid grid-cols-[80px_140px_1fr_52px] items-center gap-1.5 border-b border-base-200 px-5 py-1.5 text-xs last:border-none {op.status === '失败' ? 'bg-error/5' : ''} hover:bg-base-200/50">
            <span class="font-mono text-base-content/55">{op.time}</span>
            <span class="truncate font-semibold">{op.action} {op.service}</span>
            <span class="truncate text-base-content/70">{op.detail}</span>
            {#if op.status === '成功'}
              <span class="rounded-md bg-success/10 px-1.5 py-0.5 text-right text-success">成功</span>
            {:else if op.status === '失败'}
              <span class="rounded-md bg-error/10 px-1.5 py-0.5 text-right text-error">失败</span>
            {:else if op.status === '警告'}
              <span class="rounded-md bg-warning/10 px-1.5 py-0.5 text-right text-warning">警告</span>
            {:else}
              <span class="rounded-md bg-base-200 px-1.5 py-0.5 text-right text-base-content/60">{op.status}</span>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>
