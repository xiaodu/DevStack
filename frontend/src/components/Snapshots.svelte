<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { refreshSignal } from '../lib/stores'

let snapshots = $state<string[]>([])
let loading = $state(true)
let loadError = $state(false)
let busy = $state(false)
let confirmRollbackId = $state<string | null>(null)
let confirmDeleteId = $state<string | null>(null)

async function load() {
  loading = true
  loadError = false
  try {
    await safeInvoke(
      'loadSnapshots',
      async () => {
        const result = await api.getSnapshots()
        // 后端历史版本会把空列表序列化成 null，这里统一兜底成数组
        snapshots = Array.isArray(result) ? result : []
      },
      { errorPrefix: '加载快照失败', rethrow: true },
    )
  } catch {
    loadError = true
  } finally {
    loading = false
  }
}

async function createSnapshot() {
  if (busy) return
  busy = true
  try {
    await safeInvoke(
      'createSnapshot',
      async () => {
        await api.takeSnapshot()
        await load()
      },
      { successMsg: '快照已创建', errorPrefix: '创建快照失败' },
    )
  } finally {
    busy = false
  }
}

async function doRollback() {
  if (!confirmRollbackId || busy) return
  const id = confirmRollbackId
  busy = true
  try {
    await safeInvoke(
      'rollbackSnapshot',
      async () => {
        await api.rollbackSnapshot(id)
        confirmRollbackId = null
        refreshSignal.update((n) => n + 1)
      },
      { successMsg: '已回滚', errorPrefix: '回滚失败' },
    )
  } finally {
    busy = false
  }
}

async function doDelete() {
  if (!confirmDeleteId || busy) return
  const id = confirmDeleteId
  busy = true
  try {
    await safeInvoke(
      'deleteSnapshot',
      async () => {
        await api.deleteSnapshot(id)
        confirmDeleteId = null
        await load()
      },
      { successMsg: '已删除', errorPrefix: '删除失败' },
    )
  } finally {
    busy = false
  }
}

function formatTime(snapshotId: string) {
  if (!snapshotId?.startsWith('snapshot-')) return snapshotId
  const ts = snapshotId.replace('snapshot-', '')
  if (ts.length !== 13) return snapshotId
  return `${ts.slice(0, 4)}-${ts.slice(4, 6)}-${ts.slice(6, 8)} ${ts.slice(9, 11)}:${ts.slice(11, 13)}`
}

onMount(load)
</script>

<div class="flex flex-col gap-5">
  <div class="flex flex-wrap items-end justify-between gap-4">
    <div>
      <h1 class="text-xl font-bold">所有快照</h1>
      <p class="mt-1 text-sm text-base-content/60">保存当前组件与配置状态，随时回滚到历史节点</p>
    </div>
    <div class="flex items-center gap-2">
      {#if snapshots.length > 0}
        <span class="badge badge-sm border border-base-300 bg-base-100 text-base-content/60">{snapshots.length} 个快照</span>
      {/if}
      <button class="btn btn-primary btn-sm" onclick={createSnapshot} disabled={busy}>
        {#if busy}<span class="loading loading-spinner loading-xs"></span>{/if}
        创建快照
      </button>
    </div>
  </div>

  {#if loading}
    <div class="flex flex-col gap-2.5">
      {#each [1, 2, 3] as _}
        <div class="flex items-center gap-3 rounded-xl border border-base-300 bg-base-100 p-4">
          <div class="h-9 w-9 rounded-xl bg-base-200"></div>
          <div class="flex-1 space-y-2">
            <div class="h-3 w-1/3 rounded bg-base-200"></div>
            <div class="h-2.5 w-1/5 rounded bg-base-200"></div>
          </div>
        </div>
      {/each}
    </div>
  {:else if loadError}
    <div class="rounded-2xl border border-error/30 bg-base-100 p-12 text-center">
      <p class="text-sm font-semibold text-error">快照列表加载失败</p>
      <p class="mt-1 text-xs text-base-content/60">请查看日志后重试，失败详情已通过提示栏显示</p>
      <button class="btn btn-sm btn-outline mt-4" onclick={load}>重新加载</button>
    </div>
  {:else if snapshots.length === 0}
    <div class="flex flex-col items-center justify-center rounded-2xl border border-dashed border-base-300 bg-base-100/60 py-16">
      <div class="flex h-12 w-12 items-center justify-center rounded-2xl bg-base-200 text-base-content/40">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-6 w-6"><path d="M3 8V5a2 2 0 0 1 2-2h3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M21 16v3a2 2 0 0 1-2 2h-3"/><circle cx="12" cy="12" r="3.5"/></svg>
      </div>
      <p class="mt-3 text-sm font-semibold">暂无快照</p>
      <p class="mt-1 max-w-xs text-center text-xs text-base-content/55">快照会记录当前所有组件的运行状态与配置，创建后可随时回滚</p>
      <button class="btn btn-primary btn-sm mt-4" onclick={createSnapshot} disabled={busy}>创建第一个快照</button>
    </div>
  {:else}
    <div class="flex flex-col gap-2.5">
      {#each [...snapshots].reverse() as snap}
        <article class="flex items-center gap-3 rounded-xl border border-base-300 bg-base-100 px-4 py-3 shadow-sm transition-colors hover:border-primary/40">
          <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-4.5 w-4.5"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>
          </div>
          <div class="min-w-0 flex-1">
            <div class="truncate font-mono text-sm">{snap}</div>
            <div class="mt-0.5 text-xs text-base-content/50">{formatTime(snap)}</div>
          </div>
          <div class="flex shrink-0 items-center gap-1.5">
            <button class="btn btn-sm btn-outline" onclick={() => (confirmRollbackId = snap)} disabled={busy}>回滚</button>
            <button class="btn btn-sm btn-ghost btn-error" onclick={() => (confirmDeleteId = snap)} disabled={busy}>删除</button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<!-- Rollback confirm modal -->
<dialog class="modal" class:modal-open={!!confirmRollbackId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认回滚</h3>
    <p class="py-2 text-sm text-base-content/70">确定要回滚到快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmRollbackId}</code> 吗？</p>
    <p class="text-xs text-warning">当前运行中的组件状态将被覆盖。</p>
    <div class="modal-action">
      <button class="btn btn-ghost" onclick={() => confirmRollbackId = null}>取消</button>
      <button class="btn btn-error" onclick={doRollback} disabled={busy}>确认回滚</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={() => confirmRollbackId = null}>关闭</button></form>
</dialog>

<!-- Delete confirm modal -->
<dialog class="modal" class:modal-open={!!confirmDeleteId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认删除</h3>
    <p class="py-2 text-sm text-base-content/70">确定要删除快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmDeleteId}</code> 吗？</p>
    <div class="modal-action">
      <button class="btn btn-ghost" onclick={() => confirmDeleteId = null}>取消</button>
      <button class="btn btn-error" onclick={doDelete} disabled={busy}>确认删除</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={() => confirmDeleteId = null}>关闭</button></form>
</dialog>
