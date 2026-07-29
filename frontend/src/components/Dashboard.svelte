<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { addToast, globalLoading, nanoLoading, nanoStatusMsg, refreshSignal, statusMessage } from '../lib/stores'

let services = []
let runtimes = []
let sites = []
let systemInfo = {}
let hasLoaded = false
let loadError = false
let envSummary = {
  services: { total: 0, running: 0 },
  runtimes: { total: 0, running: 0 },
  projects: { total: 0, enabled: 0 },
}
let lastSnapshot = null
let ops = []
let activeEnv = null
let profiles = []

// 并发去重标志：防止 refreshSignal 频繁触发导致多个 load() 并发执行
let isLoading = false

const serviceMeta = {
  nginx: { label: 'Nginx', cls: 'icon-nginx', color: '#009639' },
  mysql: { label: 'MySQL', cls: 'icon-mysql', color: '#4479A1' },
  redis: { label: 'Redis', cls: 'icon-redis', color: '#DC382D' },
}

// 运行时颜色（用于卡片顶部色条和首字母圆标）
const runtimeMeta = {
  php: { color: '#777BB4', label: 'PHP' },
  node: { color: '#339933', label: 'Node.js' },
  python: { color: '#3776AB', label: 'Python' },
}

async function load() {
  // 并发去重：如果已经在加载中，跳过本次请求
  if (isLoading) {
    return
  }
  isLoading = true
  loadError = false
  nanoLoading.set(true)
  nanoStatusMsg.set('正在加载服务数据…')

  // 全局超时保护：确保 nanoLoading 不会被永久卡住
  let globalTimedOut = false
  const globalTimeout = setTimeout(() => {
    globalTimedOut = true
    console.warn('[Dashboard] load 全局超时(10s)，强制结束加载状态')
    nanoLoading.set(false)
    nanoStatusMsg.set('')
    loadError = true
    hasLoaded = true
    isLoading = false
  }, 10000)

  try {
    const timeout = (ms) => new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), ms))
    const wrap = (p, fallback) => Promise.race([p, timeout(8000)]).catch(() => fallback)
    const [s, r, st, info, opEntries, env, snaps, profileList] = await Promise.all([
      wrap(api.getServices(), []),
      wrap(api.getRuntimes(), []),
      wrap(api.getProjects(), []),
      wrap(api.getSystemInfo(), {}),
      wrap(api.getRecentOps(100), []),
      wrap(api.getEnvironment(), { services: {}, runtimes: {}, projects: {}, activeProfileID: '' }),
      wrap(api.getSnapshots(), []),
      wrap(api.listEnvironmentProfiles(), []),
    ])

    // 如果全局超时已触发，放弃本次结果
    if (globalTimedOut) {
      return
    }

    services = s
    runtimes = r
    sites = st
    systemInfo = info
    ops = opEntries || []
    activeEnv = env
    profiles = profileList || []
    const svcEntries = Object.entries(env?.services || {})
    const rtEntries = Object.entries(env?.runtimes || {})
    const projEntries = Object.entries(env?.projects || {})
    envSummary = {
      services: { total: svcEntries.length, running: svcEntries.filter(([, v]) => v.state === 'running').length },
      runtimes: { total: rtEntries.length, running: rtEntries.filter(([, v]) => v.state === 'running').length },
      projects: { total: projEntries.length, enabled: projEntries.filter(([, v]) => v.enabled).length },
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
      nanoLoading.set(false)
      if (!loadError) nanoStatusMsg.set('就绪')
    }
  }
}

// 将 profile.services / profile.runtimes 统一转换为数组
// 后端返回 map[string]Spec，浏览器 mock 用数组，两种都兼容
function toSpecArray(spec) {
  if (!spec) return []
  if (Array.isArray(spec)) return spec
  if (typeof spec === 'object') {
    // map 形式：key 是 id/runtimeId，value 是 { version, ... }
    return Object.entries(spec).map(([key, val]) => ({
      id: val.id || key,
      runtimeId: val.runtimeId || key,
      version: val.version,
      default: val.default,
    }))
  }
  return []
}

// 根据当前激活的环境模板，构建需要展示的卡片列表
function buildActiveCards(env, profileList, svcList, rtList) {
  const cards = []
  if (!env || !env.activeProfileID || !profileList) return cards

  const profile = profileList.find((p) => p.id === env.activeProfileID)
  if (!profile) return cards

  const serviceSpecs = toSpecArray(profile.services)
  const runtimeSpecs = toSpecArray(profile.runtimes)

  // 服务卡片
  for (const spec of serviceSpecs) {
    const svc = svcList.find((s) => s.id === spec.id)
    const name = svc?.name || spec.id
    cards.push({
      name,
      initial: name.charAt(0).toUpperCase(),
      version: svc?.version || spec.version,
      status: svc?.status || 'stopped',
      installed: svc?.installed ?? false,
      color: (serviceMeta[spec.id] || {}).color || 'var(--p)',
    })
  }

  // 运行时卡片
  for (const spec of runtimeSpecs) {
    const rt = rtList.find((r) => r.type === spec.runtimeId || r.id === spec.runtimeId + '-' + spec.version)
    const meta = runtimeMeta[spec.runtimeId] || {}
    const name = meta.label
      ? meta.label + ' ' + (rt?.version || spec.version)
      : rt?.name || spec.runtimeId + ' ' + spec.version
    cards.push({
      name,
      initial: name.charAt(0).toUpperCase(),
      version: rt?.version || spec.version,
      status: rt?.running ? 'running' : 'stopped',
      installed: rt?.installed ?? false,
      color: (runtimeMeta[spec.runtimeId] || {}).color || 'var(--p)',
    })
  }

  return cards
}

// 响应式：当 activeEnv/profiles/services/runtimes 变化时重新计算卡片
$: activeCards = buildActiveCards(activeEnv, profiles, services, runtimes)

function statusLabel(s) {
  if (s === 'running') return '运行中'
  if (s === 'stopped') return '已停止'
  if (s === 'starting') return '启动中'
  if (s === 'stopping') return '停止中'
  return '错误'
}

const unsub = refreshSignal.subscribe(() => load())
onMount(() => {
  load()
  return unsub
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
            <div class="relative bg-base-200 rounded-lg border border-base-300 overflow-hidden {card.status === 'running' ? 'border-success' : ''}">
              <div class="h-0.5 w-full" style="background:{card.color}"></div>
              <div class="flex items-center gap-3 p-3">
                <div class="w-9 h-9 rounded-full flex items-center justify-center text-white text-sm font-bold shrink-0" style="background:{card.color}">
                  {card.initial}
                </div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-semibold truncate">{card.name}</div>
                  <div class="flex items-center gap-1.5 text-xs mt-1">
                    <span class="w-1.5 h-1.5 rounded-full {card.status === 'running' ? 'bg-success' : 'bg-base-content/40'}"></span>
                    <span class="font-mono text-base-content/60">v{card.version}</span>
                    {#if !card.installed}
                      <span class="badge badge-xs badge-ghost text-base-content/40 ml-auto">未安装</span>
                    {:else}
                      <span class="text-base-content/40">·</span>
                      <span class="{card.status === 'running' ? 'text-success' : 'text-base-content/40'}">{statusLabel(card.status)}</span>
                    {/if}
                  </div>
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
        <button class="btn btn-xs btn-ghost text-primary" on:click={load}>重试</button>
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

  <!-- Bottom panel: System info + Ops -->
  <div class="grid grid-cols-2 gap-5 flex-1 min-h-0">
    <div class="card bg-base-100 border border-base-300 overflow-hidden">
      <div class="px-4 py-3.5 border-b border-base-300"><h3 class="text-sm font-semibold">系统信息</h3></div>
      <div class="grid grid-cols-2 gap-0">
        <div class="p-4 flex flex-col gap-2.5 border-r border-base-300">
          <div class="flex items-center justify-between gap-3"><span class="text-xs text-base-content/60 shrink-0">平台</span><span class="text-sm font-mono text-right">{systemInfo.platform || '-'} / {systemInfo.arch || '-'}</span></div>
          <div class="flex items-center justify-between gap-3"><span class="text-xs text-base-content/60 shrink-0">Go 版本</span><span class="text-sm font-mono text-right">{systemInfo.goVersion || '-'}</span></div>
          <div class="flex items-center justify-between gap-3"><span class="text-xs text-base-content/60 shrink-0">应用版本</span><span class="text-sm font-mono text-right">{systemInfo.appVersion || '-'}</span></div>
        </div>
        <div class="p-4 flex flex-col gap-2.5">
          {#if !hasLoaded}
            {#each [1,2,3] as _}<div class="flex items-center justify-between"><span class="text-xs text-base-content/60">-</span><span class="text-sm font-mono">-</span></div>{/each}
          {:else}
            {#each [...services, ...runtimes] as svc}
              {#if svc.installed}
                <div class="flex items-center justify-between gap-3">
                  <span class="text-xs text-base-content/60 shrink-0">{svc.name}</span>
                  <span class="text-sm font-mono text-right">
                    {#if svc.status === 'running' || svc.running}
                      PID: {svc.pid || '-'}　{svc.uptime || ''}
                    {:else}
                      {statusLabel(svc.status) || '已停止'}
                    {/if}
                  </span>
                </div>
              {/if}
            {/each}
          {/if}
        </div>
      </div>
    </div>

    <div class="card bg-base-100 border border-base-300 overflow-hidden flex flex-col min-h-0">
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
            <div class="grid grid-cols-[80px_140px_1fr_52px] items-center gap-1.5 px-4 py-1.5 text-xs border-b border-base-200 last:border-none {op.status?.startsWith('失败') ? 'bg-error/5' : ''} hover:bg-base-200">
              <span class="font-mono text-base-content/60">{op.time}</span>
              <span class="font-semibold">{op.action} {op.service}</span>
              <span class="text-base-content/70 truncate">{op.detail}</span>
              <span class="text-right px-1.5 py-0.5 rounded bg-base-200 {op.status === '成功' ? 'text-success' : 'text-error'}">{op.status === '成功' ? '成功' : '失败'}</span>
            </div>
          {/each}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .stat-grid > :not(:last-child) {
    border-right-width: 1px;
  }
</style>
