<script lang="ts">
  import Button from '../components/Button.svelte';
  import { go } from '../state/ui.svelte';
  import { onMount } from 'svelte';
  import { loadUpdates, pendingUpdates, store } from '../state/store.svelte';
  import { fmtAgo } from '../time.svelte';

  const waiting = $derived(pendingUpdates());
  onMount(() => void loadUpdates());
</script>

<div class="u">
  {#if waiting.length}
    <p><strong class="num">{waiting.length}</strong> <span class="muted">update{waiting.length === 1 ? '' : 's'} waiting</span></p>
    <ul>
      {#each waiting as u}
        <li><span>{u.name}</span><code class="faint">{u.current} → {u.latest}</code></li>
      {/each}
    </ul>
  {:else if store.updates.checked}
    <p class="muted">Up to date, as of {fmtAgo(store.updates.checked)}.</p>
  {:else}
    <p class="muted">Not checked yet. Checks run only when you ask.</p>
  {/if}
  <Button size="sm" onclick={() => go('updates')}>Review</Button>
</div>

<style>
  .u { height: 100%; display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
  ul { list-style: none; margin: 0; padding: 0; width: 100%; display: flex; flex-direction: column; gap: 4px; font-size: 0.87rem; }
  li { display: flex; justify-content: space-between; gap: 8px; }
</style>
