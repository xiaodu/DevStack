<script lang="ts">
import { onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'
import { useDownloadProgress } from '../lib/events'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

type PackageMeta = {
  name: string
  desc: string
  category: string
  color: string
  icon: string
}

const packageMeta: Record<string, PackageMeta> = {
  nginx: { name: 'Nginx', desc: '高性能 Web 服务器与反向代理', category: 'webserver', color: '#009639', icon: 'icon-nginx' },
  apache: { name: 'Apache', desc: '经典 Apache HTTP Server', category: 'webserver', color: '#D22128', icon: 'icon-nginx1' },
  mysql: { name: 'MySQL', desc: '最流行的开源关系型数据库', category: 'database', color: '#4479A1', icon: 'icon-mysql' },
  postgresql: { name: 'PostgreSQL', desc: '功能强大的企业级关系型数据库', category: 'database', color: '#336791', icon: '' },
  mongodb: { name: 'MongoDB', desc: '面向文档的 NoSQL 数据库', category: 'database', color: '#47A248', icon: '' },
  redis: { name: 'Redis', desc: '高性能内存缓存与消息中间件', category: 'service', color: '#DC382D', icon: 'icon-redis' },
  memcached: { name: 'Memcached', desc: '分布式内存对象缓存系统', category: 'service', color: '#5B8C5A', icon: '' },
  php: { name: 'PHP', desc: 'PHP 运行环境（FastCGI）', category: 'runtime', color: '#777BB4', icon: 'icon-php1' },
  node: { name: 'Node.js', desc: 'Node.js JavaScript 运行时', category: 'runtime', color: '#339933', icon: 'icon-Nodejs' },
  python: { name: 'Python', desc: 'Python 运行时环境', category: 'runtime', color: '#3776AB', icon: 'icon-python' },
  composer: { name: 'Composer', desc: 'PHP 依赖管理工具（composer.phar）', category: 'tools', color: '#885630', icon: '' },
  phpmyadmin: { name: 'phpMyAdmin', desc: '基于 Web 的 MySQL / MariaDB 管理工具', category: 'tools', color: '#F89C0E', icon: '' },
}

const categories = [
  { id: 'all', label: '全部' },
  { id: 'webserver', label: 'Web 服务器' },
  { id: 'database', label: '数据库' },
  { id: 'runtime', label: '运行环境' },
  { id: 'service', label: '基础服务' },
  { id: 'tools', label: '工具' },
]

type VersionRow = { version: string; installed: boolean }
type PackageGroup = PackageMeta & { kind: string; versions: VersionRow[]; installedCount: number }

let groups = $state<PackageGroup[]>([])
let selected = $state('all')
let query = $state('')
let installing = $state<Record<string, any>>({})
let cancelling = $state<Record<string, boolean>>({})
let uninstalling = $state<Record<string, any>>({})
let loading = $state(true)
let loadError = $state('')
let installDir = $state('')
let installProgress: Record<string, any> = {}
let installElapsed = $state<Record<string, any>>({})
let elapsedTimer: ReturnType<typeof setInterval> | null = null
const cancelledInstalls = new Set<string>()

useDownloadProgress((data) => {
  if (!data || !data.key) return
  const shortKey = data.key.replace('download:', '').replace(':', '-')
  installProgress[shortKey] = { downloaded: data.downloaded, total: data.total }
  installProgress = { ...installProgress }
})

onDestroy(() => {
  if (elapsedTimer) clearInterval(elapsedTimer)
})

async function load() {
  loading = true
  loadError = ''
  try {
    const kinds = Object.keys(packageMeta)
    const [versionsByKind, installedByKind, settings] = await Promise.all([
      Promise.all(
        kinds.map(async (kind) => ({
          kind,
          releases: (await api.getAvailableVersions(kind)) || [],
        })),
      ),
      Promise.all(kinds.map(async (kind) => ({ kind, installed: (await api.getInstalledVersions(kind)) || [] }))),
      api.getSettings().catch(() => ({}) as any),
    ])
    installDir = (settings as any).installDir || ''
    const installedMap = new Map(installedByKind.map((v) => [v.kind, new Set(v.installed)]))
    groups = kinds
      .map((kind) => {
        const releases = versionsByKind.find((v) => v.kind === kind)?.releases || []
        const installedSet = installedMap.get(kind) || new Set<string>()
        const versions = releases
          .map((rel: any) => ({ version: rel.version, installed: installedSet.has(rel.version) }))
          .sort((a: VersionRow, b: VersionRow) => b.version.localeCompare(a.version, undefined, { numeric: true }))
        return { ...packageMeta[kind], kind, versions, installedCount: versions.filter((v: VersionRow) => v.installed).length }
      })
      .filter((g) => g.versions.length > 0)
  } catch (e) {
    console.error(e)
    loadError = (e as any)?.message || String(e)
  } finally {
    loading = false
  }
}

function categoryCount(catId: string) {
  return catId === 'all' ? groups.length : groups.filter((g) => g.category === catId).length
}

let filteredGroups = $derived.by(() => {
  const q = query.trim().toLowerCase()
  return groups.filter((g) => {
    if (selected !== 'all' && g.category !== selected) return false
    if (!q) return true
    return (
      g.name.toLowerCase().includes(q) ||
      g.desc.toLowerCase().includes(q) ||
      g.kind.includes(q) ||
      g.versions.some((v) => v.version.includes(q))
    )
  })
})

let installedPackages = $derived(groups.filter((g) => g.installedCount > 0).length)
let totalVersions = $derived(groups.reduce((n, g) => n + g.versions.length, 0))

function formatBytes(bytes: number) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function formatElapsed(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

async function install(group: PackageGroup, rel: VersionRow) {
  const key = `${group.kind}-${rel.version}`
  if (installing[key]) return
  cancelledInstalls.delete(key)
  installing[key] = true
  installProgress[key] = { downloaded: 0, total: 0 }
  installElapsed[key] = 0

  if (!elapsedTimer) {
    elapsedTimer = setInterval(() => {
      for (const k of Object.keys(installing)) {
        if (installing[k]) {
          installElapsed[k] = (installElapsed[k] || 0) + 1
          installElapsed = { ...installElapsed }
        }
      }
    }, 1000)
  }

  try {
    await api.downloadService(group.kind, rel.version)
    if (cancelledInstalls.has(key)) return
    await load()
    addToast(`${group.name} ${rel.version} 安装成功`, 'success', 3500)
  } catch (e) {
    if (cancelledInstalls.has(key)) return
    addToast(`安装 ${group.name} ${rel.version} 失败: ${(e as any)?.message || e}`, 'error', 3500)
  } finally {
    installing[key] = false
    cancelling[key] = false
    delete installProgress[key]
    delete installElapsed[key]
    installProgress = { ...installProgress }
  }
}

function cancelInstall(group: PackageGroup, rel: VersionRow) {
  const key = `${group.kind}-${rel.version}`
  if (!installing[key]) return
  cancelling[key] = true
  cancelledInstalls.add(key)
  api.cancelDownload(`download:${group.kind}:${rel.version}`).catch((e) => {
    console.error('取消下载失败:', e)
    addToast(`取消下载失败: ${(e as any)?.message || e}`, 'error', 3500)
  })
  delete installProgress[key]
  delete installElapsed[key]
  installProgress = { ...installProgress }
  addToast(`正在取消安装 ${group.name} ${rel.version}`, 'warning', 3500)
}

function getProgress(key: string) {
  return installProgress[key] || { downloaded: 0, total: 0 }
}

async function uninstall(group: PackageGroup, rel: VersionRow) {
  if (!confirm(`确定卸载 ${group.name} ${rel.version}？`)) return
  const key = `${group.kind}-${rel.version}`
  uninstalling[key] = true
  await safeInvoke(
    async () => {
      await api.uninstallVersion(group.kind, rel.version)
      await load()
    },
    { successMsg: `${group.name} ${rel.version} 卸载成功`, errorPrefix: '卸载失败' },
  )
  uninstalling[key] = false
}

function installedExample(group: PackageGroup) {
  return group.versions.find((v) => v.installed)?.version || ''
}

onMount(load)
</script>

<div class="flex flex-col gap-5">
  <div class="flex flex-wrap items-end justify-between gap-4">
    <div>
      <h1 class="text-xl font-bold">软件仓库</h1>
      <p class="mt-1 text-sm text-base-content/60">下载并安装 Web 服务器、数据库、运行时与开发者工具</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <span class="badge badge-sm border border-base-300 bg-base-100 text-base-content/60">{installedPackages} 个软件已安装</span>
      <span class="badge badge-sm border border-base-300 bg-base-100 text-base-content/60">{totalVersions} 个可用版本</span>
      <label class="flex h-9 w-56 items-center gap-2 rounded-lg border border-base-300 bg-base-100 px-3">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4 text-base-content/40">
          <circle cx="11" cy="11" r="7"></circle><path d="m20 20-3.5-3.5"></path>
        </svg>
        <input class="w-full bg-transparent text-sm outline-none placeholder:text-base-content/40" placeholder="搜索软件或版本" bind:value={query} />
      </label>
    </div>
  </div>

  <div class="flex flex-wrap items-center gap-2">
    {#each categories as cat}
      <button
        type="button"
        class="flex items-center gap-2 rounded-full border px-3.5 py-1.5 text-sm transition-colors
          {selected === cat.id ? 'border-primary bg-primary text-primary-content' : 'border-base-300 bg-base-100 text-base-content/70 hover:border-primary/40 hover:text-base-content'}"
        onclick={() => (selected = cat.id)}>
        <span>{cat.label}</span>
        <span class="rounded-full px-1.5 text-xs {selected === cat.id ? 'bg-primary-content/20' : 'bg-base-200 text-base-content/50'}">
          {categoryCount(cat.id)}
        </span>
      </button>
    {/each}
  </div>

  {#if loading}
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {#each [1, 2, 3, 4] as _}
        <div class="rounded-2xl border border-base-300 bg-base-100 p-4">
          <div class="flex items-center gap-3">
            <div class="h-10 w-10 rounded-xl bg-base-200"></div>
            <div class="flex-1 space-y-2">
              <div class="h-3 w-1/3 rounded bg-base-200"></div>
              <div class="h-2.5 w-3/4 rounded bg-base-200"></div>
            </div>
          </div>
          <div class="mt-4 space-y-2">
            <div class="h-8 rounded-lg bg-base-200"></div>
            <div class="h-8 rounded-lg bg-base-200"></div>
          </div>
        </div>
      {/each}
    </div>
  {:else if loadError}
    <div class="rounded-2xl border border-error/30 bg-base-100 p-10 text-center">
      <p class="text-sm font-semibold text-error">软件包列表加载失败</p>
      <p class="mt-1 text-xs text-base-content/60">{loadError}</p>
      <button class="btn btn-sm btn-outline mt-4" onclick={load}>重新加载</button>
    </div>
  {:else if filteredGroups.length === 0}
    <div class="flex flex-col items-center justify-center rounded-2xl border border-dashed border-base-300 bg-base-100/60 py-16 text-base-content/50">
      <p class="text-sm font-medium">没有匹配的软件</p>
      <p class="mt-1 text-xs">换个关键词或分类试试</p>
    </div>
  {:else}
    <div class="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
      {#each filteredGroups as group (group.kind)}
        <article class="overflow-hidden rounded-2xl border border-base-300 bg-base-100 shadow-sm">
          <header class="flex items-start gap-3 border-b border-base-200 p-4">
            <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl text-white" style="background:{group.color}">
              {#if group.icon}
                <span class="iconfont text-xl leading-none {group.icon}"></span>
              {:else}
                <span class="text-sm font-bold">{group.name.charAt(0).toUpperCase()}</span>
              {/if}
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <h3 class="truncate text-[15px] font-semibold">{group.name}</h3>
                {#if group.installedCount > 0}
                  <span class="shrink-0 rounded-full bg-success/10 px-2 py-0.5 text-[11px] font-medium text-success">已安装</span>
                {/if}
              </div>
              <p class="mt-0.5 truncate text-xs text-base-content/55" title={group.desc}>{group.desc}</p>
            </div>
            <span class="shrink-0 rounded-full bg-base-200 px-2.5 py-1 text-[11px] text-base-content/50">
              {categories.find((c) => c.id === group.category)?.label || group.category}
            </span>
          </header>

          <div class="divide-y divide-base-200">
            {#each group.versions as rel, i (rel.version)}
              {@const key = `${group.kind}-${rel.version}`}
              {@const prog = getProgress(key)}
              <div class="flex items-center gap-3 px-4 py-2.5">
                <span class="font-mono text-sm {rel.installed ? 'text-base-content' : 'text-base-content/70'}">v{rel.version}</span>
                {#if i === 0 && group.versions.length > 1}
                  <span class="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-primary">最新</span>
                {/if}

                {#if rel.installed}
                  <span class="flex items-center gap-1 text-xs text-success">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" class="h-3.5 w-3.5"><path d="m5 13 4 4L19 7"></path></svg>
                    已安装
                  </span>
                {:else if installing[key]}
                  <div class="flex min-w-0 flex-1 items-center gap-2 text-xs text-base-content/60">
                    {#if prog.total > 0}
                      <progress class="progress progress-primary h-1.5 w-24" value={prog.downloaded} max={prog.total}></progress>
                      <span>{formatBytes(prog.downloaded)} / {formatBytes(prog.total)}</span>
                      <span class="text-primary">{Math.round((prog.downloaded / prog.total) * 100)}%</span>
                    {:else}
                      <span class="loading loading-spinner loading-xs"></span>
                      <span>准备下载… {formatElapsed(installElapsed[key] || 0)}</span>
                    {/if}
                  </div>
                {:else if uninstalling[key]}
                  <span class="flex items-center gap-1.5 text-xs text-warning">
                    <span class="loading loading-spinner loading-xs"></span>卸载中…
                  </span>
                {:else}
                  <span class="text-xs text-base-content/40">未安装</span>
                {/if}

                <div class="ml-auto flex shrink-0 items-center gap-1.5">
                  {#if rel.installed}
                    {#if uninstalling[key]}
                      <span class="loading loading-spinner loading-xs"></span>
                    {:else}
                      <button class="btn btn-ghost btn-error btn-xs" onclick={() => uninstall(group, rel)}>卸载</button>
                    {/if}
                  {:else if installing[key]}
                    {#if cancelling[key]}
                      <span class="loading loading-spinner loading-xs"></span>
                      <span class="text-xs text-warning">取消中…</span>
                    {:else}
                      <button class="btn btn-ghost btn-warning btn-xs" onclick={() => cancelInstall(group, rel)}>取消</button>
                    {/if}
                  {:else}
                    <button class="btn btn-primary btn-xs" onclick={() => install(group, rel)}>下载安装</button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          {#if group.category === 'tools'}
            <div class="border-t border-base-200 bg-base-200/40 px-4 py-2.5 text-xs text-base-content/50">
              {#if group.installedCount > 0 && installDir}
                安装位置：<code class="font-mono text-base-content/70">{installDir}\programs\{group.kind}\{installedExample(group)}</code>
              {:else if group.kind === 'composer'}
                安装后可将 <code class="font-mono">composer.phar</code> 复制到项目目录，执行 <code class="font-mono">php composer.phar</code> 安装依赖
              {:else}
                安装后可在「项目与网站」中创建站点，将根目录指向该安装目录
              {/if}
            </div>
          {/if}
        </article>
      {/each}
    </div>
  {/if}
</div>
