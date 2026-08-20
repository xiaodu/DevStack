<script lang="ts">
import { createEventDispatcher, onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'

let { service }: { service: any } = $props()
const dispatch = createEventDispatcher()

let nc = $state<Record<string, any>>({})
let configLoading = $state(false)

const bodySizeOptions = ['1M', '8M', '16M', '32M', '64M', '128M', '256M', '512M', '1024M']
const recommendedConfig: Record<string, string> = {
  workerConnections: '2048',
  clientMaxBodySize: '64m',
  gzipEnabled: 'true',
  staticCacheDays: '7',
  keepaliveTimeout: '65',
  serverTokens: 'false',
}

const iconClassMap: Record<string, string> = { nginx: 'icon-nginx', apache: 'icon-nginx1' }
let iconClass = $derived(iconClassMap[service.kind] || '')

onMount(() => {
  if (service.kind === 'nginx') loadConfig()
})

async function loadConfig() {
  configLoading = true
  try {
    nc = await api.getServiceConfig('nginx')
  } catch (e) {
    console.error(e)
  } finally {
    configLoading = false
  }
}

function intVal(key: string) {
  return parseInt(nc[key], 10) || 0
}
function boolVal(key: string) {
  return nc[key] === 'true'
}
function getRecommendedHint(key: string, label?: string) {
  const v = recommendedConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommended() {
  nc = { ...recommendedConfig }
}

async function save() {
  await safeInvoke(
    async () => {
      await api.saveServiceConfig(service.id, {
        workerConnections: String(intVal('workerConnections')),
        clientMaxBodySize: nc.clientMaxBodySize || '64m',
        gzipEnabled: String(boolVal('gzipEnabled')),
        staticCacheDays: String(intVal('staticCacheDays')),
        keepaliveTimeout: String(intVal('keepaliveTimeout')),
        serverTokens: String(boolVal('serverTokens')),
      })
      await api.applyServiceConfig('nginx')
      dispatch('change')
    },
    { successMsg: 'Nginx 配置已保存并生效', errorPrefix: '保存失败' },
  )
}

async function toggle() {
  await safeInvoke(
    async () => {
      if (service.running) await api.stopService(service.id)
      else await api.startService(service.id)
      dispatch('change')
    },
    { errorPrefix: '操作失败' },
  )
}
</script>

<div class="p-6">
  <div class="flex justify-between items-center mb-4">
    <h2 class="text-xl font-semibold flex items-center gap-2.5">
      {#if iconClass}<span class="iconfont text-2xl leading-none {iconClass}"></span>{/if}
      {service.name}
      <span class="text-base font-normal text-base-content/50">{service.version}</span>
    </h2>
    <button class="btn btn-sm {service.running ? 'btn-error' : 'btn-success'}" onclick={toggle}>
      {service.running ? '停止' : '启动'}
    </button>
  </div>

  <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg mb-6 text-sm">
    <span class="w-2 h-2 rounded-full {service.running ? 'bg-success' : 'bg-base-content/40'}"></span>
    <span class="text-base-content/70">{service.running ? '运行中' : '已停止'}</span>
    {#if service.running && service.uptime}<span class="text-base-content/60">运行 {service.uptime}</span>{/if}
    {#if service.port}<span class="ml-auto text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">端口 {service.port}</span>{/if}
  </div>

  {#if service.kind === 'nginx'}
    {#if configLoading}
      <div class="text-center py-10 text-base-content/60">加载配置...</div>
    {:else}
      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <div class="flex justify-between items-center mb-3.5">
          <h3 class="text-sm font-semibold text-primary">性能配置</h3>
          <button class="btn btn-sm btn-outline" onclick={loadRecommended}>加载推荐值</button>
        </div>
        <div class="flex flex-col gap-3">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full">
              <div class="label"><span class="label-text">最大并发连接数</span></div>
              <input class="input input-bordered w-full input-sm" type="number" min="1024" max="8192" step="512" value={intVal('workerConnections')} onchange={e => nc.workerConnections = String((e.currentTarget as HTMLInputElement).value)} />
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('workerConnections', 'CPU核数 × 1024')}</span></div>
            </label>
            <label class="form-control w-full">
              <div class="label"><span class="label-text">Keepalive 超时(秒)</span></div>
              <input class="input input-bordered w-full input-sm" type="number" min="10" max="120" value={intVal('keepaliveTimeout')} onchange={e => nc.keepaliveTimeout = String((e.currentTarget as HTMLInputElement).value)} />
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('keepaliveTimeout', '长连接保持时间')}</span></div>
            </label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full">
              <div class="label"><span class="label-text">上传文件最大大小</span></div>
              <select class="select select-bordered w-full select-sm" bind:value={nc.clientMaxBodySize}>
                {#each bodySizeOptions as opt}<option value={opt.toLowerCase()}>{opt}</option>{/each}
              </select>
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('clientMaxBodySize')}</span></div>
            </label>
            <label class="form-control w-full">
              <div class="label"><span class="label-text">静态资源缓存(天)</span></div>
              <input class="input input-bordered w-full input-sm" type="number" min="0" max="365" value={intVal('staticCacheDays')} onchange={e => nc.staticCacheDays = String((e.currentTarget as HTMLInputElement).value)} />
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('staticCacheDays', '0 = 不缓存')}</span></div>
            </label>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">功能开关</h3>
        <div class="grid grid-cols-2 gap-3">
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('gzipEnabled')} onchange={e => nc.gzipEnabled = String((e.currentTarget as HTMLInputElement).checked)} />
            <div><span class="text-sm">Gzip 压缩</span><p class="text-xs text-base-content/50">{getRecommendedHint('gzipEnabled', '减少传输大小 60-80%')}</p></div>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('serverTokens')} onchange={e => nc.serverTokens = String((e.currentTarget as HTMLInputElement).checked)} />
            <div><span class="text-sm">显示服务器版本</span><p class="text-xs text-base-content/50">{getRecommendedHint('serverTokens', '建议关闭')}</p></div>
          </label>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">文件信息</h3>
        <div class="grid grid-cols-2 gap-3">
          <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="nginx/{service.version}/conf/nginx.conf" readonly /></label>
          <label class="form-control w-full"><div class="label"><span class="label-text">默认网站根目录</span></div><input class="input input-bordered w-full input-sm" value="www/" readonly /></label>
        </div>
      </div>

      <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
        <button class="btn btn-primary" onclick={save}>保存并生效</button>
        <span class="text-xs text-base-content/50">保存后自动重新生成配置文件并重启 Nginx</span>
      </div>
    {/if}
  {:else}
    <div class="card bg-base-100 border border-base-300 p-5">
      <p class="text-sm text-base-content/60">暂无可配置项，请升级版本或更换其他 Web 服务器</p>
    </div>
  {/if}
</div>