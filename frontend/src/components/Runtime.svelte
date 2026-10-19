<script lang="ts">
import { createEventDispatcher, onMount } from 'svelte'
import type { runtime } from '../../wailsjs/go/models'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { nanoBusy } from '../lib/stores'

let { service, installDir = '' } = $props()
const dispatch = createEventDispatcher()

// 显式去重 key：匿名函数走 safeInvoke 的 key 生成逻辑会得到每次不同的 'anon-N'，
// 去重永远不命中，双击即并发写 php.ini。这里与 WebServer/Database 保持一致。
// 用 $derived 读取 prop，避免 state_referenced_locally（key 只取一次快照）。
const toggleKey = $derived(`runtime:toggle:${service.id}`)
const saveKey = $derived(`runtime:savecfg:${service.id}`)
let toggling = $derived(($nanoBusy[toggleKey] || 0) > 0)
let saving = $derived(($nanoBusy[saveKey] || 0) > 0)

let pc = $state<Record<string, any>>({})
let configLoading = $state(false)
// php.ini 读取失败时模型是 {}，数值字段全渲染成 0，保存即写坏真实配置 —— 禁止保存
let configError = $state('')
let extensions = $state<runtime.PHPExtensionInfo[]>([])
let extLoading = $state(false)
let extSearch = $state('')
let enabledSet = $state(new Set())

const typeMeta = {
  php: { label: 'PHP', color: '#777BB4' },
  node: { label: 'Node.js', color: '#339933' },
  python: { label: 'Python', color: '#3776AB' },
}
const rtIcons = { php: 'icon-php1', node: 'icon-Nodejs', python: 'icon-python' }
const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
const uploadOptions = ['2M', '8M', '16M', '32M', '64M', '128M', '256M', '512M']
const realpathOptions = ['512K', '1024K', '2048K', '4096K', '8192K']
const timezoneOptions = ['Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Tokyo', 'UTC', 'America/New_York', 'Europe/London']
const charsetOptions = ['UTF-8', 'UTF-8MB4', 'GBK']
const recommendedPHPConfig = {
  memoryLimit: '256M',
  maxExecutionTime: '300',
  uploadMaxFilesize: '64M',
  postMaxSize: '64M',
  displayErrors: 'false',
  opcacheEnabled: 'true',
  realpathCacheSize: '4096K',
  maxInputTime: '60',
  maxInputVars: '3000',
  maxFileUploads: '20',
  timezone: 'Asia/Shanghai',
  defaultCharset: 'UTF-8',
  fileUploads: 'true',
  sessionGCMaxLifetime: '7200',
  logErrors: 'true',
  errorLog: 'php_errors.log',
  fixPathinfo: 'true',
}

let meta = $derived(typeMeta[service.type] || { label: service.name, color: 'var(--color-accent)' })
let iconClass = $derived(rtIcons[service.type] || '')

onMount(() => {
  if (service.type === 'php') {
    loadPHPConfig()
    loadExtensions()
  }
})

async function loadPHPConfig() {
  configLoading = true
  configError = ''
  try {
    const cfg = await api.getServiceConfig('php')
    if (!cfg || typeof cfg !== 'object') {
      pc = {}
      configError = '读取到的配置为空'
    } else {
      pc = cfg
    }
  } catch (e) {
    console.error(e)
    pc = {}
    configError = (e as any)?.message || String(e)
  } finally {
    configLoading = false
  }
}

async function loadExtensions() {
  extLoading = true
  try {
    extensions = (await api.getPHPExtensions(service.version)) || []
    enabledSet = new Set(extensions.filter((e) => e.enabled).map((e) => e.name))
  } catch (e) {
    console.error(e)
  } finally {
    extLoading = false
  }
}

let filteredExts = $derived(
  extSearch ? extensions.filter((e) => e.name.toLowerCase().includes(extSearch.toLowerCase())) : extensions,
)

function toggleExt(name) {
  const s = new Set(enabledSet)
  if (s.has(name)) s.delete(name)
  else s.add(name)
  enabledSet = s
}

function selectAllExts() {
  enabledSet = new Set(extensions.map((e) => e.name))
}

function selectNoneExts() {
  enabledSet = new Set()
}

function intVal(key) {
  return parseInt(pc[key], 10) || 0
}
function boolVal(key) {
  return pc[key] === 'true'
}
function getRecommendedHint(key, label) {
  const v = recommendedPHPConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommendedPHP() {
  pc = { ...recommendedPHPConfig }
}

async function save() {
  if (configError) return // 配置未成功读取前不允许写盘（按钮已禁用，这里兜底）
  await safeInvoke(
    saveKey,
    async () => {
      await api.saveServiceConfig(service.id, {
        memoryLimit: pc.memoryLimit || '256M',
        maxExecutionTime: String(intVal('maxExecutionTime')),
        uploadMaxFilesize: pc.uploadMaxFilesize || '64M',
        postMaxSize: pc.postMaxSize || '64M',
        displayErrors: String(boolVal('displayErrors')),
        opcacheEnabled: String(boolVal('opcacheEnabled')),
        realpathCacheSize: pc.realpathCacheSize || '4096K',
        maxInputTime: String(intVal('maxInputTime')),
        maxInputVars: String(intVal('maxInputVars')),
        maxFileUploads: String(intVal('maxFileUploads')),
        timezone: pc.timezone || 'Asia/Shanghai',
        defaultCharset: pc.defaultCharset || 'UTF-8',
        fileUploads: String(boolVal('fileUploads')),
        sessionGCMaxLifetime: String(intVal('sessionGCMaxLifetime')),
        logErrors: String(boolVal('logErrors')),
        errorLog: pc.errorLog || 'php_errors.log',
        fixPathinfo: String(boolVal('fixPathinfo')),
      })
      await api.savePHPExtensions(
        service.version,
        extensions.filter((e) => enabledSet.has(e.name)).map((e) => e.name),
      )
      dispatch('change')
    },
    { successMsg: 'PHP 配置已保存，重启后生效', errorPrefix: '保存失败' },
  )
  loadExtensions()
}

async function toggle() {
  await safeInvoke(
    toggleKey,
    async () => {
      if (service.running) await api.stopRuntime(service.id)
      else await api.startRuntime(service.id)
      dispatch('change')
    },
    { successMsg: `${service.name} 状态已切换`, errorPrefix: '操作失败' },
  )
}
</script>

<div class="p-6">
  <div class="flex justify-between items-center mb-4">
    <h2 class="text-xl font-semibold flex items-center gap-2.5">
      {#if iconClass}
        <span class="iconfont text-2xl leading-none {iconClass}" style="color:{meta.color}"></span>
      {:else}
        <span class="text-2xl" style="color:{meta.color}">⚙</span>
      {/if}
      {service.name}
      <span class="text-base font-normal text-base-content/50">{service.version}</span>
    </h2>
  </div>

  <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg mb-6 text-sm">
    <span class="w-2 h-2 rounded-full {service.running ? 'bg-success' : 'bg-base-content/40'}"></span>
    <span class="text-base-content/70">{service.running ? '运行中' : '已停止'}</span>
    {#if service.active}<span class="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">命令行默认</span>{/if}
    {#if service.port}<span class="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">端口 {service.port}</span>{/if}
    {#if service.running && service.uptime}<span class="text-base-content/60">运行 {service.uptime}</span>{/if}
    <button class="btn btn-xs ml-auto {service.running ? 'btn-error' : 'btn-success'}" onclick={toggle} disabled={toggling}>
      {#if toggling}<span class="loading loading-spinner loading-xs"></span>{/if}
      {service.running ? '停止' : '启动'}
    </button>
  </div>

  {#if service.type === 'php'}
    {#if configLoading}
      <div class="text-center py-10 text-base-content/60">加载配置...</div>
    {:else}
      {#if configError}
        <div class="alert alert-error alert-sm flex items-center gap-3 py-2.5 mb-4">
          <span class="text-sm">php.ini 读取失败（{configError}），为避免用空值覆盖配置，已禁止保存。</span>
          <button class="btn btn-sm btn-ghost shrink-0" onclick={loadPHPConfig} disabled={configLoading}>重新加载</button>
        </div>
      {/if}
      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <div class="flex justify-between items-center mb-3.5">
          <h3 class="text-sm font-semibold text-primary">PHP 配置</h3>
          <button class="btn btn-sm btn-outline" onclick={loadRecommendedPHP}>加载推荐值</button>
        </div>
        <div class="flex flex-col gap-3">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">内存限制</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.memoryLimit}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('memoryLimit')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">最大执行时间(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="30" max="600" value={intVal('maxExecutionTime')} onchange={e => pc.maxExecutionTime = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxExecutionTime')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">上传文件最大大小</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.uploadMaxFilesize}>{#each uploadOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('uploadMaxFilesize')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">POST 数据最大大小</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.postMaxSize}>{#each uploadOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('postMaxSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">Realpath 缓存大小</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.realpathCacheSize}>{#each realpathOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('realpathCacheSize', 'Windows 性能关键')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="{installDir}/php/{service.version}/php.ini" readonly /></label>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">执行环境与上传</h3>
        <div class="flex flex-col gap-3 max-w-[700px]">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">输入超时(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="10" max="600" value={intVal('maxInputTime')} onchange={e => pc.maxInputTime = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxInputTime')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">输入变量上限</span></div><input class="input input-bordered w-full input-sm" type="number" min="1000" max="10000" value={intVal('maxInputVars')} onchange={e => pc.maxInputVars = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxInputVars', '表单字段过多时可调大')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">同时上传文件数</span></div><input class="input input-bordered w-full input-sm" type="number" min="1" max="100" value={intVal('maxFileUploads')} onchange={e => pc.maxFileUploads = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxFileUploads')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">Session 有效期(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="60" max="86400" value={intVal('sessionGCMaxLifetime')} onchange={e => pc.sessionGCMaxLifetime = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('sessionGCMaxLifetime', '登录态保持时间')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">时区</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.timezone}>{#each timezoneOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('timezone', '影响 date() 输出')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">默认字符集</span></div><select class="select select-bordered w-full select-sm" bind:value={pc.defaultCharset}>{#each charsetOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('defaultCharset')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">错误日志文件</span></div><input class="input input-bordered w-full input-sm" value={pc.errorLog || 'php_errors.log'} onchange={e => pc.errorLog = e.target.value} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('errorLog')}</span></div></label>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">功能开关</h3>
        <div class="grid grid-cols-2 gap-3">
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('displayErrors')} onchange={e => pc.displayErrors = String(e.target.checked)} />
            <div><span class="text-sm">显示错误信息</span><p class="text-xs text-base-content/50">{getRecommendedHint('displayErrors', '生产环境建议关闭')}</p></div>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('opcacheEnabled')} onchange={e => pc.opcacheEnabled = String(e.target.checked)} />
            <div><span class="text-sm">启用 OPcache</span><p class="text-xs text-base-content/50">{getRecommendedHint('opcacheEnabled', '性能提升 30-50%')}</p></div>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('fileUploads')} onchange={e => pc.fileUploads = String(e.target.checked)} />
            <div><span class="text-sm">允许文件上传</span><p class="text-xs text-base-content/50">file_uploads，上传大小由上方限制</p></div>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('logErrors')} onchange={e => pc.logErrors = String(e.target.checked)} />
            <div><span class="text-sm">记录错误日志</span><p class="text-xs text-base-content/50">写入 error_log 文件</p></div>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('fixPathinfo')} onchange={e => pc.fixPathinfo = String(e.target.checked)} />
            <div><span class="text-sm">启用 pathinfo 支持</span><p class="text-xs text-base-content/50">cgi.fix_pathinfo，ThinkPHP 等框架必需</p></div>
          </label>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">PHP 扩展</h3>
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-2">
            <input class="input input-bordered w-full input-sm" type="search" placeholder="搜索扩展..." bind:value={extSearch} />
            <span class="text-xs text-base-content/50 whitespace-nowrap">已启用 {enabledSet.size}/{extensions.length}</span>
            <button class="btn btn-sm btn-outline" onclick={selectAllExts}>全部启用</button>
            <button class="btn btn-sm btn-outline" onclick={selectNoneExts}>全部禁用</button>
          </div>
          {#if extLoading}
            <div class="text-center py-6 text-base-content/60">加载扩展中...</div>
          {:else if filteredExts.length === 0}
            <div class="text-center py-6 text-base-content/60">未找到扩展{extSearch ? '（试试调整搜索关键词）' : ''}</div>
          {:else}
            <div class="grid grid-cols-2 md:grid-cols-3 gap-x-3 gap-y-1.5 max-h-56 overflow-y-auto pr-1">
              {#each filteredExts as ext}
                <label class="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" class="toggle toggle-xs toggle-primary" checked={enabledSet.has(ext.name)} onchange={() => toggleExt(ext.name)} />
                  <span class="text-sm text-base-content/80 truncate">{ext.name}</span>
                </label>
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
        <button class="btn btn-primary" onclick={save} disabled={saving || !!configError}>
          {#if saving}<span class="loading loading-spinner loading-xs"></span>{/if}保存配置
        </button>
        <span class="text-xs {configError ? 'text-error' : 'text-base-content/50'}">
          {configError ? '配置未成功读取，无法保存（请先重新加载）' : '保存后重新生成 php.ini，需重启 PHP 生效'}
        </span>
      </div>
    {/if}
  {:else}
    <div class="card bg-base-100 border border-base-300 p-5 mb-4">
      <h3 class="text-sm font-semibold text-primary mb-3.5">{meta.label} 信息</h3>
      <div class="grid grid-cols-2 gap-3">
        <label class="form-control w-full"><div class="label"><span class="label-text">类型</span></div><input class="input input-bordered w-full input-sm" value={meta.label || service.type} readonly /></label>
        <label class="form-control w-full"><div class="label"><span class="label-text">版本</span></div><input class="input input-bordered w-full input-sm" value={service.version} readonly /></label>
        <label class="form-control w-full"><div class="label"><span class="label-text">安装路径</span></div><input class="input input-bordered w-full input-sm" value={service.path || '-'} readonly /></label>
        {#if service.type === 'node' || service.type === 'python'}
          <label class="form-control w-full"><div class="label"><span class="label-text">可执行文件</span></div><input class="input input-bordered w-full input-sm" value="{installDir}/{service.type}/{service.version}/{service.type}.exe" readonly /></label>
        {/if}
        {#if service.running}
          <label class="form-control w-full"><div class="label"><span class="label-text">PID</span></div><input class="input input-bordered w-full input-sm" value={service.pid || '-'} readonly /></label>
          <label class="form-control w-full"><div class="label"><span class="label-text">运行时长</span></div><input class="input input-bordered w-full input-sm" value={service.uptime || '-'} readonly /></label>
        {/if}
      </div>
      <p class="text-xs text-base-content/50 mt-3">
        {service.type === 'python'
          ? 'Python 为解释型运行环境，无需常驻服务。项目中选择该版本后，命令行与站点均使用此处安装的解释器。'
          : 'Node.js 支持以常驻服务方式启动（Stop 后即退出），项目中选择该版本作为运行时。'}
      </p>
    </div>
  {/if}
</div>