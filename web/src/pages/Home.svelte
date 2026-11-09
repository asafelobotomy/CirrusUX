<script lang="ts">
  import Board from '../lib/layout/Board.svelte';
  import Button from '../lib/components/Button.svelte';
  import { activeBoard } from '../lib/state/layout.svelte';
  import { ui } from '../lib/state/ui.svelte';
  import { Plus } from '../lib/icons';

  let { onAddWidget }: { onAddWidget: () => void } = $props();
  let width = $state(0);
  $effect(() => {
    if (width) ui.boardDevice = width < 600 ? 'phone' : width < 1000 ? 'tablet' : 'desktop';
  });
  const items = $derived(activeBoard().layouts[ui.boardDevice]);
</script>

<div bind:clientWidth={width}>

{#if items.length}
  <Board {items} device={ui.boardDevice} editing={ui.editLayout} />
{:else}
  <div class="empty">
    <h2>This board is empty</h2>
    <p class="muted">Add widgets to build the {ui.boardDevice} layout.</p>
    <Button variant="primary" onclick={() => { ui.editLayout = true; onAddWidget(); }}>{#snippet icon()}<Plus />{/snippet}Add a widget</Button>
  </div>
{/if}
</div>

<style>
  .empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 80px 16px; text-align: center; border: 1px dashed var(--border-strong); border-radius: var(--radius-lg); }
</style>
