<script lang="ts">
  import Button from '../components/Button.svelte';
  import { live } from '../state/live.svelte';
  import { go } from '../state/ui.svelte';
</script>

<div class="u">
  {#if live.updates.length}
    <p><strong class="num">{live.updates.length}</strong> <span class="muted">updates waiting for approval</span></p>
    <ul>
      {#each live.updates as u}
        <li><span>{u.name}</span><code class="faint">{u.from} → {u.to}</code></li>
      {/each}
    </ul>
  {:else}
    <p class="muted">Update checks arrive in Phase 3. They run only when you ask.</p>
  {/if}
  <Button size="sm" onclick={() => go('updates')}>Review</Button>
</div>

<style>
  .u { height: 100%; display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
  ul { list-style: none; margin: 0; padding: 0; width: 100%; display: flex; flex-direction: column; gap: 4px; font-size: 0.87rem; }
  li { display: flex; justify-content: space-between; gap: 8px; }
</style>
