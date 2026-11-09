<script lang="ts">
  // The Media page: CirrusUX doesn't manage media libraries itself. The media
  // apps (Jellyfin and the like) do; this points to where they live.
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import { has } from '../lib/api.svelte';
  import { boards } from '../lib/state/layout.svelte';
  import { go } from '../lib/state/ui.svelte';

  const board = $derived(boards.list.find((b) => b.id === 'media'));

  function openBoard() {
    if (board) boards.active = board.id;
    go('home');
  }
</script>

<Card title="Media" subtitle="Your films, music and photos are run by the media apps you install, not by CirrusUX.">
  <p class="muted">
    The Media board gathers those apps and the storage they use on one page. To add a media app, use CirrusStore.
  </p>
  <div class="row">
    {#if board}<Button variant="primary" onclick={openBoard}>Open the Media board</Button>{/if}
    {#if has('apps')}<Button onclick={() => go('apps')}>Open CirrusStore</Button>{/if}
  </div>
</Card>

<style>
  p { margin: 0 0 var(--space-3); }
  .row { display: flex; flex-wrap: wrap; gap: var(--space-2); }
</style>
