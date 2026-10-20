<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let sites = $state<any[]>([])
let selectedId = $state<string | null>(null)
let editing = $state<any>(null)
let loading = $state(true)
let loadError = $state('')
let searchQuery = $state('')
let statusFilter = $state<'all' | 'enabled' | 'disabled'>('all')
let busy = $state(false)
let profiles = $state<any[]>([])
let profilesLoading = $state(false)

const typeMeta: Record<string, { label: string; color: string }> = {
  static: { label: '静态站点', color: '#64748b' },
  php: { label: 'PHP', color: '#777BB4' },
  node: { label: 'Node.js', color: '#339933' },
  python: { label: 'Python', color: '#3776AB' },
  proxy: { label: '反向代理', color: '#f59e0b' },
}

function typeLabel(type: string) {
  return typeMeta[type]?.label || type || '静态站点'
}

function typeColor(type: string) {
  return typeMeta[type]?.color || '#64748b'
}

function siteInitial(site: any) {
  const name = (site?.name || '').trim()
  return name ? name.charAt(0).toUpperCase() : '+'
}

function envName(id: string) {
  if (!id) return ''
  return profiles.find((p) => p.id === id)?.name || ''
}

async function load() {
  loading = true
  try {
    sites = (await api.getProjects()) || []
    await loadProfiles()
    loadError = ''
    if (selectedId && selectedId !== '__new__' && !sites.some((s) => s.id === selectedId)) {
      selectedId = null
      editing = null
    }
    if (!selectedId && sites.length > 0) selectSite(sites[0].id)
  } catch (e: any) {
    console.error('[Sites] 加载失败:', e)
    loadError = e?.message || String(e)
  } finally {
    loading = false
  }
}

async function loadProfiles() {
  profilesLoading = true
  try {
    profiles = (await api.listEnvironmentProfiles()) || []
  } catch (e) {
    profiles = []
  } finally {
    profilesLoading = false
  }
}

function selectSite(id: string) {
  selectedId = id
  const s = sites.find((x) => x.id === id)
  if (!s) return
  editing = {
    ...s,
    root: s.root || s.documentRoot || '',
    ssl: s.ssl || false,
    targetPort: s.targetPort || 0,
    proxyPass: s.proxyPass || '',
    mysqlVersion: s.mysqlVersion || '',
    nginxVersion: s.nginxVersion || '',
    nodeVersion: s.nodeVersion || '',
    pythonVersion: s.pythonVersion || '',
    environmentID: s.environmentID || s.EnvironmentID || '',
  }
}

function newSite() {
  editing = {
    id: '',
    name: '',
    domain: '',
    root: '',
    enabled: true,
    type: 'static',
    port: 80,
    httpsPort: 443,
    ssl: false,
    phpVersion: '',
    mysqlVersion: '',
    nginxVersion: '',
    nodeVersion: '',
    pythonVersion: '',
    rewrite: 'default',
    targetPort: 3000,
    proxyPass: '',
    environmentID: '',
  }
  selectedId = '__new__'
}

async function activateProjectEnvironment() {
  if (!editing?.id || editing.id === '__new__') {
    addToast('请先保存项目再激活环境', 'warning', 3500)
    return
  }
  if (busy) return
  busy = true
  try {
    await safeInvoke('activateProjectEnvironment', () => api.activateEnvironmentForProject(editing.id), {
      successMsg: `「${editing.name}」的环境已按依赖顺序激活`,
      errorPrefix: '环境激活失败',
    })
  } finally {
    busy = false
  }
}

let selectedProfile = $derived(editing?.environmentID ? profiles.find((p) => p.id === editing.environmentID) : null)

let previewURL = $derived.by(() => {
  const host = (editing?.domain || '').trim()
  if (!host) return ''
  const ssl = !!editing?.ssl
  const port = Number(ssl ? editing?.httpsPort : editing?.port)
  const def = ssl ? 443 : 80
  return `${ssl ? 'https' : 'http'}://${host}${port && port !== def ? `:${port}` : ''}`
})

let enabledCount = $derived(sites.filter((s) => s.enabled).length)
let disabledCount = $derived(sites.length - enabledCount)

async function save() {
  if (!editing) return
  await safeInvoke(
    'saveProject',
    async () => {
      let savedId = editing.id
      if (editing.id && editing.id !== '__new__') {
        await api.updateProject(editing)
      } else {
        const { id, ...rest } = editing
        const created = await api.createProject(rest)
        if (created?.id) savedId = created.id
      }
      await load()
      if (savedId && savedId !== '__new__') selectSite(savedId)
    },
    { successMsg: '保存成功', errorPrefix: '保存失败' },
  )
}

async function remove() {
  if (!editing?.id || editing.id === '__new__') return
  if (busy) return
  busy = true
  try {
    await safeInvoke(
      'deleteProject',
      async () => {
        await api.deleteProject(editing.id)
        selectedId = null
        editing = null
        await load()
      },
      { successMsg: '已删除', errorPrefix: '删除失败' },
    )
  } finally {
    busy = false
  }
}

async function toggleProject(id: string) {
  await safeInvoke(
    'toggleProject',
    async () => {
      await api.toggleProject(id)
      await load()
      if (editing?.id === id) selectSite(id)
    },
    { errorPrefix: '切换失败' },
  )
}

let filteredSites = $derived.by(() => {
  const q = searchQuery.trim().toLowerCase()
  return sites.filter((s) => {
    if (statusFilter === 'enabled' && !s.enabled) return false
    if (statusFilter === 'disabled' && s.enabled) return false
    if (!q) return true
    return (s.name || '').toLowerCase().includes(q) || (s.domain || '').toLowerCase().includes(q)
  })
})

let showTargetPort = $derived(editing && (editing.type === 'node' || editing.type === 'python'))
let showProxyPass = $derived(editing && editing.type === 'proxy')
let showPHP = $derived(editing && editing.type === 'php')

async function browseDirectory() {
  const path = await api.openDirectory(editing?.root || '')
  if (path && editing) {
    editing.root = path
  }
}

function clearFilters() {
  searchQuery = ''
  statusFilter = 'all'
}

onMount(load)
</script>

<div class="flex flex-col gap-5">
  <!-- 页面工具栏 -->
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h2 class="text-base font-semibold">所有站点</h2>
      <p class="mt-0.5 text-xs text-base-content/45">统一管理域名、根目录与项目绑定的运行环境</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <div class="relative">
        <svg xmlns="http://www.w3.org/2000/svg" class="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-base-content/35" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
        <input class="input input-bordered input-sm w-full pl-9 sm:w-64" placeholder="搜索名称或域名" bind:value={searchQuery} />
      </div>
      <button class="btn btn-primary btn-sm gap-1.5" onclick={newSite}>
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        新建项目
      </button>
    </div>
  </div>

  {#if loadError}
    <div class="alert alert-warning alert-sm flex items-center gap-3 py-2.5">
      <span class="text-sm">项目加载失败：{loadError}</span>
      <button class="btn btn-sm btn-ghost ml-auto shrink-0" onclick={() => load()} disabled={loading}>重试</button>
    </div>
  {/if}

  <div class="grid grid-cols-1 items-start gap-5 lg:grid-cols-[300px_minmax(0,1fr)]">
    <!-- 项目列表 -->
    <aside class="flex flex-col overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm lg:sticky lg:top-0 lg:min-h-[520px]">
      <div class="flex items-center justify-between gap-2 border-b border-base-300 px-4 py-3">
        <div class="flex min-w-0 items-center gap-2">
          <h3 class="text-sm font-semibold">项目列表</h3>
          <span class="rounded-full bg-base-200 px-2 py-0.5 font-mono text-[10px] text-base-content/50">{filteredSites.length}</span>
        </div>
        <div class="flex shrink-0 rounded-lg bg-base-200/70 p-0.5">
          <button class="rounded-md px-2 py-1 text-[11px] transition-colors {statusFilter === 'all' ? 'bg-base-100 text-base-content shadow-sm' : 'text-base-content/45 hover:text-base-content'}" onclick={() => (statusFilter = 'all')}>全部</button>
          <button class="rounded-md px-2 py-1 text-[11px] transition-colors {statusFilter === 'enabled' ? 'bg-base-100 text-success shadow-sm' : 'text-base-content/45 hover:text-base-content'}" onclick={() => (statusFilter = 'enabled')}>启用</button>
          <button class="rounded-md px-2 py-1 text-[11px] transition-colors {statusFilter === 'disabled' ? 'bg-base-100 text-base-content shadow-sm' : 'text-base-content/45 hover:text-base-content'}" onclick={() => (statusFilter = 'disabled')}>停用</button>
        </div>
      </div>

      <div class="max-h-[420px] flex-1 overflow-y-auto p-2 lg:max-h-[calc(100vh-232px)]">
        {#if loading}
          <div class="flex flex-col gap-1.5">
            {#each [1, 2, 3] as _}
              <div class="flex items-start gap-2.5 rounded-xl px-3 py-2.5">
                <div class="h-9 w-9 shrink-0 animate-pulse rounded-lg bg-base-200"></div>
                <div class="flex-1 space-y-2 pt-1">
                  <div class="h-2.5 w-2/5 animate-pulse rounded bg-base-200"></div>
                  <div class="h-2 w-3/5 animate-pulse rounded bg-base-200"></div>
                </div>
              </div>
            {/each}
          </div>
        {:else if filteredSites.length === 0}
          <div class="flex flex-col items-center justify-center px-4 py-12 text-center">
            <p class="text-xs text-base-content/45">{sites.length === 0 ? '暂无项目' : '没有匹配的项目'}</p>
            {#if sites.length > 0}
              <button class="btn btn-xs btn-ghost mt-2 text-primary" onclick={clearFilters}>清除筛选</button>
            {/if}
          </div>
        {:else}
          <div class="flex flex-col gap-0.5">
            {#each filteredSites as site (site.id)}
              <div
                class="group relative flex w-full cursor-pointer items-start gap-2.5 rounded-xl border px-3 py-2.5 text-left transition-all {selectedId === site.id ? 'border-primary/30 bg-primary/5 shadow-sm' : 'border-transparent hover:bg-base-200/60'}"
                role="button"
                tabindex="0"
                onclick={() => selectSite(site.id)}
                onkeydown={(e) => {
                  if (e.target !== e.currentTarget) return
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    selectSite(site.id)
                  }
                }}>
                {#if selectedId === site.id}
                  <span class="absolute left-0 top-1/2 h-7 w-0.5 -translate-y-1/2 rounded-r bg-primary"></span>
                {/if}
                <div class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-sm font-semibold text-white shadow-sm" style="background:{typeColor(site.type)}">
                  {siteInitial(site)}
                </div>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-1.5">
                    <span class="truncate text-sm font-medium">{site.name || '未命名项目'}</span>
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full {site.enabled ? 'bg-success' : 'bg-base-content/25'}"></span>
                  </div>
                  <div class="mt-0.5 truncate font-mono text-[11px] text-base-content/45">{site.domain || '未设置域名'}</div>
                  <div class="mt-1.5 flex flex-wrap items-center gap-1">
                    <span class="rounded bg-base-200 px-1.5 py-0.5 text-[10px] text-base-content/55">{typeLabel(site.type)}</span>
                    {#if envName(site.environmentID)}
                      <span class="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] text-primary/80">{envName(site.environmentID)}</span>
                    {/if}
                  </div>
                </div>
                <button
                  class="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-base-content/35 opacity-60 transition hover:bg-base-100 hover:text-base-content group-hover:opacity-100"
                  onclick={(e) => { e.stopPropagation(); toggleProject(site.id) }}
                  title={site.enabled ? '停用项目' : '启用项目'}>
                  {#if site.enabled}
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="5" width="4" height="14" rx="1"/><rect x="14" y="5" width="4" height="14" rx="1"/></svg>
                  {:else}
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                  {/if}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      {#if !loading && sites.length > 0}
        <div class="flex items-center gap-3 border-t border-base-300 px-4 py-2.5 text-[10px] text-base-content/40">
          <span class="flex items-center gap-1"><span class="h-1.5 w-1.5 rounded-full bg-success"></span>启用 {enabledCount}</span>
          <span class="flex items-center gap-1"><span class="h-1.5 w-1.5 rounded-full bg-base-content/25"></span>停用 {disabledCount}</span>
        </div>
      {/if}
    </aside>

    <!-- 项目详情 -->
    <section class="flex min-h-[560px] flex-col overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
      {#if loading}
        <div class="flex flex-1 items-center justify-center gap-2 text-sm text-base-content/45">
          <span class="loading loading-spinner loading-sm text-primary"></span>加载项目中…
        </div>
      {:else if editing}
        {@const isNew = !editing.id || editing.id === '__new__'}
        <div class="relative overflow-hidden border-b border-base-300">
          <div class="pointer-events-none absolute inset-0 bg-gradient-to-r from-primary/[0.06] via-transparent to-success/[0.05]"></div>
          <div class="relative flex flex-wrap items-start gap-4 p-5 sm:p-6">
            <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl text-lg font-bold text-white shadow-sm" style="background:{typeColor(editing.type)}">
              {siteInitial(editing)}
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="truncate text-xl font-semibold">{isNew ? '新建项目' : editing.name || '未命名项目'}</h2>
                {#if !isNew}
                  <span class="rounded-full px-2 py-0.5 text-[10px] font-medium {editing.enabled ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/45'}">{editing.enabled ? '已启用' : '已停用'}</span>
                {/if}
              </div>
              <div class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-base-content/50">
                {#if previewURL}
                  <span class="font-mono">{previewURL}</span>
                {:else}
                  <span>尚未设置域名</span>
                {/if}
                <span class="text-base-content/25">/</span>
                <span>{typeLabel(editing.type)}</span>
                {#if selectedProfile}
                  <span class="text-base-content/25">/</span>
                  <span>{selectedProfile.name}</span>
                {/if}
              </div>
            </div>
            <div class="flex items-center gap-2">
              {#if !isNew}
                <button class="btn btn-ghost btn-sm text-base-content/45 hover:bg-error/10 hover:text-error" onclick={remove} disabled={busy}>删除</button>
              {/if}
              <button class="btn btn-primary btn-sm px-5" onclick={save} disabled={busy}>{busy ? '保存中…' : '保存项目'}</button>
            </div>
          </div>
        </div>

        <div class="flex flex-col gap-5 p-5 sm:p-6">
          <!-- 基本信息 -->
          <section class="overflow-hidden rounded-xl border border-base-300 bg-base-100">
            <div class="flex items-center gap-2.5 border-b border-base-300 px-4 py-3">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8h.01"/></svg>
              </span>
              <h3 class="text-sm font-semibold">基本信息</h3>
            </div>
            <div class="grid gap-4 p-4 md:grid-cols-2">
              <label class="flex flex-col gap-1.5">
                <span class="text-xs font-medium text-base-content/60">项目名称</span>
                <input class="input input-bordered input-sm w-full" bind:value={editing.name} placeholder="例如：企业官网" />
              </label>
              <label class="flex flex-col gap-1.5">
                <span class="text-xs font-medium text-base-content/60">域名</span>
                <input class="input input-bordered input-sm w-full font-mono" bind:value={editing.domain} placeholder="clouderp.local" />
              </label>
              <label class="flex flex-col gap-1.5 md:col-span-2">
                <span class="text-xs font-medium text-base-content/60">项目根目录</span>
                <div class="flex gap-2">
                  <input class="input input-bordered input-sm w-full font-mono" bind:value={editing.root} placeholder="E:\projects\my-project" />
                  <button class="btn btn-sm btn-outline shrink-0" onclick={browseDirectory}>浏览</button>
                </div>
              </label>
              <div class="flex items-center justify-between gap-3 rounded-lg border border-base-300/70 bg-base-200/40 px-3.5 py-2.5 md:col-span-2">
                <div>
                  <div class="text-xs font-medium">启用项目</div>
                  <div class="mt-0.5 text-[11px] text-base-content/45">停用后不会写入 Nginx 站点配置</div>
                </div>
                <input type="checkbox" class="toggle toggle-sm toggle-success" bind:checked={editing.enabled} />
              </div>
            </div>
          </section>

          <!-- 站点配置 -->
          <section class="overflow-hidden rounded-xl border border-base-300 bg-base-100">
            <div class="flex items-center gap-2.5 border-b border-base-300 px-4 py-3">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"/></svg>
              </span>
              <h3 class="text-sm font-semibold">站点配置</h3>
            </div>
            <div class="p-4">
              <div class="grid gap-4 md:grid-cols-2">
                <label class="flex flex-col gap-1.5">
                  <span class="text-xs font-medium text-base-content/60">项目类型</span>
                  <select class="select select-bordered select-sm w-full" bind:value={editing.type}>
                    <option value="static">静态</option>
                    <option value="php">PHP</option>
                    <option value="node">Node.js</option>
                    <option value="python">Python</option>
                    <option value="proxy">反向代理</option>
                  </select>
                </label>
                {#if showPHP}
                  <label class="flex flex-col gap-1.5">
                    <span class="text-xs font-medium text-base-content/60">Rewrite 规则</span>
                    <select class="select select-bordered select-sm w-full" bind:value={editing.rewrite}>
                      <option value="default">Default</option>
                      <option value="laravel">Laravel</option>
                      <option value="thinkphp">ThinkPHP</option>
                    </select>
                  </label>
                {:else if showTargetPort}
                  <label class="flex flex-col gap-1.5">
                    <span class="text-xs font-medium text-base-content/60">应用端口</span>
                    <input class="input input-bordered input-sm w-full font-mono" type="number" bind:value={editing.targetPort} />
                  </label>
                {:else if showProxyPass}
                  <label class="flex flex-col gap-1.5">
                    <span class="text-xs font-medium text-base-content/60">目标地址</span>
                    <input class="input input-bordered input-sm w-full font-mono" bind:value={editing.proxyPass} placeholder="http://127.0.0.1:3000" />
                  </label>
                {/if}
              </div>

              <div class="mt-4 grid gap-4 rounded-lg border border-base-300/70 bg-base-200/40 p-3.5 md:grid-cols-3">
                <label class="flex flex-col gap-1.5">
                  <span class="text-xs font-medium text-base-content/60">HTTP 端口</span>
                  <input class="input input-bordered input-sm w-full font-mono" type="number" bind:value={editing.port} />
                </label>
                <label class="flex flex-col gap-1.5">
                  <span class="text-xs font-medium text-base-content/60">HTTPS 端口</span>
                  <input class="input input-bordered input-sm w-full font-mono" type="number" bind:value={editing.httpsPort} placeholder="443" />
                </label>
                <div class="flex items-center justify-between gap-3 md:justify-end">
                  <span class="text-xs font-medium text-base-content/60">启用 SSL</span>
                  <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={editing.ssl} />
                </div>
              </div>
            </div>
          </section>

          <!-- 环境配置 -->
          <section class="overflow-hidden rounded-xl border border-base-300 bg-base-100">
            <div class="flex items-center gap-2.5 border-b border-base-300 px-4 py-3">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5"/><path d="m3 17 9 5 9-5"/></svg>
              </span>
              <h3 class="text-sm font-semibold">环境配置</h3>
            </div>
            <div class="flex flex-col gap-3 p-4">
              <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                <label class="flex flex-col gap-1.5">
                  <span class="text-xs font-medium text-base-content/60">关联环境模板</span>
                  {#if profilesLoading}
                    <input class="input input-bordered input-sm w-full" value="加载环境列表中..." readonly />
                  {:else}
                    <select class="select select-bordered select-sm w-full" bind:value={editing.environmentID}>
                      <option value="">请选择环境模板</option>
                      {#each profiles as profile}
                        <option value={profile.id}>{profile.name}{profile.isBuiltin ? '  [预设]' : ''}</option>
                      {/each}
                    </select>
                  {/if}
                </label>
                {#if !isNew}
                  <button class="btn btn-sm btn-primary" onclick={activateProjectEnvironment} disabled={busy || !selectedProfile}>
                    {busy ? '处理中...' : '切换到此环境'}
                  </button>
                {/if}
              </div>

              {#if selectedProfile}
                <div class="rounded-xl border border-base-300/70 bg-base-200/40 p-3.5">
                  <div class="flex items-start justify-between gap-2">
                    <div class="min-w-0">
                      <div class="text-sm font-medium">{selectedProfile.name}</div>
                      <p class="mt-0.5 text-xs leading-relaxed text-base-content/50">{selectedProfile.description || '该环境包含以下组件'}</p>
                    </div>
                    {#if selectedProfile.isBuiltin}
                      <span class="shrink-0 rounded-full bg-base-200 px-2 py-0.5 text-[10px] text-base-content/45">预设</span>
                    {/if}
                  </div>
                  <div class="mt-2.5 flex flex-wrap gap-1.5">
                    {#each selectedProfile.units || [] as u}
                      <span class="inline-flex items-center gap-1 rounded-md border border-base-300/70 bg-base-100 px-2 py-0.5 text-[11px]">
                        <span class="font-medium text-base-content/70">{u.kind}</span>
                        <span class="font-mono text-base-content/40">{u.version}</span>
                      </span>
                    {/each}
                  </div>
                </div>
              {:else}
                <div class="rounded-lg border border-dashed border-base-300 px-3.5 py-3 text-xs text-base-content/45">
                  选择环境后，可一键切换并启动对应组件
                </div>
              {/if}
            </div>
          </section>
        </div>
      {:else}
        <div class="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-2xl bg-base-200/70 text-base-content/25">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"/></svg>
          </div>
          <p class="mt-4 text-sm font-semibold">{sites.length === 0 ? '还没有项目' : '选择一个项目'}</p>
          <p class="mt-1 max-w-xs text-xs leading-relaxed text-base-content/45">
            {sites.length === 0
              ? '创建项目并绑定域名与根目录，DevStack 会自动生成并同步 Nginx 站点配置'
              : '从左侧选择项目查看详情，或新建一个项目'}
          </p>
          {#if sites.length === 0}
            <button class="btn btn-primary btn-sm mt-5 gap-1.5" onclick={newSite}>
              <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
              新建第一个项目
            </button>
          {/if}
        </div>
      {/if}
    </section>
  </div>
</div>
