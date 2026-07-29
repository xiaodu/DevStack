<script lang="ts">
import Database from './Database.svelte'
import Runtime from './Runtime.svelte'
import WebServer from './WebServer.svelte'

let activeTab = 'webserver'
</script>

<div class="flex flex-col">
  <div role="tablist" class="tabs tabs-bordered mb-5">
    <button class="tab" class:tab-active={activeTab === 'webserver'} on:click={() => activeTab = 'webserver'}>Web 服务器</button>
    <button class="tab" class:tab-active={activeTab === 'database'} on:click={() => activeTab = 'database'}>数据库</button>
    <button class="tab" class:tab-active={activeTab === 'runtime'} on:click={() => activeTab = 'runtime'}>运行环境</button>
  </div>
  <div class="flex-1">
    {#if activeTab === 'webserver'}
      <WebServer />
    {:else if activeTab === 'database'}
      <Database />
    {:else if activeTab === 'runtime'}
      <Runtime />
    {/if}
  </div>
</div>
