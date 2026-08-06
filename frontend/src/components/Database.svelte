<script lang="ts">
import { createEventDispatcher, onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'

export let service
const dispatch = createEventDispatcher()

let mc = {}
let configLoading = false
let mysqlRootHasPassword = null
let checkingAuth = false
let authError = ''
let lastStatus = null

const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
const innodbLogOptions = ['32M', '64M', '128M', '256M', '512M']
const sizeOptions = ['2M', '4M', '8M', '16M', '32M', '64M', '128M', '256M', '512M', '1G']
const keyBufferOptions = ['8M', '16M', '32M', '64M', '128M', '256M', '512M']
const charsetOptions = ['utf8mb4', 'utf8', 'gbk', 'latin1']
const collationOptions = ['utf8mb4_unicode_ci', 'utf8mb4_general_ci', 'utf8_general_ci', 'gbk_chinese_ci']
const sqlModeOptions = [
  { value: 'STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION', label: '严格模式（推荐，兼容主流框架）' },
  { value: 'STRICT_TRANS_TABLES', label: '严格事务表模式' },
  { value: '', label: '宽松模式（兼容老旧程序）' },
]
const recommendedMySQLConfig = {
  innodbBufferPoolSize: '256M',
  innodbLogFileSize: '64M',
  innodbFlushLogAtTrxCommit: '2',
  maxConnections: '50',
  tmpTableSize: '64M',
  maxHeapTableSize: '64M',
  threadCacheSize: '8',
  tableOpenCache: '400',
  bindAddress: '127.0.0.1',
  port: '3306',
  characterSet: 'utf8mb4',
  collation: 'utf8mb4_unicode_ci',
  sqlMode: 'STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION',
  skipNameResolve: 'false',
  generalLog: 'false',
  slowQueryLog: 'false',
  longQueryTime: '2',
  maxAllowedPacket: '16M',
  keyBufferSize: '64M',
  waitTimeout: '28800',
  sortBufferSize: '2M',
  joinBufferSize: '2M',
}

const iconClass = { mysql: 'icon-mysql', redis: 'icon-redis' }[service.kind] || ''

onMount(() => {
  if (service.kind === 'mysql') loadMysqlConfig()
})

// 以 service.status 变化驱动检测（含首次挂载），不再依赖挂载时快照的过期状态
$: if (service.kind === 'mysql' && service.status !== lastStatus) {
  lastStatus = service.status
  checkMysqlAuth()
}

async function checkMysqlAuth() {
  checkingAuth = true
  authError = ''
  try {
    mysqlRootHasPassword = await api.checkMySQLRootAuth()
  } catch (e) {
    mysqlRootHasPassword = null
    authError = e?.message || String(e)
  } finally {
    checkingAuth = false
  }
}

async function loadMysqlConfig() {
  configLoading = true
  try {
    mc = await api.getServiceConfig('mysql')
  } catch (e) {
    console.error(e)
  } finally {
    configLoading = false
  }
}

function intVal(key) {
  return parseInt(mc[key], 10) || 0
}
function boolVal(key) {
  return mc[key] === 'true'
}
function getRecommendedHint(key, label) {
  const v = recommendedMySQLConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommendedMySQL() {
  mc = { ...recommendedMySQLConfig }
}

async function save() {
  const vals = {}
  if (service.kind === 'mysql') {
    Object.assign(vals, {
      innodbBufferPoolSize: mc.innodbBufferPoolSize || '256M',
      innodbLogFileSize: mc.innodbLogFileSize || '64M',
      innodbFlushLogAtTrxCommit: String(intVal('innodbFlushLogAtTrxCommit')),
      maxConnections: String(intVal('maxConnections')),
      tmpTableSize: mc.tmpTableSize || '64M',
      maxHeapTableSize: mc.maxHeapTableSize || '64M',
      threadCacheSize: String(intVal('threadCacheSize')),
      tableOpenCache: String(intVal('tableOpenCache')),
      bindAddress: mc.bindAddress || '127.0.0.1',
      port: String(intVal('port')),
      characterSet: mc.characterSet || 'utf8mb4',
      collation: mc.collation || 'utf8mb4_unicode_ci',
      sqlMode: mc.sqlMode ?? 'STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION',
      skipNameResolve: String(boolVal('skipNameResolve')),
      generalLog: String(boolVal('generalLog')),
      slowQueryLog: String(boolVal('slowQueryLog')),
      longQueryTime: String(intVal('longQueryTime')),
      maxAllowedPacket: mc.maxAllowedPacket || '16M',
      keyBufferSize: mc.keyBufferSize || '64M',
      waitTimeout: String(intVal('waitTimeout')),
      sortBufferSize: mc.sortBufferSize || '2M',
      joinBufferSize: mc.joinBufferSize || '2M',
    })
    const pw = document.getElementById('mysql-password')
    if (pw?.value) vals.password = pw.value
  } else if (service.kind === 'redis') {
    const bindIp = document.getElementById('redis-bind')
    const port = document.getElementById('redis-port')
    if (bindIp) vals.bind = bindIp.value
    if (port) vals.port = port.value
  }
  await safeInvoke(
    async () => {
      await api.saveServiceConfig(service.id, vals)
      if (service.kind === 'mysql') {
        await checkMysqlAuth()
        await loadMysqlConfig()
      }
      dispatch('change')
    },
    { successMsg: '保存成功', errorPrefix: '保存失败' },
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
    <button class="btn btn-sm {service.running ? 'btn-error' : 'btn-success'}" on:click={toggle}>
      {service.running ? '停止' : '启动'}
    </button>
  </div>

  <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg mb-6 text-sm">
    <span class="w-2 h-2 rounded-full {service.running ? 'bg-success' : 'bg-base-content/40'}"></span>
    <span class="text-base-content/70">{service.running ? '运行中' : '已停止'}</span>
    {#if service.running && service.uptime}<span class="text-base-content/60">运行 {service.uptime}</span>{/if}
    {#if service.port}<span class="ml-auto text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">端口 {service.port}</span>{/if}
  </div>

  {#if service.kind === 'mysql'}
    {#if configLoading}
      <div class="text-center py-10 text-base-content/60">加载配置...</div>
    {:else}
      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <div class="flex items-center gap-2 px-3.5 py-2.5 bg-base-200 rounded-lg">
          <span class="w-2 h-2 rounded-full {service.status === 'running' ? 'bg-success' : 'bg-error'}"></span>
          <span class="text-base-content/80">{service.status === 'running' ? 'MySQL 运行中' : 'MySQL 未运行'}</span>
          <span class="mx-2 text-base-content/30">|</span>
          {#if checkingAuth}
            <span class="flex items-center gap-1.5 text-base-content/50">检查 root 密码...<span class="loading loading-spinner loading-xs"></span></span>
          {:else if authError}
            <span class="text-warning">{authError}</span>
          {:else if mysqlRootHasPassword === true}
            <span class="text-success">root 已设置密码</span>
          {:else if mysqlRootHasPassword === false}
            <span class="text-warning">root 无密码（初始状态）</span>
          {:else}
            <span class="text-base-content/50">未检测</span>
          {/if}
        </div>
        {#if service.status !== 'running'}
          <p class="text-xs text-base-content/50 mt-3">
            修改 root 密码必须在 MySQL 运行状态下进行；未运行时可查看 / 编辑配置并保存，但不修改密码。
          </p>
        {/if}
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">连接设置</h3>
        <div class="flex flex-col gap-3 max-w-[700px]">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">账号</span></div><input class="input input-bordered w-full input-sm" value="root" readonly /></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">密码</span></div><input id="mysql-password" class="input input-bordered w-full input-sm" type="password" placeholder="留空则不修改" /></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full">
              <div class="label"><span class="label-text">绑定 IP</span></div>
              <input class="input input-bordered w-full input-sm" bind:value={mc.bindAddress} />
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('bindAddress', '仅限本地')}</span></div>
            </label>
            <label class="form-control w-full">
              <div class="label"><span class="label-text">端口</span></div>
              <input class="input input-bordered w-full input-sm" type="number" min="1" max="65535" value={intVal('port')} on:change={e => mc.port = String(e.target.value)} />
              <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('port')}</span></div>
            </label>
          </div>
          <p class="text-xs text-base-content/50">在「密码」输入新密码并保存，即可修改 MySQL root 密码（需 MySQL 正在运行）。</p>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">字符集与兼容性</h3>
        <div class="flex flex-col gap-3 max-w-[700px]">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">字符集</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.characterSet}>{#each charsetOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('characterSet', '建议 utf8mb4')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">排序规则</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.collation}>{#each collationOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('collation', '需与字符集匹配')}</span></div></label>
          </div>
          <div class="grid grid-cols-1 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">SQL 模式</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.sqlMode}>{#each sqlModeOptions as opt}<option value={opt.value}>{opt.label}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('sqlMode')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('skipNameResolve')} on:change={e => mc.skipNameResolve = String(e.target.checked)} />
              <div><span class="text-sm">跳过域名解析</span><p class="text-xs text-base-content/50">跳过客户端主机名反查，仅本机访问推荐开启</p></div>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('generalLog')} on:change={e => mc.generalLog = String(e.target.checked)} />
              <div><span class="text-sm">全量查询日志</span><p class="text-xs text-base-content/50">记录所有 SQL，调试时开启，平时建议关闭</p></div>
            </label>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <div class="flex justify-between items-center mb-3.5">
          <h3 class="text-sm font-semibold text-primary">InnoDB 优化</h3>
          <button class="btn btn-sm btn-outline" on:click={loadRecommendedMySQL}>加载推荐值</button>
        </div>
        <div class="flex flex-col gap-3 max-w-[700px]">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 缓冲池大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.innodbBufferPoolSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbBufferPoolSize', '建议内存的50-70%')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 日志文件大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.innodbLogFileSize}>{#each innodbLogOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbLogFileSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-1 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 刷新日志策略</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.innodbFlushLogAtTrxCommit}><option value="1">1 - 每次提交刷新（最安全）</option><option value="2">2 - 每秒刷新（平衡）</option><option value="0">0 - 不主动刷新（最快）</option></select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbFlushLogAtTrxCommit', '2为推荐值')}</span></div></label>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">连接与缓存</h3>
        <div class="flex flex-col gap-3 max-w-[700px]">
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">最大连接数</span></div><input class="input input-bordered w-full input-sm" type="number" min="10" max="1000" value={intVal('maxConnections')} on:change={e => mc.maxConnections = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxConnections')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">临时表大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.tmpTableSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('tmpTableSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">最大堆表大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.maxHeapTableSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxHeapTableSize', '与tmp一致')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">线程缓存大小</span></div><input class="input input-bordered w-full input-sm" type="number" min="1" max="100" value={intVal('threadCacheSize')} on:change={e => mc.threadCacheSize = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('threadCacheSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-1 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">表打开缓存</span></div><input class="input input-bordered w-full input-sm" type="number" min="100" max="4000" value={intVal('tableOpenCache')} on:change={e => mc.tableOpenCache = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('tableOpenCache')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">最大数据包</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.maxAllowedPacket}>{#each sizeOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxAllowedPacket', '大字段/包提交相关')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">MyISAM 键缓存</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.keyBufferSize}>{#each keyBufferOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('keyBufferSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">排序缓冲区</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.sortBufferSize}>{#each sizeOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('sortBufferSize')}</span></div></label>
            <label class="form-control w-full"><div class="label"><span class="label-text">连接缓冲区</span></div><select class="select select-bordered w-full select-sm" bind:value={mc.joinBufferSize}>{#each sizeOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('joinBufferSize')}</span></div></label>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="form-control w-full"><div class="label"><span class="label-text">超时等待(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="10" max="86400" value={intVal('waitTimeout')} on:change={e => mc.waitTimeout = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('waitTimeout', '空闲连接超时')}</span></div></label>
            <div class="flex items-end gap-3 pb-1">
              <label class="flex items-center gap-2 cursor-pointer">
                <input type="checkbox" class="toggle toggle-sm toggle-primary" checked={boolVal('slowQueryLog')} on:change={e => mc.slowQueryLog = String(e.target.checked)} />
                <div><span class="text-sm">慢查询日志</span></div>
              </label>
              <label class="form-control flex-1"><div class="label"><span class="label-text">慢查询阈值(秒)</span></div><input class="input input-bordered w-full input-sm" type="number" min="1" max="60" value={intVal('longQueryTime')} on:change={e => mc.longQueryTime = String(e.target.value)} /></label>
            </div>
          </div>
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300 p-5 mb-4">
        <h3 class="text-sm font-semibold text-primary mb-3.5">文件信息</h3>
        <div class="grid grid-cols-2 gap-3 max-w-[700px]">
          <label class="form-control w-full"><div class="label"><span class="label-text">数据目录</span></div><input class="input input-bordered w-full input-sm" value="db/mysql/" readonly /></label>
          <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="mysql/{service.version}/my.ini" readonly /></label>
        </div>
      </div>
    {/if}
  {:else if service.kind === 'redis'}
    <div class="card bg-base-100 border border-base-300 p-5 mb-4">
      <h3 class="text-sm font-semibold text-primary mb-3.5">连接设置</h3>
      <div class="grid grid-cols-2 gap-3 max-w-[700px]">
        <label class="form-control w-full"><div class="label"><span class="label-text">绑定 IP</span></div><input id="redis-bind" class="input input-bordered w-full input-sm" value="127.0.0.1" /></label>
        <label class="form-control w-full"><div class="label"><span class="label-text">端口</span></div><input id="redis-port" class="input input-bordered w-full input-sm" type="number" value="6379" /></label>
      </div>
    </div>
    <div class="card bg-base-100 border border-base-300 p-5 mb-4">
      <h3 class="text-sm font-semibold text-primary mb-3.5">文件信息</h3>
      <div class="grid grid-cols-2 gap-3 max-w-[700px]">
        <label class="form-control w-full"><div class="label"><span class="label-text">数据目录</span></div><input class="input input-bordered w-full input-sm" value="data/redis" readonly /></label>
      </div>
    </div>
  {:else}
    <div class="card bg-base-100 border border-base-300 p-5">
      <h3 class="text-sm font-semibold text-primary mb-3.5">{service.name} 信息</h3>
      <div class="grid grid-cols-2 gap-3 max-w-[700px]">
        <label class="form-control w-full"><div class="label"><span class="label-text">版本</span></div><input class="input input-bordered w-full input-sm" value={service.version} readonly /></label>
      </div>
      <p class="text-xs text-base-content/50 mt-3">该数据库暂无可配置项，请升级版本或选择 MySQL / Redis</p>
    </div>
  {/if}

  {#if service.kind === 'mysql' || service.kind === 'redis'}
    <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
      <button class="btn btn-primary" on:click={save}>保存设置</button>
      <span class="text-xs text-base-content/50">{service.kind === 'mysql' ? '配置修改与密码修改后需重启生效' : 'Redis 配置修改后需重启生效'}</span>
    </div>
  {/if}
</div>