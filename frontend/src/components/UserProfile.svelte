<script lang="ts">
import { api } from '../lib/api'

interface Props {
  open: boolean
  onclose: () => void
}

let { open, onclose }: Props = $props()

let info = $state<any>(null)
let loading = $state(false)

// 首次打开时拉取本机信息（平台/版本/安装目录），失败则静默留空
$effect(() => {
  if (!open || info || loading) return
  loading = true
  api
    .getSystemInfo()
    .then((r) => (info = r))
    .catch(() => (info = {}))
    .finally(() => (loading = false))
})

const rows = $derived<{ label: string; value: string; mono?: boolean }[]>([
  { label: '应用版本', value: info?.appVersion || (loading ? '读取中…' : '—') },
  { label: '运行平台', value: info?.platform ? `${info.platform} / ${info.arch}` : '—' },
  { label: '软件安装目录', value: info?.installDir || '未设置', mono: true },
])
</script>

<dialog class="modal" class:modal-open={open}>
  <div class="modal-box max-w-[420px] p-0">
    <!-- 头部 -->
    <div class="px-6 pt-6 pb-5 bg-gradient-to-br from-primary/10 to-transparent border-b border-base-300">
      <div class="flex items-center gap-4">
        <div
          class="w-14 h-14 rounded-full bg-primary text-primary-content flex items-center justify-center text-xl font-semibold shrink-0"
        >
          D
        </div>
        <div class="min-w-0">
          <div class="text-base font-semibold truncate">DevStack User</div>
          <div class="flex items-center gap-2 mt-1">
            <span class="badge badge-sm badge-outline text-base-content/60 border-base-content/20">社区版</span>
            <span class="text-xs text-base-content/50">本地模式</span>
          </div>
        </div>
      </div>
      <p class="text-xs text-base-content/50 mt-3 leading-relaxed">
        当前无需登录，所有配置与已安装组件均保存在本机。账户与云端同步功能尚未开放。
      </p>
    </div>

    <!-- 本机信息 -->
    <div class="px-6 py-4">
      <div class="text-xs font-semibold text-base-content/50 mb-2">本机信息</div>
      <div class="flex flex-col gap-2">
        {#each rows as row}
          <div class="flex items-baseline justify-between gap-4 text-sm">
            <span class="text-base-content/60 shrink-0">{row.label}</span>
            <span class="truncate {row.mono ? 'font-mono text-xs' : ''}" title={String(row.value)}>{row.value}</span>
          </div>
        {/each}
      </div>
    </div>

    <div class="modal-action px-6 pb-5 mt-0">
      <button class="btn btn-sm btn-ghost" onclick={onclose}>关闭</button>
    </div>
  </div>
  <form method="dialog" class="modal-backdrop"><button onclick={onclose}>关闭</button></form>
</dialog>
