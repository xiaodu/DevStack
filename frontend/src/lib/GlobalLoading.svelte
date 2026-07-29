<script lang="ts">
import { onMount } from 'svelte'
import { fade } from 'svelte/transition'
import { nanoLoading, nanoStatusMsg, statusMessage } from '../lib/stores'

let safetyTimer

onMount(() => {
  // 安全定时器：8 秒后强制关闭加载状态，防止 nanoLoading 永久卡住
  safetyTimer = setInterval(() => {
    if ($nanoLoading) {
      console.warn('[GlobalLoading] 安全定时器触发，强制关闭加载状态')
      nanoLoading.set(false)
      nanoStatusMsg.set('')
    }
  }, 8000)
  return () => clearInterval(safetyTimer)
})
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="fixed inset-0 z-[9999] flex flex-col items-center justify-center bg-black/50 backdrop-blur-sm transition-opacity duration-300"
  class:opacity-100={$nanoLoading}
  class:opacity-0={!$nanoLoading}
  class:pointer-events-none={!$nanoLoading}
  on:click|stopPropagation
>
  {#if $nanoLoading}
    <div class="card bg-base-100 shadow-2xl border border-base-300 px-8 py-6 flex flex-col items-center gap-4 min-w-[220px]" transition:fade={{ duration: 200 }}>
      <span class="loading loading-spinner loading-lg text-primary"></span>
      {#if $statusMessage}
        <p class="text-sm text-base-content/70">{$statusMessage}</p>
      {/if}
      <p class="text-xs text-base-content/40 mt-2">Loading will auto-dismiss if it takes too long</p>
    </div>
  {/if}
</div>