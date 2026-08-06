<script lang="ts">
import { fade } from 'svelte/transition'
import { operation } from '../lib/stores'
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<!-- svelte-ignore a11y-no-static-element-interactions -->
{#if $operation.active}
  <div
    class="fixed inset-0 z-[9999] flex flex-col items-center justify-center bg-black/50 backdrop-blur-sm transition-opacity duration-300"
    on:click|stopPropagation
  >
    <div
      class="card bg-base-100 shadow-2xl border border-base-300 px-8 py-6 min-w-[340px] max-w-[440px]"
      transition:fade={{ duration: 200 }}
    >
      <div class="flex items-center gap-3">
        {#if $operation.status === 'running'}
          <span class="loading loading-spinner loading-lg text-primary"></span>
        {:else if $operation.status === 'success'}
          <span class="w-10 h-10 rounded-full bg-success/15 text-success flex items-center justify-center text-2xl">✓</span>
        {:else}
          <span class="w-10 h-10 rounded-full bg-error/15 text-error flex items-center justify-center text-2xl">✕</span>
        {/if}
        <div class="min-w-0">
          <div class="font-semibold text-sm truncate">{$operation.title}</div>
          <div class="text-xs text-base-content/60 mt-0.5">{$operation.message}</div>
        </div>
      </div>

      {#if $operation.steps.length > 0}
        <ul class="mt-4 flex flex-col gap-1.5 text-sm">
          {#each $operation.steps as step (step.id)}
            <li class="flex items-center gap-2">
              {#if step.state === 'done'}
                <span class="w-4 text-success shrink-0">✓</span>
              {:else if step.state === 'error'}
                <span class="w-4 text-error shrink-0">✕</span>
              {:else if step.state === 'active'}
                <span class="w-4 flex justify-center shrink-0"><span class="loading loading-spinner loading-xs text-primary"></span></span>
              {:else}
                <span class="w-2 h-2 rounded-full bg-base-content/30 shrink-0 ml-1"></span>
              {/if}
              <span class="text-base-content/80">{step.label}</span>
              {#if step.message && step.state !== 'pending'}
                <span class="ml-auto text-xs text-base-content/50 truncate max-w-[120px]">{step.message}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}

      {#if $operation.status === 'error'}
        <div class="mt-4 text-xs text-error bg-error/10 rounded-lg px-3 py-2 break-all">{$operation.error}</div>
      {/if}
    </div>
  </div>
{/if}
