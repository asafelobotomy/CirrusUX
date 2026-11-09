<script lang="ts">
  import { live } from '../state/live.svelte';
  import { mode } from '../api.svelte';
  import { TriangleAlert } from '../icons';

  // Shown when the gateway and the server's agent speak different protocols.
  // Reading still works; actions stay off until the older side is upgraded.
  const where = $derived(live.agentTooOld ? mode.server : mode.gateway);
  const show = $derived(mode.live && (live.agentTooOld || live.gatewayTooOld));
</script>

{#if show}
  <p class="banner" role="status">
    <TriangleAlert size={15} />
    <span>
      CirrusUX on <strong>{where}</strong> is older than the other half, so changes are turned off for now.
      Upgrade it there with <code>sudo cirrusux-setup upgrade</code>, then reload.
    </span>
  </p>
{/if}

<style>
  .banner {
    display: flex; gap: var(--space-2); align-items: flex-start; margin: 0;
    padding: var(--space-2) var(--space-4); font-size: 0.88rem;
    background: var(--warn-soft);
    border-bottom: 1px solid var(--border);
  }
  .banner :global(svg) { color: var(--warn); flex: none; margin-top: 2px; }
  code { font-family: var(--font-mono, monospace); }
</style>
