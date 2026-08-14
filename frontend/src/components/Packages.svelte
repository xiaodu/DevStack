<script lang="ts">
import { onDestroy, onMount } from 'svelte'
import { api } from '../lib/api'
import { useDownloadProgress } from '../lib/events'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let packages = $state([])
let selected = $state('all')
let installing = $state({})
let uninstalling = $state({})
let loading = $state(true)
let installProgress = {} // { 'nginx-1.24.0': { downloaded: 0, total: 0 } }
let installElapsed = $state({}) // { 'nginx-1.24.0': seconds }
let elapsedTimer = null
const cancelledInstalls = new Set() // 已取消的安装，下载结束后不再弹成功/失败提示

const categories = [
  { id: 'all', label: '全部' },
  { id: 'webserver', label: 'Web 服务器' },
  { id: 'database', label: '数据库' },
  { id: 'runtime', label: '运行环境' },
  { id: 'service', label: '基础服务' },
]

const serviceNames = {
  nginx: 'Nginx',
  apache: 'Apache',
  mysql: 'MySQL',
  postgresql: 'PostgreSQL',
  mongodb: 'MongoDB',
  redis: 'Redis',
  memcached: 'Memcached',
  php: 'PHP',
  node: 'Node.js',
  python: 'Python',
  composer: 'Composer',
}

// 监听下载进度（后端 key 格式 "download:nginx:1.24.0"）
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
  packages = []
  try {
    const kinds = Object.keys(serviceNames)
    for (const kind of kinds) {
      const releases = (await api.getAvailableVersions(kind)) || []
      for (const rel of releases) {
        const installed = await api.checkVersionInstalled(kind, rel.version)
        packages.push({
          id: kind,
          name: serviceNames[kind],
          version: rel.version,
          category: categoryFor(kind),
          installed,
        })
      }
    }
    packages = [...packages]
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

function categoryFor(kind) {
  if (kind === 'nginx' || kind === 'apache') return 'webserver'
  if (kind === 'mysql' || kind === 'postgresql' || kind === 'mongodb') return 'database'
  if (kind === 'redis' || kind === 'memcached') return 'service'
  return 'runtime'
}

function formatBytes(bytes) {
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

function formatElapsed(seconds) {
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

async function install(pkg) {
  const key = `${pkg.id}-${pkg.version}`
  if (installing[key]) return
  cancelledInstalls.delete(key)
  installing[key] = true
  installProgress[key] = { downloaded: 0, total: 0 }
  installElapsed[key] = 0
  packages = [...packages]

  // 计时器
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
    await api.downloadService(pkg.id, pkg.version)
    if (cancelledInstalls.has(key)) return
    await load()
    addToast(`${pkg.name} ${pkg.version} 安装成功`, 'success')
  } catch (e) {
    if (cancelledInstalls.has(key)) return
    addToast(`安装 ${pkg.name} ${pkg.version} 失败: ${e.message || e}`, 'error')
  } finally {
    installing[key] = false
    delete installProgress[key]
    delete installElapsed[key]
    installProgress = { ...installProgress }
    packages = [...packages]
  }
}

function cancelInstall(pkg) {
  const key = `${pkg.id}-${pkg.version}`
  if (!installing[key]) return
  installing[key] = false
  cancelledInstalls.add(key)
  delete installProgress[key]
  delete installElapsed[key]
  installProgress = { ...installProgress }
  packages = [...packages]
  addToast(`已取消安装 ${pkg.name} ${pkg.version}`, 'warning')
}

function getProgress(key) {
  return installProgress[key] || { downloaded: 0, total: 0 }
}

async function uninstall(pkg) {
  if (!confirm(`确定卸载 ${pkg.name} ${pkg.version}？`)) return
  const key = `${pkg.id}-${pkg.version}`
  uninstalling[key] = true
  packages = [...packages]
  await safeInvoke(
    async () => {
      await api.uninstallVersion(pkg.id, pkg.version)
      await load()
    },
    { successMsg: `${pkg.name} ${pkg.version} 卸载成功`, errorPrefix: `卸载失败` },
  )
  uninstalling[key] = false
  packages = [...packages]
}

let filtered = $derived(selected === 'all' ? packages : packages.filter((p) => p.category === selected))
onMount(load)
</script>

<div class="grid grid-cols-[220px_1fr] h-[calc(100vh-48px)] -m-6 -mx-8 bg-base-100">
  <div class="border-r border-base-300 flex flex-col">
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">软件仓库</h3></div>
    <div class="flex-1 p-2 overflow-y-auto">
      {#each categories as cat}
        <button class="flex items-center gap-2.5 w-full px-3 py-2.5 rounded-lg text-left text-sm transition-colors duration-100 border-none {selected === cat.id ? 'bg-primary text-primary-content' : 'text-base-content/70 hover:bg-base-200'}"
          onclick={() => selected = cat.id}>
          {cat.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="overflow-y-auto">
    <div class="px-6 py-4 border-b border-base-300"><h2 class="text-lg font-semibold">{categories.find(c => c.id === selected)?.label || ''}</h2></div>

    {#if loading}
      <div class="text-center py-10 text-base-content/60">加载软件包中...</div>
    {:else if filtered.length === 0}
      <div class="text-center py-10 text-base-content/60">暂无软件包</div>
    {:else}
      <table class="table">
        <thead>
          <tr>
            <th class="text-sm">包名称</th>
            <th class="text-sm">版本</th>
            <th class="text-sm">状态</th>
            <th class="text-sm">操作</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as pkg}
            {@const key = `${pkg.id}-${pkg.version}`}
            {@const prog = getProgress(key)}
            <tr>
              <td class="font-medium text-sm">{pkg.name}</td>
              <td class="font-mono text-sm text-base-content/70">
                {pkg.version}
              </td>
              <td class="text-sm min-w-[160px]">
                {#if pkg.installed}
                  <span class="badge badge-success badge-sm">已安装</span>
                {:else if installing[key]}
                  <div class="flex flex-col gap-1">
                    <span class="badge badge-warning badge-sm self-start">安装中...</span>
                    {#if prog.total > 0}
                      <div class="flex items-center gap-1.5 text-xs text-base-content/60">
                        <progress class="progress progress-primary w-24 h-1.5" value={prog.downloaded} max={prog.total}></progress>
                        <span>{Math.round((prog.downloaded / prog.total) * 100)}%</span>
                      </div>
                    {:else}
                      <div class="flex items-center gap-1.5 text-xs text-base-content/60">
                        <span class="loading loading-spinner loading-xs"></span>
                        <span>{formatElapsed(installElapsed[key] || 0)}</span>
                      </div>
                    {/if}
                  </div>
                {:else if uninstalling[key]}
                  <span class="badge badge-warning badge-sm">卸载中...</span>
                {:else}
                  <span class="badge badge-ghost badge-sm">未安装</span>
                {/if}
              </td>
              <td>
                <div class="flex gap-1.5 items-center">
                  {#if pkg.installed}
                    {#if uninstalling[key]}
                      <span class="loading loading-spinner loading-xs"></span>
                    {:else}
                      <button class="btn btn-ghost btn-sm text-error" onclick={() => uninstall(pkg)} title="卸载">🗑 卸载</button>
                    {/if}
                  {:else if installing[key]}
                    <button class="btn btn-ghost btn-sm text-warning" onclick={() => cancelInstall(pkg)} title="取消安装">取消</button>
                  {:else}
                    <button class="btn btn-primary btn-sm" onclick={() => install(pkg)} title="安装">下载</button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
