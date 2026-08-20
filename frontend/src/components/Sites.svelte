<!-- @migration-task Error while migrating Svelte code: Mixing old (on:click) and new syntaxes for event handling is not allowed. Use only the onclick syntax
https://svelte.dev/e/mixed_event_handler_syntaxes -->
<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast } from '../lib/stores'

let sites = $state<any[]>([])
let selectedId = $state<string | null>(null)
let editing = $state<any>(null)
let loading = $state(true)
let searchQuery = $state('')
let busy = $state(false)
let phpVersions = $state<any[]>([])
let mysqlVersions = $state<any[]>([])
let nginxVersions = $state<any[]>([])
let profiles = $state<any[]>([])
let profilesLoading = $state(false)

async function load() {
  try {
    sites = await api.getProjects()
    await Promise.all([loadPHPVersions(), loadMySQLVersions(), loadNginxVersions(), loadProfiles()])
    if (!selectedId && sites.length > 0) selectSite(sites[0].id)
  } catch (e) {
    console.error(e)
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

async function loadPHPVersions() {
  try {
    const registry = await api.getPHPRegistry()
    if (registry?.length > 0) {
      phpVersions = registry.map((v) => ({ version: v.version, port: v.port }))
      return
    }
  } catch (e) {
    /* fallthrough */
  }
  try {
    const installed = await api.getInstalledVersions('php')
    phpVersions = (installed || []).map((v) => ({ version: v, port: 0 }))
  } catch (e) {
    phpVersions = []
  }
}

async function loadMySQLVersions() {
  try {
    const installed = await api.getInstalledVersions('mysql')
    mysqlVersions = (installed || []).map((v) => ({ version: v }))
  } catch (e) {
    mysqlVersions = []
  }
}

async function loadNginxVersions() {
  try {
    const installed = await api.getInstalledVersions('nginx')
    nginxVersions = (installed || []).map((v) => ({ version: v }))
  } catch (e) {
    nginxVersions = []
  }
}

function selectSite(id) {
  selectedId = id
  const s = sites.find((x) => x.id === id)
  if (s)
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
    addToast('请先保存项目再激活环境', 'warning')
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

// 从环境模板（units: {kind, version}）读取指定组件版本
function profileUnit(kind: string) {
  return selectedProfile?.units?.find((u) => u.kind === kind) || null
}

// 从模板提取版本（只读展示）
let templateNginxVersion = $derived((() => {
  const u = profileUnit('nginx')
  return u ? 'Nginx ' + u.version : '未指定'
})())
let templateMySQLVersion = $derived((() => {
  const u = profileUnit('mysql')
  return u ? 'MySQL ' + u.version : '未指定'
})())
let templatePHPVersion = $derived((() => {
  const u = profileUnit('php')
  return u ? 'PHP ' + u.version : '未指定'
})())
let templateRuntimeBadges = $derived((selectedProfile?.units || [])
  .filter((u) => u.kind === 'php' || u.kind === 'node' || u.kind === 'python')
  .map((u) => ({ runtimeId: u.kind, version: u.version })))

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

async function toggleProject(id) {
  await safeInvoke(
    'toggleProject',
    async () => {
      await api.toggleProject(id)
      await load()
    },
    { errorPrefix: '切换失败' },
  )
}

let filteredSites = $derived(sites.filter(
  (s) =>
    s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    s.domain.toLowerCase().includes(searchQuery.toLowerCase()),
))
let showTargetPort = $derived(editing && (editing.type === 'node' || editing.type === 'python'))
let showProxyPass = $derived(editing && editing.type === 'proxy')
let showPHP = $derived(editing && editing.type === 'php')

async function browseDirectory() {
  const path = await api.openDirectory(editing?.root || '')
  if (path && editing) {
    editing.root = path
  }
}

onMount(load)
</script>

<div class="grid grid-cols-[300px_1fr] h-[calc(100vh-48px)] -m-6 -mx-8 bg-base-100">
  <div class="border-r border-base-300 flex flex-col overflow-hidden">
    <div class="flex justify-between items-center p-4 border-b border-base-300">
      <h3 class="text-base font-semibold">项目</h3>
      <button class="w-7 h-7 rounded-full bg-primary text-primary-content text-base flex items-center justify-center hover:brightness-110 border-none" onclick={newSite} title="添加项目">+</button>
    </div>
    <div class="px-4 py-3 border-b border-base-300">
      <input class="input input-bordered w-full input-sm" placeholder="搜索..." bind:value={searchQuery} />
    </div>
    <div class="flex-1 overflow-y-auto p-2">
      {#each filteredSites as site}
        <div class="flex items-center justify-between w-full px-3 py-2.5 rounded-lg text-left transition-colors cursor-pointer {selectedId === site.id ? 'bg-primary/10' : 'hover:bg-base-200'}"
          role="button" tabindex="0"
          onclick={() => selectSite(site.id)}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              selectSite(site.id)
            }
          }}>
          <div class="flex items-center gap-2.5 min-w-0">
            <span class="w-2 h-2 rounded-full shrink-0 {site.enabled ? 'bg-success' : 'bg-base-content/40'}"></span>
            <div>
              <div class="text-sm font-medium">{site.name}</div>
              <div class="text-xs text-base-content/60 font-mono">{site.domain}</div>
            </div>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-xs px-1 py-0.5 rounded bg-base-200 text-base-content/60 font-mono">{site.type}</span>
            <button class="btn btn-ghost btn-xs btn-square text-base-content/60 hover:text-base-content" onclick={(e) => { e.stopPropagation(); toggleProject(site.id) }} title={site.enabled ? '禁用' : '启用'}>
              {site.enabled ? '⏸' : '▶'}
            </button>
          </div>
        </div>
      {/each}
      {#if filteredSites.length === 0}
        <div class="text-center py-6 text-base-content/60 text-sm">暂无项目</div>
      {/if}
    </div>
  </div>

  <div class="overflow-y-auto p-6">
    {#if editing}
      <div class="flex justify-between items-center mb-6">
        <h2 class="text-xl font-semibold">{editing.id === '__new__' ? '新建项目' : editing.name}</h2>
        <div class="flex gap-2">
          {#if editing.id && editing.id !== '__new__'}
            <button class="btn btn-sm btn-outline" onclick={activateProjectEnvironment} disabled={busy}>{busy ? '处理中...' : '激活此项目环境'}</button>
            <button class="btn btn-sm btn-circle bg-error/10 text-error hover:bg-error/20 border-none" onclick={remove} title="删除">🗑</button>
          {/if}
          <button class="btn btn-sm btn-circle bg-success text-success-content hover:brightness-110 border-none" onclick={save} title="保存">✓</button>
        </div>
      </div>

      <div class="flex flex-col gap-4 max-w-[700px]">
        <label class="form-control w-full"><div class="label"><span class="label-text">名字</span></div><input class="input input-bordered w-full input-sm" bind:value={editing.name} /></label>
        <label class="form-control w-full"><div class="label"><span class="label-text">域名</span></div><input class="input input-bordered w-full input-sm" bind:value={editing.domain} placeholder="example.dev" /></label>

        <div class="grid grid-cols-3 gap-3">
          <label class="form-control"><div class="label"><span class="label-text">HTTP Port</span></div><input class="input input-bordered w-full input-sm" type="number" bind:value={editing.port} /></label>
          <label class="form-control"><div class="label"><span class="label-text">HTTPS Port</span></div><input class="input input-bordered w-full input-sm" type="number" bind:value={editing.httpsPort} placeholder="443" /></label>
          <label class="flex items-center gap-2 cursor-pointer pt-9">
            <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={editing.ssl} />
            <span class="text-sm text-base-content/70">SSL</span>
          </label>
        </div>

        <div class="grid grid-cols-3 gap-3">
          <label class="form-control"><div class="label"><span class="label-text">项目类型</span></div>
            <select class="select select-bordered w-full select-sm" bind:value={editing.type}>
              <option value="static">静态</option><option value="php">PHP</option><option value="node">Node.js</option><option value="python">Python</option><option value="proxy">反向代理</option>
            </select>
          </label>

          {#if showPHP}
            <label class="form-control col-span-2"><div class="label"><span class="label-text">Rewrite 规则</span></div>
              <select class="select select-bordered w-full select-sm" bind:value={editing.rewrite}>
                <option value="default">Default</option><option value="laravel">Laravel</option><option value="thinkphp">ThinkPHP</option>
              </select>
            </label>
          {:else if showTargetPort}
            <label class="form-control"><div class="label"><span class="label-text">应用端口</span></div><input class="input input-bordered w-full input-sm" type="number" bind:value={editing.targetPort} /></label>
            <label class="form-control"><div class="label"><span class="label-text">说明</span></div><input class="input input-bordered w-full input-sm" value="自行启动进程" readonly /></label>
          {:else if showProxyPass}
            <label class="form-control col-span-2"><div class="label"><span class="label-text">目标地址</span></div><input class="input input-bordered w-full input-sm" bind:value={editing.proxyPass} placeholder="http://127.0.0.1:3000" /></label>
          {:else}
            <div></div><div></div>
          {/if}
        </div>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">项目根目录</span></div>
          <div class="flex gap-2">
            <input class="input input-bordered w-full input-sm" bind:value={editing.root} placeholder="E:\projects\my-project" />
            <button class="btn btn-sm btn-outline" onclick={browseDirectory}>浏览</button>
          </div>
        </label>

        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={editing.enabled} />
          <span class="text-sm">启用项目</span>
        </label>

        <!-- 环境配置（合并：关联模板 + 版本配置） -->
        <div class="mt-2 p-4 bg-base-200 rounded-xl border border-base-300">
          <div class="flex justify-between items-center mb-3">
            <h3 class="text-sm font-semibold text-primary">环境配置</h3>
            {#if selectedProfile}
              <span class="badge badge-sm badge-primary">继承自「{selectedProfile.name}」</span>
            {:else}
              <span class="badge badge-sm badge-ghost">独立配置</span>
            {/if}
          </div>

          <!-- 关联环境模板选择 -->
          <label class="form-control w-full mb-3">
            <div class="label"><span class="label-text">关联环境模板</span></div>
            {#if profilesLoading}
              <input class="input input-bordered w-full input-sm" value="加载环境列表中..." readonly />
            {:else}
              <select class="select select-bordered w-full select-sm" bind:value={editing.environmentID}>
                <option value="">（不使用模板，独立配置）</option>
                {#each profiles as profile}<option value={profile.id}>{profile.name}{profile.isBuiltin ? '  [预设]' : ''}</option>{/each}
              </select>
            {/if}
            {#if selectedProfile}
              <div class="label"><span class="label-text-alt text-base-content/60 leading-relaxed">
                {selectedProfile.description}
              </span></div>
            {:else}
              <div class="label"><span class="label-text-alt text-base-content/60">不关联模板时，下方版本独立生效</span></div>
            {/if}
          </label>

          {#if selectedProfile}
            <!-- 关联模板：只读展示从模板继承的版本 -->
            <div class="grid grid-cols-3 gap-3">
              <div class="form-control">
                <div class="label"><span class="label-text">Nginx 版本</span></div>
                <input class="input input-bordered w-full input-sm" value={templateNginxVersion} readonly />
              </div>
              <div class="form-control">
                <div class="label"><span class="label-text">MySQL 版本</span></div>
                <input class="input input-bordered w-full input-sm" value={templateMySQLVersion} readonly />
              </div>
              <div class="form-control">
                <div class="label"><span class="label-text">PHP 版本</span></div>
                <input class="input input-bordered w-full input-sm" value={templatePHPVersion} readonly />
              </div>
            </div>
            {#if templateRuntimeBadges.length > 0}
              <div class="mt-2 text-xs text-base-content/60">
                运行时：
                {#each templateRuntimeBadges as rt}
                  <span class="badge badge-sm badge-ghost mr-1">{rt.runtimeId} {rt.version}</span>
                {/each}
              </div>
            {/if}
          {:else}
            <!-- 独立配置：可选择的版本下拉 -->
            <div class="grid grid-cols-3 gap-3">
              <label class="form-control"><div class="label"><span class="label-text">Nginx 版本</span></div>
                <select class="select select-bordered w-full select-sm" bind:value={editing.nginxVersion}>
                  <option value="">默认</option>
                  {#each nginxVersions as nv}<option value={nv.version}>Nginx {nv.version}</option>{/each}
                </select>
              </label>
              <label class="form-control"><div class="label"><span class="label-text">MySQL 版本</span></div>
                <select class="select select-bordered w-full select-sm" bind:value={editing.mysqlVersion}>
                  <option value="">不使用</option>
                  {#each mysqlVersions as mv}<option value={mv.version}>MySQL {mv.version}</option>{/each}
                </select>
              </label>
              <label class="form-control"><div class="label"><span class="label-text">PHP 版本</span></div>
                <select class="select select-bordered w-full select-sm" bind:value={editing.phpVersion}>
                  <option value="">不使用</option>
                  {#each phpVersions as pv}<option value={pv.version}>PHP {pv.version} (端口 {pv.port})</option>{/each}
                </select>
              </label>
            </div>
          {/if}
        </div>
      </div>
    {:else}
      <div class="flex flex-col items-center justify-center h-full text-base-content/60">
        <div class="text-4xl opacity-50 mb-3">🌐</div>
        <p>选择左侧项目进行编辑</p>
        <p class="text-xs mt-1">或点击 + 创建新项目</p>
      </div>
    {/if}
  </div>
</div>
