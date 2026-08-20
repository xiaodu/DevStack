<script lang="ts">
import { createBubbler, stopPropagation } from 'svelte/legacy'

const bubble = createBubbler()

import { fade } from 'svelte/transition'
import { nanoLoading, nanoStatusMsg, statusMessage } from '../lib/stores'

// 加载状态的清理统一由 safeInvoke（15s 安全超时 + finally 复位）与 runOperation（60s 看门狗）负责，
// 无需此处 8s 强制清 loading —— 旧 timer 会与上述超时打架，提前误关遮罩（F9）。
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="fixed inset-0 z-[9999] flex flex-col items-center justify-center bg-black/50 backdrop-blur-sm transition-opacity duration-300"
  class:opacity-100={$nanoLoading}
  class:opacity-0={!$nanoLoading}
  class:pointer-events-none={!$nanoLoading}
  onclick={stopPropagation(bubble('click'))}
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