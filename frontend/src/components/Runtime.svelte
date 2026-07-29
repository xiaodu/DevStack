<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast, refreshSignal } from '../lib/stores'

let runtimes = []
let selectedId = ''
let loading = true
let phpConfig = {}
let configLoading = false

const typeMeta = {
  php: { label: 'PHP', color: '#777BB4' },
  node: { label: 'Node.js', color: '#339933' },
  python: { label: 'Python', color: '#3776AB' },
}
const rtIcons = { php: '🐘', node: '💚', python: '🐍' }
const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
const uploadOptions = ['2M', '8M', '16M', '32M', '64M', '128M', '256M', '512M']
const realpathOptions = ['512K', '1024K', '2048K', '4096K', '8192K']
const recommendedPHPConfig = {
  memoryLimit: '256M',
  maxExecutionTime: '300',
  uploadMaxFilesize: '64M',
  postMaxSize: '64M',
  displayErrors: 'false',
  opcacheEnabled: 'true',
  realpathCacheSize: '4096K',
}

async function load() {
  loading = true
  try {
    runtimes = await api.getRuntimes()
    if (runtimes.length > 0 && !selectedId) selectedId = runtimes[0].id
    if (current && current.type === 'php') await loadPHPConfig()
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

async function loadPHPConfig() {
  configLoading = true
  try {
    phpConfig = await api.getServiceConfig('php')
  } catch (e) {
    console.error(e)
  } finally {
    configLoading = false
  }
}

function intVal(key) {
  return parseInt(phpConfig[key], 10) || 0
}
function boolVal(key) {
  return phpConfig[key] === 'true'
}
function getRecommendedHint(key, label) {
  const v = recommendedPHPConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommendedPHP() {
  phpConfig = { ...recommendedPHPConfig }
  addToast('已加载推荐配置')
}

async function toggle(rt) {
  await safeInvoke(
    async () => {
      const isRunning = rt.running || rt.status === 'running'
      const env = await api.getEnvironment()
      if (!env?.runtimes?.[rt.id]) {
        addToast('未找到该运行时的声明式状态，请先在「软件包」中注册', 'error')
        return
      }
      env.runtimes[rt.id].state = isRunning ? 'stopped' : 'running'
      await api.applyEnvironment(env)
      await load()
    },
    { successMsg: `${rt.name} 状态已切换`, errorPrefix: '操作失败' },
  )
}

async function savePHPConfig(rt) {
  await safeInvoke(
    async () => {
      await api.saveServiceConfig(rt.id, {
        memoryLimit: phpConfig.memoryLimit || '256M',
        maxExecutionTime: String(intVal('maxExecutionTime')),
        uploadMaxFilesize: phpConfig.uploadMaxFilesize || '64M',
        postMaxSize: phpConfig.postMaxSize || '64M',
        displayErrors: String(boolVal('displayErrors')),
        opcacheEnabled: String(boolVal('opcacheEnabled')),
        realpathCacheSize: phpConfig.realpathCacheSize || '4096K',
      })
      await load()
    },
    { successMsg: 'PHP 配置已保存，重启后生效', errorPrefix: '保存失败' },
  )
}

function statusLabel(rt) {
  if (rt.running || rt.status === 'running') return '运行中'
  if (rt.status === 'starting') return '启动中'
  if (rt.status === 'stopping') return '停止中'
  return '已停止'
}

function selectRuntime(id) {
  selectedId = id
  if (current?.type === 'php') loadPHPConfig()
  else {
    phpConfig = {}
    configLoading = false
  }
}

$: current = selectedId ? runtimes.find((r) => r.id === selectedId) : null
$: currentMeta = current ? typeMeta[current.type] || { label: '', color: 'var(--accent)' } : null
const unsub = refreshSignal.subscribe(() => load())
onMount(() => {
  load()
  return unsub
})
</script>

<div class="grid grid-cols-[260px_1fr] h-[calc(100vh-48px)] -m-6 -mx-8 bg-base-100">
  <div class="border-r border-base-300 flex flex-col">
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">运行环境</h3></div>
    <div class="flex-1 p-2 overflow-y-auto">
      {#if loading}
        <div class="text-center py-6 text-base-content/60 text-sm">加载运行环境中...</div>
      {:else if runtimes.length === 0}
        <div class="text-center py-6 text-base-content/60 text-sm">暂无运行环境</div>
      {:else}
        {#each runtimes as rt}
          {@const meta = typeMeta[rt.type] || { color: 'var(--accent)' }}
          <button class="flex items-center gap-2.5 w-full px-3 py-2.5 rounded-lg text-left transition-colors border-none {selectedId === rt.id ? 'bg-primary/10' : ''}"
            on:click={() => selectRuntime(rt.id)}>
            <span class="w-2 h-2 rounded-full shrink-0" style="background:{meta.color}"></span>
            <div class="flex-1 min-w-0">
              <div class="text-sm font-medium">{rt.name}</div>
              <div class="text-xs text-base-content/60">v{rt.version}</div>
            </div>
            <span class="w-2 h-2 rounded-full shrink-0 {(rt.running || rt.status === 'running') ? 'bg-success' : 'bg-base-content/40'}"></span>
          </button>
        {/each}
      {/if}
    </div>
  </div>

  <div class="overflow-y-auto p-6">
    {#if current}
      <div class="flex justify-between items-center mb-4">
        <h2 class="text-xl font-semibold flex items-center gap-2.5">
          <span class="text-2xl" style="color:{currentMeta.color}">{rtIcons[current.type] || '⚙'}</span>
          {current.name}
        </h2>
        <button class="btn btn-sm {(current.running || current.status === 'running') ? 'btn-error' : 'btn-success'}" on:click={() => toggle(current)}>
          {(current.running || current.status === 'running') ? '停止' : '启动'}
        </button>
      </div>

      <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg mb-6 text-sm">
        <span class="w-2 h-2 rounded-full {(current.running || current.status === 'running') ? 'bg-success' : 'bg-base-content/40'}"></span>
        <span class="text-base-content/70">{statusLabel(current)}</span>
        {#if current.active}<span class="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">命令行默认</span>{/if}
        {#if current.port}<span class="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">端口 {current.port}</span>{/if}
        {#if (current.running || current.status === 'running') && current.uptime}<span class="text-base-content/60">运行 {current.uptime}</span>{/if}
      </div>

      {#if current.type === 'php'}
        {#if configLoading}
          <div class="text-center py-10 text-base-content/60">加载配置...</div>
        {:else}
          <div class="card bg-base-100 border border-base-300 p-5 mb-4">
            <div class="flex justify-between items-center mb-3.5">
              <h3 class="text-sm font-semibold text-primary">PHP 配置</h3>
              <button class="btn btn-sm btn-outline" on:click={loadRecommendedPHP}>加载推荐值</button>
            </div>
            <div class="flex flex-col gap-3">
              <div class="grid grid-cols-2 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">内存限制</span></div><select class="select select-bordered w-full select-sm" bind:value={phpConfig.memoryLimit}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('memoryLimit')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">最大执行时间(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="30" max="600" value={intVal('maxExecutionTime')} on:change={e => phpConfig.maxExecutionTime = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxExecutionTime')}</span></div></label>
              </div>
              <div class="grid grid-cols-2 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">上传文件最大大小</span></div><select class="select select-bordered w-full select-sm" bind:value={phpConfig.uploadMaxFilesize}>{#each uploadOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('uploadMaxFilesize')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">POST 数据最大大小</span></div><select class="select select-bordered w-full select-sm" bind:value={phpConfig.postMaxSize}>{#each uploadOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('postMaxSize')}</span></div></label>
              </div>
              <div class="grid grid-cols-2 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">Realpath 缓存大小</span></div><select class="select select-bordered w-full select-sm" bind:value={phpConfig.realpathCacheSize}>{#each realpathOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('realpathCacheSize', 'Windows 性能关键')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="php/{current.version}/php.ini" readonly /></label>
              </div>
            </div>
          </div>

          <div class="card bg-base-100 border border-base-300 p-5 mb-4">
            <h3 class="text-sm font-semibold text-primary mb-3.5">功能开关</h3>
            <div class="grid grid-cols-2 gap-3">
              <label class="flex items-center gap-2 cursor-pointer">
                <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('displayErrors')} on:change={e => phpConfig.displayErrors = String(e.target.checked)} />
                <div><span class="text-sm">显示错误信息</span><p class="text-xs text-base-content/50">{getRecommendedHint('displayErrors', '生产环境建议关闭')}</p></div>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('opcacheEnabled')} on:change={e => phpConfig.opcacheEnabled = String(e.target.checked)} />
                <div><span class="text-sm">启用 OPcache</span><p class="text-xs text-base-content/50">{getRecommendedHint('opcacheEnabled', '性能提升 30-50%')}</p></div>
              </label>
            </div>
          </div>

          <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
            <button class="btn btn-primary" on:click={() => savePHPConfig(current)}>保存配置</button>
            <span class="text-xs text-base-content/50">保存后重新生成 php.ini，需重启 PHP 生效</span>
          </div>
        {/if}
      {:else}
        <div class="card bg-base-100 border border-base-300 p-5 mb-4">
          <h3 class="text-sm font-semibold text-primary mb-3.5">{currentMeta.label} 信息</h3>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">类型</span></div><input class="input input-bordered w-full input-sm" value={currentMeta.label || current.type} readonly /></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">版本</span></div><input class="input input-bordered w-full input-sm" value={current.version} readonly /></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">安装路径</span></div><input class="input input-bordered w-full input-sm" value={current.path || '-'} readonly /></label>
            {#if current.type === 'node' || current.type === 'python'}
              <label class="form-control w-full"><div class="label"><span class="label-text">可执行文件</span></div><input class="input input-bordered w-full input-sm" value="{current.type}/{current.version}/{current.type}.exe" readonly /></label>
            {/if}
            {#if current.running || current.status === 'running'}
              <label class="form-control w-full"><div class="label"><span class="label-text">PID</span></div><input class="input input-bordered w-full input-sm" value={current.pid || '-'} readonly /></label>
              <label class="form-control w-full"><div class="label"><span class="label-text">运行时长</span></div><input class="input input-bordered w-full input-sm" value={current.uptime || '-'} readonly /></label>
            {/if}
          </div>
        </div>
      {/if}
    {:else if !loading}
      <div class="flex flex-col items-center justify-center h-full text-base-content/60">
        <div class="text-4xl opacity-50 mb-3">⚙</div>
        <p>未找到运行环境</p>
        <p class="text-xs mt-1 text-base-content/50">运行环境包括 PHP、Node.js、Python 等</p>
        <p class="text-xs text-base-content/50">请前往「软件包」页面安装后，即可在此管理配置和启停服务</p>
      </div>
    {/if}
  </div>
</div>

