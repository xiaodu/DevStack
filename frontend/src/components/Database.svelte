<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let services = []
let selectedId = null
let loading = true
let mysqlRootHasPassword = null
let checkingAuth = false
let mysqlConfig = {}
let configLoading = false

const memoryOptions = ['64M', '128M', '256M', '512M', '1G', '2G', '4G']
const innodbLogOptions = ['32M', '64M', '128M', '256M', '512M']
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
}

async function load() {
  try {
    const all = await api.getServices()
    services = all.filter((s) => s.category === 'database' && s.installed)
    if (!selectedId && services.length > 0) selectDb(services[0].id)
    await checkMysqlAuth()
  } catch (e) {
    console.error(e)
  } finally {
    loading = false
  }
}

async function checkMysqlAuth() {
  const mysql = services.find((s) => s.id === 'mysql')
  if (mysql?.status !== 'running') {
    mysqlRootHasPassword = null
    return
  }
  checkingAuth = true
  try {
    mysqlRootHasPassword = await api.checkMySQLRootAuth()
  } catch (e) {
    mysqlRootHasPassword = null
  } finally {
    checkingAuth = false
  }
}

function selectDb(id) {
  selectedId = id
  if (id === 'mysql') {
    checkMysqlAuth()
    loadMySQLConfig()
  }
}

async function loadMySQLConfig() {
  configLoading = true
  try {
    mysqlConfig = await api.getServiceConfig('mysql')
  } catch (e) {
    console.error(e)
  } finally {
    configLoading = false
  }
}

function intVal(key) {
  return parseInt(mysqlConfig[key], 10) || 0
}
function getRecommendedHint(key, label) {
  const v = recommendedMySQLConfig[key]
  return v ? `推荐值: ${v}${label ? ` · ${label}` : ''}` : ''
}
function loadRecommendedMySQL() {
  mysqlConfig = { ...recommendedMySQLConfig }
  addToast('已加载推荐配置')
}

async function saveConfig(svc) {
  const vals = {}
  if (svc.id === 'mysql') {
    Object.assign(vals, {
      innodbBufferPoolSize: mysqlConfig.innodbBufferPoolSize || '256M',
      innodbLogFileSize: mysqlConfig.innodbLogFileSize || '64M',
      innodbFlushLogAtTrxCommit: String(intVal('innodbFlushLogAtTrxCommit')),
      maxConnections: String(intVal('maxConnections')),
      tmpTableSize: mysqlConfig.tmpTableSize || '64M',
      maxHeapTableSize: mysqlConfig.maxHeapTableSize || '64M',
      threadCacheSize: String(intVal('threadCacheSize')),
      tableOpenCache: String(intVal('tableOpenCache')),
      bindAddress: mysqlConfig.bindAddress || '127.0.0.1',
      port: String(intVal('port')),
    })
    const pw = document.getElementById('mysql-password')
    if (pw?.value) vals.password = pw.value
  } else if (svc.id === 'redis') {
    const bindIp = document.getElementById('redis-bind')
    const port = document.getElementById('redis-port')
    if (bindIp) vals.bind = bindIp.value
    if (port) vals.port = port.value
  }
  await safeInvoke(
    async () => {
      await api.saveServiceConfig(svc.id, vals)
      if (svc.id === 'mysql') {
        await checkMysqlAuth()
        await loadMySQLConfig()
      }
    },
    { successMsg: '保存成功', errorPrefix: '保存失败' },
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
    <div class="p-4 border-b border-base-300"><h3 class="text-base font-semibold">数据库</h3></div>
    <div class="flex-1 p-2 overflow-y-auto">
      {#each services as svc}
        <button class="flex items-center gap-2.5 w-full px-3 py-2.5 rounded-lg text-left transition-colors border-none {selectedId === svc.id ? 'bg-primary/10' : ''}"
          on:click={() => selectDb(svc.id)}>
          <span class="text-lg">{svc.id === 'mysql' ? '🐬' : '🔴'}</span>
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
        {#if current.port}<span class="ml-auto text-xs px-2 py-0.5 rounded bg-primary/10 text-primary">端口 {current.port}</span>{/if}
      </div>

      {#if current.id === 'mysql'}
        {#if configLoading}
          <div class="text-center py-10 text-base-content/60">加载配置...</div>
        {:else}
          <div class="card bg-base-100 border border-base-300 p-5 mb-4">
            <div class="flex items-center gap-2 px-3 py-2 bg-base-200 rounded-lg">
              {#if current.status !== 'running'}
                <span class="text-base-content/50">MySQL 未运行</span>
              {:else if checkingAuth}
                <span class="text-base-content/50">检查中...</span>
              {:else if mysqlRootHasPassword === true}
                <span class="text-success">root 已设置密码</span>
              {:else if mysqlRootHasPassword === false}
                <span class="text-warning">root 无密码（初始状态）</span>
              {:else}
                <span class="text-base-content/50">无法检测</span>
              {/if}
            </div>
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
                  <input class="input input-bordered w-full input-sm" bind:value={mysqlConfig.bindAddress} />
                  <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('bindAddress', '仅限本地')}</span></div>
                </label>
                <label class="form-control w-full">
                  <div class="label"><span class="label-text">端口</span></div>
                  <input class="input input-bordered w-full input-sm" type="number" min="1" max="65535" value={intVal('port')} on:change={e => mysqlConfig.port = String(e.target.value)} />
                  <div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('port')}</span></div>
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
                <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 缓冲池大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mysqlConfig.innodbBufferPoolSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbBufferPoolSize', '建议内存的50-70%')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 日志文件大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mysqlConfig.innodbLogFileSize}>{#each innodbLogOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbLogFileSize')}</span></div></label>
              </div>
              <div class="grid grid-cols-1 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">InnoDB 刷新日志策略</span></div><select class="select select-bordered w-full select-sm" bind:value={mysqlConfig.innodbFlushLogAtTrxCommit}><option value="1">1 - 每次提交刷新（最安全）</option><option value="2">2 - 每秒刷新（平衡）</option><option value="0">0 - 不主动刷新（最快）</option></select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('innodbFlushLogAtTrxCommit', '2为推荐值')}</span></div></label>
              </div>
            </div>
          </div>

          <div class="card bg-base-100 border border-base-300 p-5 mb-4">
            <h3 class="text-sm font-semibold text-primary mb-3.5">连接与缓存</h3>
            <div class="flex flex-col gap-3 max-w-[700px]">
              <div class="grid grid-cols-2 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">最大连接数</span></div><input class="input input-bordered w-full input-sm" type="number" min="10" max="1000" value={intVal('maxConnections')} on:change={e => mysqlConfig.maxConnections = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxConnections')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">临时表大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mysqlConfig.tmpTableSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('tmpTableSize')}</span></div></label>
              </div>
              <div class="grid grid-cols-2 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">最大堆表大小</span></div><select class="select select-bordered w-full select-sm" bind:value={mysqlConfig.maxHeapTableSize}>{#each memoryOptions as opt}<option>{opt}</option>{/each}</select><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('maxHeapTableSize', '与tmp一致')}</span></div></label>
                <label class="form-control w-full"><div class="label"><span class="label-text">线程缓存大小</span></div><input class="input input-bordered w-full input-sm" type="number" min="1" max="100" value={intVal('threadCacheSize')} on:change={e => mysqlConfig.threadCacheSize = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('threadCacheSize')}</span></div></label>
              </div>
              <div class="grid grid-cols-1 gap-3">
                <label class="form-control w-full"><div class="label"><span class="label-text">表打开缓存</span></div><input class="input input-bordered w-full input-sm" type="number" min="100" max="4000" value={intVal('tableOpenCache')} on:change={e => mysqlConfig.tableOpenCache = String(e.target.value)} /><div class="label"><span class="label-text-alt text-base-content/50">{getRecommendedHint('tableOpenCache')}</span></div></label>
              </div>
            </div>
          </div>

          <div class="card bg-base-100 border border-base-300 p-5 mb-4">
            <h3 class="text-sm font-semibold text-primary mb-3.5">文件信息</h3>
            <div class="grid grid-cols-2 gap-3 max-w-[700px]">
              <label class="form-control w-full"><div class="label"><span class="label-text">数据目录</span></div><input class="input input-bordered w-full input-sm" value="db/mysql/" readonly /></label>
              <label class="form-control w-full"><div class="label"><span class="label-text">配置文件</span></div><input class="input input-bordered w-full input-sm" value="mysql/{current.version}/my.ini" readonly /></label>
            </div>
          </div>
        {/if}
      {:else if current.id === 'redis'}
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
      {/if}

      <div class="flex items-center gap-3 mt-6 pt-4 border-t border-base-300">
        <button class="btn btn-primary" on:click={() => saveConfig(current)}>保存设置</button>
        <span class="text-xs text-base-content/50">MySQL 配置修改后需重启生效</span>
      </div>
    {:else}
      <div class="flex flex-col items-center justify-center h-full text-base-content/60">
        <div class="text-4xl opacity-50 mb-3">🗄</div>
        <p>选择左侧数据库进行配置</p>
      </div>
    {/if}
  </div>
</div>
