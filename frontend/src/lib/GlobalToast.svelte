<script lang="ts">
import { fly } from 'svelte/transition'
import { dismissToast, toasts } from '../lib/stores'
</script>

<div class="fixed top-4 right-4 z-[99999] flex flex-col gap-2 max-w-sm w-full pointer-events-none">
  {#each $toasts as t (t.id)}
    <div
      class="alert shadow-xl border flex items-start gap-3 px-4 py-3 pointer-events-auto cursor-pointer"
      class:alert-success={t.type === 'success'}
      class:alert-error={t.type === 'error'}
      class:alert-info={t.type === 'info' || (t.type !== 'success' && t.type !== 'error')}
      transition:fly={{ x: 40, duration: 250, opacity: 0 }}
      on:click={() => dismissToast(t.id)}
      role="alert"
    >
      <span class="text-sm flex-1">{t.msg}</span>
      <button
        class="btn btn-ghost btn-xs text-base-content/50 hover:text-base-content shrink-0 -mr-1"
        on:click|stopPropagation={() => dismissToast(t.id)}
        aria-label="关闭"
      >✕</button>
    </div>
  {/each}
</div>