<script lang="ts">
  import { mode } from '../api.svelte';
  import { store } from '../state/store.svelte';
  import Meter from '../components/Meter.svelte';
  import Badge from '../components/Badge.svelte';
  import { live } from '../state/live.svelte';
  import { TriangleAlert } from '../icons';

  const fmt = (gb: number) => (gb >= 1000 ? `${(gb / 1000).toFixed(1)} TB` : gb >= 1 ? `${Math.round(gb)} GB` : `${Math.round(gb * 1000)} MB`);
  const disk = $derived(live.disk);
  // The last measurement's biggest folders (Storage page), media first.
  const folders = $derived.by(() => {
    const roots = store.storage?.roots ?? [];
    const root = roots.find((x) => x.label === 'Media') ?? roots[0];
    return root ? root.children.map((c) => ({ name: c.name, gb: c.bytes / 1e9 })) : mode.live ? [] : live.folders;
  });
  const max = $derived(Math.max(1, ...folders.map((f) => f.gb)));
</script>

{#if disk}
  <div class="st">
    <div class="top">
      <div>
        <strong class="big num">{fmt(disk.totalGb - disk.usedGb)}</strong>
        <span class="muted"> free of {fmt(disk.totalGb)}</span>
      </div>
      {#if disk.linkMbps && disk.linkMbps < 5000}
        <Badge tone="warn">{#snippet icon()}<TriangleAlert />{/snippet}USB {disk.linkMbps <= 480 ? '2' : '3'} · {disk.linkMbps} Mb/s</Badge>
      {/if}
    </div>
    <Meter value={disk.usedGb} max={disk.totalGb} label="Disk used" tone={disk.usedGb / disk.totalGb > 0.85 ? 'warn' : 'accent'} />
    {#if folders.length}
      <ul class="folders">
        {#each folders as f}
          <li>
            <span class="n">{f.name}</span>
            <span class="bar"><span style="width: {(f.gb / max) * 100}%"></span></span>
            <span class="v num muted">{fmt(f.gb)}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="faint small">Folder sizes come from the Storage page's “Measure now”: measuring reads every file, so it runs only when you ask.</p>
    {/if}
  </div>
{:else}
  <p class="muted">No disk data from {mode.server} yet.</p>
{/if}

<style>
  .st { height: 100%; display: flex; flex-direction: column; gap: var(--space-3); min-height: 0; }
  .top { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
  .big { font-size: 1.5rem; letter-spacing: -0.02em; }
  .small { font-size: 0.8rem; }
  .folders { list-style: none; margin: 0; padding: 0; overflow: auto; display: flex; flex-direction: column; gap: 7px; min-height: 0; }
  li { display: grid; grid-template-columns: 7.5em 1fr 4.5em; align-items: center; gap: var(--space-2); font-size: 0.84rem; }
  .n { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .bar { height: 6px; background: var(--surface-3); border-radius: 999px; overflow: hidden; }
  .bar span { display: block; height: 100%; background: var(--accent-line); border-radius: inherit; }
  .v { text-align: right; }
</style>
