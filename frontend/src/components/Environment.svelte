<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast, refreshSignal } from '../lib/stores'

let env = { services: {}, projects: {}, runtimes: {}, activeProfileID: '' }
let envLoading = true
let profiles = []
let profilesLoading = true
let selectedProfile = null
let editingProfile = null
let profileModalOpen = false
let snapshots = []
let snapshotDetails = {}
let snapLoading = true
let busy = false
let confirmRollbackId = null
let confirmDeleteId = null
let confirmDeleteProfileId = null
let selectedSnapshotId = null
let activeTab = 'profiles'
let availableServices = []
let availableRuntimes = []

$: sortedSnapshots = Array.isArray(snapshots) ? [...snapshots].reverse() : []

// 当前激活的 profile ID
$: activeProfileID = env.activeProfileID || ''
$: hasRunningServices =
  Object.values(env.services || {}).some((s) => s.state === 'running') ||
  Object.values(env.runtimes || {}).some((r) => r.state === 'running')

async function withTimeout(promise, timeoutMs = 5000, fallback = null) {
  return Promise.race([promise, new Promise((resolve) => setTimeout(() => resolve(fallback), timeoutMs))])
}

async function loadEnvironment() {
  envLoading = true
  try {
    const result = await withTimeout(api.getActiveEnvironment(), 5000, {
      services: {},
      projects: {},
      runtimes: {},
      activeProfileID: '',
    })
    env = result || { services: {}, projects: {}, runtimes: {}, activeProfileID: '' }
  } catch (e) {
    console.error('[Environment] load error:', e)
  } finally {
    envLoading = false
  }
}

async function loadProfiles() {
  profilesLoading = true
  try {
    const result = await withTimeout(api.listEnvironmentProfiles(), 5000, [])
    profiles = Array.isArray(result) ? result : []
  } catch (e) {
    profiles = []
  } finally {
    profilesLoading = false
  }
}

async function loadSnapshots() {
  snapLoading = true
  try {
    const result = await withTimeout(api.getSnapshots(), 5000, [])
    snapshots = Array.isArray(result) ? result : []
  } catch (e) {
    console.error('[Environment] snapshots error:', e)
    snapshots = []
  } finally {
    snapLoading = false
  }
}

async function load() {
  await Promise.all([loadEnvironment(), loadProfiles(), loadSnapshots()])
}

// 检查 profile 中声明的服务/运行时是否全部已安装
function checkProfileInstalled(profile) {
  const svcs = profile.services
    ? Array.isArray(profile.services)
      ? profile.services
      : Object.values(profile.services)
    : []
  const rts = profile.runtimes
    ? Array.isArray(profile.runtimes)
      ? profile.runtimes
      : Object.values(profile.runtimes)
    : []

  const allInstalled = []
  const missing = []

  for (const s of svcs) {
    const found = availableServices.find((av) => av.id === s.id && av.installed)
    if (found) {
      allInstalled.push(`${s.id} ${s.version}`)
    } else {
      missing.push(`${s.id} ${s.version}`)
    }
  }

  for (const r of rts) {
    // 运行时 ID 格式为 "php-8.3"，但 profile 中 runtimeId 为 "php"，version 为 "8.3"
    const rtID = `${r.runtimeId}-${r.version}`
    const found = availableRuntimes.find((av) => av.id === rtID && av.installed)
    if (found) {
      allInstalled.push(`${r.runtimeId} ${r.version}`)
    } else {
      missing.push(`${r.runtimeId} ${r.version}`)
    }
  }

  return {
    allInstalled: missing.length === 0,
    missing,
    total: allInstalled.length + missing.length,
  }
}

async function activateCurrent() {
  if (busy) return
  busy = true
  try {
    await withTimeout(api.activateEnvironment(''), 8000)
    addToast('环境已按依赖顺序激活', 'success')
    refreshSignal.update((n) => n + 1)
    await load()
  } catch (e) {
    addToast(`环境激活失败: ${e.message || e}`, 'error')
  } finally {
    busy = false
  }
}

async function deactivateCurrent() {
  await safeInvoke(
    'deactivateEnvironment',
    async () => {
      await api.deactivateEnvironment()
      refreshSignal.update((n) => n + 1)
      await load()
    },
    { successMsg: '环境已按依赖顺序安全停止', errorPrefix: '环境停止失败' },
  )
}

async function activateProfile(profile) {
  if (busy) return
  const installCheck = checkProfileInstalled(profile)
  if (!installCheck.allInstalled && installCheck.total > 0) {
    addToast(`部分组件未安装: ${installCheck.missing.join('，')}`, 'warning')
  }
  busy = true
  try {
    await withTimeout(api.activateEnvironment(profile.id), 15000)
    refreshSignal.update((n) => n + 1)
    await load()
    addToast(`已按「${profile.name}」激活环境`, 'success')
  } catch (e) {
    addToast(`环境激活失败: ${e.message || e}`, 'error')
  } finally {
    busy = false
  }
}

async function saveProfile() {
  if (!editingProfile.name?.trim()) {
    addToast('请填写环境名称', 'error')
    return
  }
  await safeInvoke(
    'saveProfile',
    async () => {
      if (editingProfile.id) {
        await api.updateEnvironmentProfile(editingProfile)
      } else {
        await api.createEnvironmentProfile(editingProfile)
      }
      profileModalOpen = false
      editingProfile = null
      await loadProfiles()
    },
    { successMsg: editingProfile.id ? '环境已更新' : '环境已创建', errorPrefix: '保存失败' },
  )
}

async function doDeleteProfile() {
  if (!confirmDeleteProfileId) return
  const deletedId = confirmDeleteProfileId
  await safeInvoke(
    'deleteProfile',
    async () => {
      await api.deleteEnvironmentProfile(deletedId)
      confirmDeleteProfileId = null
      if (selectedProfile?.id === deletedId) selectedProfile = null
      await loadProfiles()
    },
    { successMsg: '环境已删除', errorPrefix: '删除失败' },
  )
}

async function createSnapshot() {
  await safeInvoke(
    'createSnapshot',
    async () => {
      const id = await api.takeSnapshot()
      await loadSnapshots()
    },
    { successMsg: '快照已创建', errorPrefix: '创建快照失败' },
  )
}

async function doRollback() {
  if (!confirmRollbackId) return
  if (busy) return
  busy = true
  const id = confirmRollbackId
  try {
    await withTimeout(api.rollbackSnapshot(id), 30000)
    confirmRollbackId = null
    refreshSignal.update((n) => n + 1)
    await load()
    addToast('已回滚', 'success')
  } catch (e) {
    addToast(`回滚失败: ${e.message || e}`, 'error')
  } finally {
    busy = false
  }
}

async function doDelete() {
  if (!confirmDeleteId) return
  if (busy) return
  busy = true
  const id = confirmDeleteId
  try {
    await withTimeout(api.deleteSnapshot(id), 10000)
    delete snapshotDetails[id]
    if (selectedSnapshotId === id) selectedSnapshotId = null
    confirmDeleteId = null
    snapshots = snapshots.filter((s) => s !== id)
    addToast('快照已删除', 'success')
  } catch (e) {
    addToast(`删除失败: ${e.message || e}`, 'error')
  } finally {
    busy = false
  }
}

function formatTime(snapshotId) {
  if (!snapshotId?.startsWith('snapshot-')) return snapshotId
  const ts = snapshotId.replace('snapshot-', '')
  if (ts.length !== 13) return snapshotId
  return `${ts.slice(0, 4)}-${ts.slice(4, 6)}-${ts.slice(6, 8)} ${ts.slice(9, 11)}:${ts.slice(11, 13)}`
}

function profileSummary(profile) {
  const parts = []
  const svcs = profile.services
    ? Array.isArray(profile.services)
      ? profile.services
      : Object.values(profile.services)
    : []
  const rts = profile.runtimes
    ? Array.isArray(profile.runtimes)
      ? profile.runtimes
      : Object.values(profile.runtimes)
    : []
  if (svcs.length) parts.push(svcs.map((s) => `${s.id || '?'} ${s.version || ''}`).join('，'))
  if (rts.length) parts.push(rts.map((r) => `${r.runtimeId || '?'} ${r.version || ''}`).join('，'))
  return parts.length ? parts.join('；') : '空环境模板'
}

function stateLabel(state) {
  return { running: '运行中', stopped: '已停止', starting: '启动中', stopping: '停止中' }[state] || state || '未知'
}

function openCreateProfile() {
  editingProfile = { id: '', name: '', description: '', isBuiltin: false, services: [], runtimes: [] }
  profileModalOpen = true
}

function openEditProfile(profile) {
  if (profile.isBuiltin) {
    addToast('内置预设无法编辑，建议先保存为自定义环境', 'warning')
    return
  }
  editingProfile = JSON.parse(JSON.stringify(profile))
  profileModalOpen = true
}

function loadSnapshotDetail(id) {
  if (selectedSnapshotId === id) {
    selectedSnapshotId = null
    return
  }
  if (snapshotDetails[id]) {
    selectedSnapshotId = id
    return
  }
  withTimeout(api.getSnapshotDetail(id), 8000)
    .then((detail) => {
      if (detail) {
        snapshotDetails = { ...snapshotDetails, [id]: detail }
        selectedSnapshotId = id
      }
    })
    .catch(() => addToast('加载快照详情失败', 'error'))
}

function addServiceSpec() {
  editingProfile.services = [...(editingProfile.services || []), { id: '', version: '' }]
}
function removeServiceSpec(idx) {
  const a = [...editingProfile.services]
  a.splice(idx, 1)
  editingProfile.services = a
}
function updateServiceSpec(idx, field, value) {
  const a = [...editingProfile.services]
  a[idx] = { ...a[idx], [field]: value }
  editingProfile.services = a
}
function addRuntimeSpec() {
  editingProfile.runtimes = [...(editingProfile.runtimes || []), { runtimeId: '', version: '' }]
}
function removeRuntimeSpec(idx) {
  const a = [...editingProfile.runtimes]
  a.splice(idx, 1)
  editingProfile.runtimes = a
}
function updateRuntimeSpec(idx, field, value) {
  const a = [...editingProfile.runtimes]
  a[idx] = { ...a[idx], [field]: value }
  editingProfile.runtimes = a
}

$: svcEntries = Object.entries(env.services || {})
$: projEntries = Object.entries(env.projects || {})
$: rtEntries = Object.entries(env.runtimes || {})

const unsub = refreshSignal.subscribe(() => load())
onMount(() => {
  load()
  Promise.all([
    withTimeout(api.getServices(), 3000, [])
      .then((s) => (availableServices = Array.isArray(s) ? s : []))
      .catch(() => {}),
    withTimeout(api.getRuntimes(), 3000, [])
      .then((r) => (availableRuntimes = Array.isArray(r) ? r : []))
      .catch(() => {}),
  ])
  return unsub
})
</script>

<div class="flex flex-col gap-5">
  <div class="flex justify-between items-center">
    <div>
      <h1 class="text-xl font-bold">环境管理</h1>
      <p class="text-sm text-base-content/60 mt-1">管理环境模板、当前状态和快照存档</p>
    </div>
    <div class="flex gap-2">
      {#if activeTab === 'profiles'}
        <button class="btn btn-primary btn-sm" on:click={openCreateProfile} disabled={busy}>新建环境</button>
      {/if}
      {#if activeTab === 'snapshots'}
        <button class="btn btn-primary btn-sm" on:click={createSnapshot} disabled={busy}>{busy ? '创建中...' : '创建快照'}</button>
      {/if}
    </div>
  </div>

  <div role="tablist" class="tabs tabs-bordered">
    <button class="tab" class:tab-active={activeTab === 'profiles'} on:click={() => activeTab = 'profiles'}>我的环境</button>
    <button class="tab" class:tab-active={activeTab === 'overview'} on:click={() => activeTab = 'overview'}>运行状态</button>
    <button class="tab" class:tab-active={activeTab === 'snapshots'} on:click={() => activeTab = 'snapshots'}>快照存档</button>
  </div>

  {#if activeTab === 'profiles'}
    <!-- 当前环境操作栏 -->
    <div class="flex items-center justify-between px-4 py-3 bg-base-100 border border-base-300 rounded-lg">
      <div class="flex items-center gap-3">
        {#if envLoading}
          <span class="text-sm text-base-content/60">加载中...</span>
        {:else if activeProfileID}
          <span class="w-2 h-2 rounded-full bg-success"></span>
          <span class="text-sm">
            当前环境：
            <span class="font-semibold text-primary">
              {profiles.find((p) => p.id === activeProfileID)?.name || '自定义环境'}
            </span>
          </span>
        {:else if hasRunningServices}
          <span class="w-2 h-2 rounded-full bg-success"></span>
          <span class="text-sm">当前环境：<span class="font-semibold">基于项目推导</span></span>
        {:else}
          <span class="w-2 h-2 rounded-full bg-base-content/30"></span>
          <span class="text-sm text-base-content/60">未激活环境</span>
        {/if}
      </div>
      <div class="flex gap-2">
        {#if !hasRunningServices}
          <button class="btn btn-sm btn-primary" on:click={activateCurrent} disabled={busy || envLoading}>
            {busy ? '处理中...' : '激活环境'}
          </button>
        {:else}
          <button class="btn btn-sm btn-outline" on:click={deactivateCurrent} disabled={busy}>
            停止环境
          </button>
        {/if}
      </div>
    </div>

    {#if profilesLoading}
      <div class="text-center py-16 text-base-content/60">加载环境模板中...</div>
    {:else if profiles.length === 0}
      <div class="flex flex-col items-center justify-center py-16 text-base-content/60 gap-2">
        <p class="text-base font-semibold">暂无环境</p>
        <p class="text-sm">新建一个环境，组合不同版本的软件和运行时</p>
        <p class="text-xs text-base-content/50">多个项目可共享同一个环境</p>
        <div class="mt-4"><button class="btn btn-primary btn-sm" on:click={openCreateProfile} disabled={busy}>新建第一个环境</button></div>
      </div>
    {:else}
      <div class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4">
        {#each profiles as profile (profile.id)}
          {@const isActive = activeProfileID === profile.id}
          {@const installCheck = checkProfileInstalled(profile)}
          <div class="card bg-base-100 border border-base-300 overflow-hidden transition-all hover:border-primary hover:-translate-y-0.5 {isActive ? 'border-primary shadow-md' : ''} {profile.isBuiltin ? 'bg-gradient-to-br from-base-100 to-base-200' : ''}">
            <!-- 卡片头部 -->
            <div class="px-4 pt-4 pb-3 flex justify-between items-start gap-2">
              <div class="min-w-0 flex-1">
                <div class="text-[15px] font-semibold flex items-center gap-2 flex-wrap">
                  {profile.name}
                  {#if profile.isBuiltin}<span class="badge badge-xs badge-ghost">预设</span>{/if}
                  {#if isActive}<span class="badge badge-xs badge-success">当前在用</span>{/if}
                </div>
                {#if profile.description}
                  <div class="text-xs text-base-content/60 mt-1 leading-relaxed">{profile.description}</div>
                {/if}
              </div>
            </div>

            <!-- 卡片内容：组件摘要 + 安装状态 -->
            <div class="px-4 pb-3">
              <div class="text-xs text-base-content/70 leading-relaxed bg-base-200 px-3 py-2.5 rounded-lg min-h-[40px]">
                {profileSummary(profile)}
              </div>
              {#if installCheck.total > 0}
                <div class="flex items-center gap-1.5 mt-2 text-xs">
                  {#if installCheck.allInstalled}
                    <span class="text-success">✓ 全部组件已安装</span>
                  {:else}
                    <span class="text-warning">⚠ {installCheck.missing.length} 项未安装: {installCheck.missing.slice(0, 3).join('，')}{installCheck.missing.length > 3 ? '...' : ''}</span>
                  {/if}
                </div>
              {/if}
            </div>

            <!-- 卡片底部：操作按钮 -->
            <div class="px-4 pb-4 pt-1 flex gap-2 items-center">
              {#if isActive}
                <button class="btn btn-sm btn-disabled" disabled>当前在用</button>
              {:else}
                <button class="btn btn-sm btn-primary" on:click={() => activateProfile(profile)} disabled={busy}>激活</button>
              {/if}
              {#if !profile.isBuiltin}
                <button class="btn btn-sm btn-ghost" on:click={() => openEditProfile(profile)} disabled={busy}>编辑</button>
                <button class="btn btn-sm btn-ghost text-error" on:click={() => confirmDeleteProfileId = profile.id} disabled={busy}>删除</button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}

  {#if activeTab === 'overview'}
    {#if envLoading}
      <div class="text-center py-16 text-base-content/60">加载环境状态中...</div>
    {:else}
      <div class="grid grid-cols-3 gap-5">
        <!-- Services -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">服务 ({svcEntries.length})</h3>
          {#if svcEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无已安装的服务</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each svcEntries as [id, svc]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm">
                  <span class="font-medium">{id}</span>
                  <span class="font-mono text-base-content/50 text-xs">{svc.version}</span>
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {svc.state === 'running' ? 'bg-success/10 text-success' : svc.state === 'stopped' ? 'bg-base-200 text-base-content/50' : 'bg-warning/10 text-warning'}">{stateLabel(svc.state)}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Projects -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">项目 ({projEntries.length})</h3>
          {#if projEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无已启用的项目</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each projEntries as [id, proj]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm flex-wrap">
                  <span class="font-medium">{proj.name || id}</span>
                  <span class="text-xs px-1.5 py-0.5 rounded bg-primary/10 text-primary">{proj.type}</span>
                  {#if proj.phpVersion}<span class="font-mono text-base-content/50 text-xs">PHP {proj.phpVersion}</span>{/if}
                  {#if proj.mysqlVersion}<span class="font-mono text-base-content/50 text-xs">MySQL {proj.mysqlVersion}</span>{/if}
                  {#if proj.nginxVersion}<span class="font-mono text-base-content/50 text-xs">Nginx {proj.nginxVersion}</span>{/if}
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {proj.enabled ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/50'}">{proj.enabled ? '已启用' : '已禁用'}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Runtimes -->
        <div class="card bg-base-100 border border-base-300 p-4">
          <h3 class="text-sm font-semibold text-primary mb-3">运行时 ({rtEntries.length})</h3>
          {#if rtEntries.length === 0}
            <div class="text-xs text-base-content/50 py-2">暂无运行时</div>
          {:else}
            <div class="flex flex-col gap-1.5">
              {#each rtEntries as [id, rt]}
                <div class="flex items-center gap-2 px-2.5 py-2 bg-base-200 rounded-lg text-sm">
                  <span class="font-medium">{id}</span>
                  <span class="font-mono text-base-content/50 text-xs">{rt.version}</span>
                  {#if rt.active}<span class="text-xs px-1.5 py-0.5 rounded bg-warning/10 text-warning">默认</span>{/if}
                  <span class="ml-auto text-xs px-2 py-0.5 rounded-full {rt.state === 'running' ? 'bg-success/10 text-success' : rt.state === 'stopped' ? 'bg-base-200 text-base-content/50' : 'bg-warning/10 text-warning'}">{stateLabel(rt.state)}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}

  {#if activeTab === 'snapshots'}
    {#if snapLoading}
      <div class="text-center py-16 text-base-content/60">加载快照中...</div>
    {:else if snapshots.length === 0}
      <div class="flex flex-col items-center justify-center py-16 text-base-content/60 gap-2">
        <p class="text-base font-semibold">暂无快照</p>
        <p class="text-sm">快照保存当前所有服务的运行状态和配置</p>
        <p class="text-xs">创建快照后，您可以随时回滚到该状态</p>
        <div class="mt-4"><button class="btn btn-primary btn-sm" on:click={createSnapshot} disabled={busy}>{busy ? '创建中...' : '创建第一个快照'}</button></div>
      </div>
    {:else}
      <div class="flex flex-col gap-2.5">
        {#each sortedSnapshots as snap}
          <div class="card bg-base-100 border border-base-300" class:border-primary={selectedSnapshotId === snap}>
            <div class="card-body p-3.5">
              <div class="flex items-center justify-between cursor-pointer" on:click={() => loadSnapshotDetail(snap)} on:keydown={() => {}}>
                <div class="flex-1">
                  <div class="font-mono text-sm">{snap}</div>
                  <div class="text-xs text-base-content/60 mt-0.5">{formatTime(snap)}</div>
                  {#if snapshotDetails[snap]}
                    {@const detail = snapshotDetails[snap]}
                    <div class="flex gap-1.5 mt-1.5 flex-wrap">
                      {#if detail.services}<span class="badge badge-sm badge-ghost">{Object.keys(detail.services).length} 个服务</span>{/if}
                      {#if detail.projects}<span class="badge badge-sm badge-ghost">{Object.keys(detail.projects).length} 个项目</span>{/if}
                      {#if detail.runtimes}<span class="badge badge-sm badge-ghost">{Object.keys(detail.runtimes).length} 个运行时</span>{/if}
                    </div>
                  {/if}
                </div>
                <div class="flex gap-2 shrink-0">
                  <button class="btn btn-sm btn-outline" on:click|stopPropagation={() => confirmRollbackId = snap} disabled={busy}>回滚</button>
                  <button class="btn btn-sm btn-ghost text-error" on:click|stopPropagation={() => confirmDeleteId = snap} disabled={busy}>删除</button>
                </div>
              </div>

              {#if selectedSnapshotId === snap && snapshotDetails[snap]}
                {@const detail = snapshotDetails[snap]}
                <div class="mt-3 pt-3 border-t border-base-300 space-y-3.5">
                  {#if detail.services && Object.keys(detail.services).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">服务状态</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.services) as [id, svc]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{id}</span>
                            <span class="font-mono text-base-content/50">{svc.version}</span>
                            <span class="ml-auto px-1.5 py-0.5 rounded-full {svc.state === 'running' ? 'bg-success/10 text-success' : 'bg-base-200 text-base-content/50'}">{svc.state === 'running' ? '运行中' : '已停止'}</span>
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                  {#if detail.projects && Object.keys(detail.projects).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">项目环境</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.projects) as [id, proj]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{proj.name || id}</span>
                            <span class="text-base-content/50">{proj.type}</span>
                            {#if proj.phpVersion}<span class="font-mono text-base-content/50">PHP {proj.phpVersion}</span>{/if}
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                  {#if detail.runtimes && Object.keys(detail.runtimes).length > 0}
                    <div>
                      <h4 class="text-xs font-semibold text-base-content/70 mb-2">运行时</h4>
                      <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-1.5">
                        {#each Object.entries(detail.runtimes) as [id, rt]}
                          <div class="flex items-center gap-2 px-2.5 py-1.5 bg-base-200 rounded-lg text-xs">
                            <span class="font-medium">{id}</span>
                            <span class="font-mono text-base-content/50">{rt.version}</span>
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<!-- Profile Edit Modal -->
<dialog class="modal" class:modal-open={profileModalOpen && !!editingProfile}>
  <div class="modal-box max-w-[620px]">
    <h3 class="font-bold text-lg mb-3">{editingProfile?.id ? '编辑环境' : '新建环境'}</h3>
    {#if editingProfile}
      <div class="grid grid-cols-2 gap-3 mb-3.5">
        <label class="form-control col-span-2"><span class="label-text text-xs text-base-content/70 mb-1">名称 *</span><input class="input input-bordered input-sm" type="text" bind:value={editingProfile.name} placeholder="如：PHP7.4 + MySQL5.7" /></label>
        <label class="form-control col-span-2"><span class="label-text text-xs text-base-content/70 mb-1">描述</span><input class="input input-bordered input-sm" type="text" bind:value={editingProfile.description} placeholder="可选，简要说明用途" /></label>
      </div>

      <div class="mb-3.5 p-3 bg-base-200 rounded-lg">
        <div class="flex justify-between items-center mb-2.5 text-sm font-medium">
          <span>软件服务</span>
          <button class="btn btn-sm btn-outline" on:click={addServiceSpec} type="button">+ 添加</button>
        </div>
        {#if !editingProfile.services?.length}
          <div class="text-xs text-base-content/50 py-1">暂无服务，可添加 nginx、mysql、redis 等</div>
        {/if}
        {#each editingProfile.services as spec, idx}
          <div class="grid grid-cols-[1fr_auto] gap-2 mb-1.5">
            <select class="select select-bordered select-xs" value={spec.id} on:change={(e) => { updateServiceSpec(idx, 'id', e.target.value); const found = availableServices.find(s => s.id === e.target.value); if (found) updateServiceSpec(idx, 'version', found.version) }}>
              <option value="">选择服务</option>
              {#each availableServices as s}<option value={s.id}>{s.name} ({s.version})</option>{/each}
            </select>
            <button class="btn btn-sm btn-ghost text-error btn-xs" on:click={() => removeServiceSpec(idx)} type="button">移除</button>
          </div>
        {/each}
      </div>

      <div class="mb-3.5 p-3 bg-base-200 rounded-lg">
        <div class="flex justify-between items-center mb-2.5 text-sm font-medium">
          <span>运行时</span>
          <button class="btn btn-sm btn-outline" on:click={addRuntimeSpec} type="button">+ 添加</button>
        </div>
        {#if !editingProfile.runtimes?.length}
          <div class="text-xs text-base-content/50 py-1">暂无运行时，可添加 php、node、python 等</div>
        {/if}
        {#if availableRuntimes.length === 0}
          <div class="text-xs text-warning py-1">暂无已安装的运行时，请先在「安装软件」页面安装 PHP / Node / Python</div>
        {/if}
        {#each editingProfile.runtimes as spec, idx}
          <div class="grid grid-cols-[1fr_auto] gap-2 mb-1.5">
            <select class="select select-bordered select-xs" value={spec.runtimeId + '-' + spec.version} on:change={(e) => { const found = availableRuntimes.find(r => r.id === e.target.value); if (found) { updateRuntimeSpec(idx, 'runtimeId', found.type); updateRuntimeSpec(idx, 'version', found.version) } }}>
              <option value="">选择运行时</option>
              {#each availableRuntimes as r}<option value={r.id}>{r.name || r.type} ({r.version})</option>{/each}
            </select>
            <button class="btn btn-sm btn-ghost text-error btn-xs" on:click={() => removeRuntimeSpec(idx)} type="button">移除</button>
          </div>
        {/each}
      </div>
    {/if}
    <div class="modal-action">
      <button class="btn btn-ghost" on:click={() => { profileModalOpen = false; editingProfile = null }} disabled={busy}>取消</button>
      <button class="btn btn-primary" on:click={saveProfile} disabled={busy}>{busy ? '保存中...' : '保存'}</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button on:click={() => { profileModalOpen = false; editingProfile = null }}>关闭</button></form>
</dialog>

<!-- Delete Profile Confirm -->
<dialog class="modal" class:modal-open={!!confirmDeleteProfileId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认删除环境</h3>
    <p class="py-2 text-sm text-base-content/70">删除后无法恢复，正在使用该环境的项目将回退到独立配置。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" on:click={() => confirmDeleteProfileId = null}>取消</button>
      <button class="btn btn-error" on:click={doDeleteProfile} disabled={busy}>确认删除</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button on:click={() => confirmDeleteProfileId = null}>关闭</button></form>
</dialog>

<!-- Rollback Confirm -->
<dialog class="modal" class:modal-open={!!confirmRollbackId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认回滚</h3>
    <p class="py-2 text-sm text-base-content/70">确定要回滚到快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmRollbackId}</code> 吗？</p>
    <p class="text-xs text-warning">当前运行中的服务状态将被覆盖。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" on:click={() => confirmRollbackId = null}>取消</button>
      <button class="btn btn-error" on:click={doRollback} disabled={busy}>确认回滚</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button on:click={() => confirmRollbackId = null}>关闭</button></form>
</dialog>

<!-- Delete Snapshot Confirm -->
<dialog class="modal" class:modal-open={!!confirmDeleteId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认删除</h3>
    <p class="py-2 text-sm text-base-content/70">确定要删除快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmDeleteId}</code> 吗？</p>
    <p class="text-xs text-warning">删除后无法恢复。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" on:click={() => confirmDeleteId = null}>取消</button>
      <button class="btn btn-error" on:click={doDelete} disabled={busy}>确认删除</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button on:click={() => confirmDeleteId = null}>关闭</button></form>
</dialog>
