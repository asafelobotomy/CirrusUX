<script lang="ts">
  import { live } from '../state/live.svelte';
  import { fmtDayTime } from '../time.svelte';
  import { Undo2 } from '../icons';
  import { toast } from '../state/ui.svelte';

  let { limit = 0 }: { limit?: number } = $props();
  const entries = $derived(limit ? live.changelog.slice(0, limit) : live.changelog);
</script>

<ol class="cl">
  {#each entries as e}
    <li>
      <span class="when faint num">{fmtDayTime(e.at)}</span>
      <span class="what">{e.what} <span class="faint">· {e.who}</span></span>
      {#if e.undo}
        <button class="undo" title={e.undo} onclick={() => toast('Undo would ask for confirmation first (prototype)', 'info')}>
          <Undo2 size={13} /> Undo
        </button>
      {/if}
    </li>
  {/each}
</ol>

<style>
  .cl { list-style: none; margin: 0; padding: 0; height: 100%; overflow: auto; display: flex; flex-direction: column; }
  li { display: grid; grid-template-columns: 9.5em 1fr auto; gap: var(--space-3); align-items: center; padding: 7px 0; border-bottom: 1px solid var(--border); font-size: 0.87rem; }
  li:last-child { border-bottom: 0; }
  .when { font-size: 0.78rem; }
  .undo { display: inline-flex; align-items: center; gap: 4px; border: 1px solid var(--border); background: transparent; color: var(--text-muted); border-radius: 999px; padding: 2px 8px; font-size: 0.75rem; }
  .undo:hover { color: var(--text); background: var(--hover); }
  @container (max-width: 420px) {
    li { grid-template-columns: 1fr auto; }
    .when { grid-column: 1 / -1; }
  }
</style>
