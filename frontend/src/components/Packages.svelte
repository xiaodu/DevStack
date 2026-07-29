<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let packages = []
let selected = 'all'
let installing = {}
let uninstalling = {}
let loading = true

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

async function load() {
  loading = true
  packages = []
  try {
    const services = await api.getServices()
    for (const svc of services) {
      const releases = (await api.getAvailableVersions(svc.id)) || []
      for (const rel of releases) {
        const installed = await api.checkVersionInstalled(svc.id, rel.version)
        packages.push({
          id: svc.id,
          name: serviceNames[svc.id] || svc.name,
          version: rel.version,
          category: svc.category,
          installed,
          isCurrent: svc.version === rel.version && svc.installed,
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

async function install(pkg) {
  const key = `${pkg.id}-${pkg.version}`
  installing[key] = true
  packages = [...packages]
  await safeInvoke(
    async () => {
      await api.downloadService(pkg.id, pkg.version)
      await load()
    },
    { successMsg: `${pkg.name} ${pkg.version} 安装成功`, errorPrefix: `安装失败` },
  )
  installing[key] = false
  packages = [...packages]
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

$: filtered = selected === 'all' ? packages : packages.filter((p) => p.category === selected)
onMount(load)
</script>

<div class="grid grid-cols-[220px_1fr] h-[calc(100vh-48px)] -m-6 -mx-8 bg-base-100">
  <div class="border-r border-base-300 flex flex-col">
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">软件仓库</h3></div>
    <div class="flex-1 p-2 overflow-y-auto">
      {#each categories as cat}
        <button class="flex items-center gap-2.5 w-full px-3 py-2.5 rounded-lg text-left text-sm transition-colors duration-100 border-none {selected === cat.id ? 'bg-primary text-primary-content' : 'text-base-content/70 hover:bg-base-200'}"
          on:click={() => selected = cat.id}>
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
            <th class="text-sm">下载</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as pkg}
            {@const key = `${pkg.id}-${pkg.version}`}
            <tr class={pkg.isCurrent ? 'bg-primary/5' : ''}>
              <td class="font-medium text-sm">{pkg.name}</td>
              <td class="font-mono text-sm text-base-content/70">
                {pkg.version}
                {#if pkg.isCurrent}
                  <span class="badge badge-xs badge-primary ml-1.5">当前使用</span>
                {/if}
              </td>
              <td class="text-sm">
                {#if pkg.installed}
                  <span class="badge badge-success badge-sm">已安装</span>
                {:else if installing[key]}
                  <span class="badge badge-warning badge-sm">安装中...</span>
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
                      <button class="btn btn-ghost btn-sm text-error" on:click={() => uninstall(pkg)} title="卸载">🗑 卸载</button>
                    {/if}
                  {:else if installing[key]}
                    <span class="loading loading-spinner loading-xs"></span>
                  {:else}
                    <button class="btn btn-primary btn-sm" on:click={() => install(pkg)} title="安装">下载</button>
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
