<script lang="ts">
import { onMount } from 'svelte'
import { api } from '../lib/api'
import { safeInvoke } from '../lib/safeInvoke'
import { addToast, refreshSignal } from '../lib/stores'

let snapshots = $state<string[]>([])
let loading = $state(true)
let busy = $state(false)
let confirmRollbackId = $state<string | null>(null)
let confirmDeleteId = $state<string | null>(null)

async function load() {
  loading = true
  await safeInvoke(
    'loadSnapshots',
    async () => {
      snapshots = await api.getSnapshots()
    },
    { errorPrefix: '加载快照失败' },
  )
  loading = false
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
  <div class="flex justify-between items-center">
    <div>
      <h1 class="text-xl font-bold">快照管理</h1>
      <p class="text-sm text-base-content/60 mt-1">保存和恢复环境状态</p>
    </div>
    <button class="btn btn-primary" onclick={createSnapshot} disabled={busy}>
      {busy ? '处理中...' : '创建快照'}
    </button>
  </div>

  {#if loading}
    <div class="text-center py-16 text-base-content/60">加载快照中...</div>
  {:else if snapshots.length === 0}
    <div class="flex flex-col items-center justify-center py-16 text-base-content/60 gap-2">
      <div class="text-4xl opacity-50">📸</div>
      <p class="text-base font-semibold text-base-content">暂无快照</p>
      <p class="text-xs">快照可以保存当前所有服务的运行状态和配置</p>
      <p class="text-xs">创建快照后，您可以随时回滚到该状态</p>
      <div class="mt-4"><button class="btn btn-primary" onclick={createSnapshot} disabled={busy}>{busy ? '创建中...' : '创建第一个快照'}</button></div>
    </div>
  {:else}
    <div class="flex flex-col gap-2.5">
      {#each [...snapshots].reverse() as snap}
        <div class="card bg-base-100 border border-base-300">
          <div class="card-body p-3.5 flex-row items-center justify-between">
            <div>
              <div class="font-mono text-sm">{snap}</div>
              <div class="text-xs text-base-content/60 mt-0.5">{formatTime(snap)}</div>
            </div>
            <button class="btn btn-sm btn-outline" onclick={() => confirmRollbackId = snap} disabled={busy}>回滚</button>
            <button class="btn btn-sm btn-ghost btn-error" onclick={() => confirmDeleteId = snap} disabled={busy}>删除</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Rollback confirm modal -->
<dialog class="modal" class:modal-open={!!confirmRollbackId}>
  <div class="modal-box">
    <h3 class="font-bold text-lg">确认回滚</h3>
    <p class="py-2 text-sm text-base-content/70">确定要回滚到快照 <code class="bg-base-300 px-1.5 py-0.5 rounded text-xs font-mono">{confirmRollbackId}</code> 吗？</p>
    <p class="text-xs text-warning">当前运行中的服务状态将被覆盖。</p>
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
