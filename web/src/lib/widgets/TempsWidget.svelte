<script lang="ts">
  import Sparkline from '../components/Sparkline.svelte';
  import { live } from '../state/live.svelte';

  const cur = $derived(live.temps.at(-1));
  const peak = $derived(live.temps.length ? Math.max(...live.temps) : 0);
</script>

{#if cur !== undefined}
  <div class="t">
    <div class="row">
      <strong class="big num">{Math.round(cur)}°C</strong>
      <span class="faint small num">peak {Math.round(peak)}°C · {live.temps.length > 1 ? '2 h' : 'now'}</span>
    </div>
    <div class="chart">
      {#if live.temps.length > 1}
        <Sparkline values={live.temps} label="CPU temperature over the last two hours, {Math.round(cur)} degrees now" tone={peak > 70 ? 'warn' : 'accent'} />
      {:else}
        <p class="faint small">The chart fills in as the agent reports.</p>
      {/if}
    </div>
  </div>
{:else}
  <p class="muted">No temperature reading yet.</p>
{/if}

<style>
  .t { height: 100%; display: flex; flex-direction: column; gap: 6px; }
  .row { display: flex; justify-content: space-between; align-items: baseline; }
  .big { font-size: 1.5rem; letter-spacing: -0.02em; }
  .small { font-size: 0.78rem; }
  .chart { flex: 1; min-height: 30px; }
</style>
