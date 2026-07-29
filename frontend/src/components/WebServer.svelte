<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let services = []
let selectedId = 'nginx'
let loading = true
let nginxConfig = {}
let configLoading = false

const bodySizeOptions = ['1M', '8M', '16M', '32M', '64M', '128M', '256M', '512M', '1024M']
const recommendedConfig = {
  workerConnections: '2048',
  clientMaxBodySize: '64m',
  gzipEnabled: 'true',
  staticCacheDays: '7',
  keepaliveTimeout: '65',
  serverTokens: 'false',
}

async function load() {
  try {
    const all = await api.getServices()
    services = all.filter((s) => s.category === 'webserver' && s.installed)
    if (services.length > 0) selectedId = services[0].id
    await loadConfig()
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

async function loadConfig() {
  configLoading = true
  try {
    nginxConfig = await api.getServiceConfig('nginx')
  } catch (e) {
    console.error(e)
  } finally {
    configLoading = false
  }
}

function intVal(key) {
  return parseInt(nginxConfig[key], 10) || 0
}
function boolVal(key) {
  return nginxConfig[key] === 'true'
}
function getRecommendedHint(key, label) {
  const v = recommendedConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommended() {
  nginxConfig = { ...recommendedConfig }
  addToast('已加载推荐配置')
}

async function saveNginxConfig() {
  await safeInvoke(
    async () => {
      await api.saveServiceConfig('nginx', {
        workerConnections: String(intVal('workerConnections')),
        clientMaxBodySize: nginxConfig.clientMaxBodySize || '64m',
        gzipEnabled: String(boolVal('gzipEnabled')),
        staticCacheDays: String(intVal('staticCacheDays')),
        keepaliveTimeout: String(intVal('keepaliveTimeout')),
        serverTokens: String(boolVal('serverTokens')),
      })
      await api.applyServiceConfig('nginx')
      await load()
    },
    { successMsg: 'Nginx 配置已保存并生效', errorPrefix: '保存失败' },
  )
}

async function toggle(id) {
  await safeInvoke(
    async () => {
      const env = await api.getEnvironment()
      const svc = env.services[id]
      if (svc) {
        svc.state = svc.state === 'running' ? 'stopped' : 'running'
        await api.applyEnvironment(env)
      }
      await load()
    },
    { errorPrefix: '操作失败' },
  )
}

$: current = selectedId ? services.find((s) => s.id === selectedId) : null
onMount(load)
</script>

<div class="grid grid-cols-[260px_1fr] h-[calc(100vh-48px)] -m-6 -mx-8 bg-base-100">
  <div class="border-r border-base-300 flex flex-col">
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">Web 服务器</h3></div>
    <div class="flex-1 p-2 overflow-y-auto">
      {#each services as svc}
          <button class="flex items-center gap-2.5 w-full px-3 py-2.5 rounded-lg text-left transition-colors border-none {selectedId === svc.id ? 'bg-primary/10' : ''}"
          on:click={() => { selectedId = svc.id; loadConfig() }}>
          <span class="text-lg">⚡</span>
          <div class="flex-1 min-w-0">
            <div class="text-sm font-medium">{svc.name}</div>
            <div class="text-xs text-base-content/60">v{svc.version}</div>
          </div>
          <span class="w-2 h-2 rounded-full shrink-0 {svc.status === 'running' ? 'bg-success' : 'bg-base-content/40'}"></span>
        </button>
      {/each}
    </div>
  </div>

  <div class="overflow-y-auto p-6">
    {#if current}
      <div class="flex justify-between items-center mb-4">
        <h2 class="text-xl font-semibold">{current.name} {current.version}</h2>
        <button class="btn btn-sm {current.status === 'running' ? 'btn-error' : 'btn-success'}" on:click={() => toggle(current.id)}>
          {current.status === 'running' ? '停止' : '启动'}
        </button>
      </div>

      <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg mb-6 text-sm">
        <span class="w-2 h-2 rounded-full {current.status === 'running' ? 'bg-success' : 'bg-base-content/40'}"></span>
        <span class="text-base-content/70">{current.status === 'running' ? '运行中' : '已停止'}</span>
        {#if current.status === 'running' && current.uptime}<span class="text-base-content/60">运行 {current.uptime}</span>{/if}
        {#if current.port}<span class="ml-auto text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">{current.port}</span>{/if}
      </div>

      {#if configLoading}
        <div class="text-center py-10 text-base-content/60">加载配置...</div>
      {:else}
        <div class="card bg-base-100 border border-base-300 p-5 mb-4">
          <div class="flex justify-between items-center mb-3.5">
            <h3 class="text-sm font-semibold text-primary">性能配置</h3>
            <button class="btn btn-sm btn-outline" on:click={loadRecommended}>加载推荐值</button>
          </div>
          <div class="flex flex-col gap-3">
            <div class="grid grid-cols-2 gap-3">
              <label class="form-control w-full">
                <div class="label"><span class="label-text">最大并发连接数</span></div>
                <input class="input input-bordered w-full input-sm" type="number" min="1024" max="8192" step="512"
                  value={intVal('workerConnections')} on:change={e => nginxConfig.workerConnections = String(e.target.value)} />
                <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('workerConnections', 'CPU核数 × 1024')}</span></div>
              </label>
              <label class="form-control w-full">
                <div class="label"><span class="label-text">Keepalive 超时(秒)</span></div>
                <input class="input input-bordered w-full input-sm" type="number" min="10" max="120"
                  value={intVal('keepaliveTimeout')} on:change={e => nginxConfig.keepaliveTimeout = String(e.target.value)} />
                <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('keepaliveTimeout', '长连接保持时间')}</span></div>
              </label>
            </div>
            <div class="grid grid-cols-2 gap-3">
              <label class="form-control w-full">
                <div class="label"><span class="label-text">上传文件最大大小</span></div>
                <select class="select select-bordered w-full select-sm" bind:value={nginxConfig.clientMaxBodySize}>
                  {#each bodySizeOptions as opt}<option value={opt.toLowerCase()}>{opt}</option>{/each}
                </select>
                <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('clientMaxBodySize')}</span></div>
              </label>
              <label class="form-control w-full">
                <div class="label"><span class="label-text">静态资源缓存(天)</span></div>
                <input class="input input-bordered w-full input-sm" type="number" min="0" max="365"
                  value={intVal('staticCacheDays')} on:change={e => nginxConfig.staticCacheDays = String(e.target.value)} />
                <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('staticCacheDays', '0 = 不缓存')}</span></div>
              </label>
            </div>
          </div>
        </div>

        <div class="card bg-base-100 border border-base-300 p-5 mb-4">
          <h3 class="text-sm font-semibold text-primary mb-3.5">功能开关</h3>
          <div class="grid grid-cols-2 gap-3">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('gzipEnabled')} on:change={e => nginxConfig.gzipEnabled = String(e.target.checked)} />
              <div><span class="text-sm">Gzip 压缩</span><p class="text-xs text-base-content/50">{getRecommendedHint('gzipEnabled', '减少传输大小 60-80%')}</p></div>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('serverTokens')} on:change={e => nginxConfig.serverTokens = String(e.target.checked)} />
              <div><span class="text-sm">显示服务器版本</span><p class="text-xs text-base-content/50">{getRecommendedHint('serverTokens', '建议关闭')}</p></div>
            </label>
          </div>
        </div>

        <div class="card bg-base-100 border border-base-300 p-5 mb-4">
          <h3 class="text-sm font-semibold text-primary mb-3.5">文件信息</h3>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="nginx/{current.version}/conf/nginx.conf" readonly /></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">默认网站根目录</span></div><input class="input input-bordered w-full input-sm" value="www/" readonly /></label>
          </div>
        </div>

        <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
          <button class="btn btn-primary" on:click={saveNginxConfig}>保存并生效</button>
          <span class="text-xs text-base-content/50">保存后自动重新生成配置文件并重启 Nginx</span>
        </div>
      {/if}
    {:else}
      <div class="flex flex-col items-center justify-center h-full text-base-content/60">
        <div class="text-4xl opacity-50 mb-3">⚡</div>
        <p>未找到 Web 服务器</p>
        <p class="text-xs mt-1 text-base-content/50">请前往「软件包」页面安装 Nginx</p>
      </div>
    {/if}
  </div>
</div>
