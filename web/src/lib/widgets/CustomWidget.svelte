<script lang="ts">
  // A widget you put together: a title and up to six values from a fixed
  // list (lib/metrics.ts), as tiles or a list. Its settings live in the
  // board like any widget's, and are edited here while "Edit layout" is on.
  import Segmented from '../components/Segmented.svelte';
  import { ui } from '../state/ui.svelte';
  import { metric, usableMetrics } from '../metrics';

  let { config }: { config?: Record<string, string> } = $props();
  const MAX = 6;
  const chosen = $derived((config?.metrics ?? '').split(',').filter((id) => metric(id)));
  const readings = $derived(
    chosen
      .map((id) => ({ m: metric(id)!, r: metric(id)!.available?.() === false ? null : metric(id)!.read() }))
      .filter((x) => x.r),
  );

  function toggle(id: string) {
    if (!config) return;
    const next = chosen.includes(id) ? chosen.filter((x) => x !== id) : [...chosen, id].slice(0, MAX);
    config.metrics = next.join(',');
  }
</script>

{#if ui.editLayout && config}
  <div class="edit">
    <label class="title">Title <input bind:value={config.title} placeholder="My widget" /></label>
    <Segmented label="Style" bind:value={config.style} options={[{ value: 'tiles', label: 'Tiles' }, { value: 'list', label: 'List' }]} />
    <fieldset>
      <legend>Show (up to {MAX})</legend>
      {#each usableMetrics() as m}
        <label class="pick"><input type="checkbox" checked={chosen.includes(m.id)} disabled={!chosen.includes(m.id) && chosen.length >= MAX} onchange={() => toggle(m.id)} />{m.label}</label>
      {/each}
    </fieldset>
  </div>
{:else if !readings.length}
  <p class="muted">Nothing chosen yet. Switch on Edit layout to pick what this widget shows.</p>
{:else if config?.style === 'list'}
  <ul class="list">
    {#each readings as { m, r }}<li><span class="muted">{m.label}</span><strong class="num" class:warn={r!.warn}>{r!.text}</strong></li>{/each}
  </ul>
{:else}
  <ul class="tiles">
    {#each readings as { m, r }}<li class:warn={r!.warn}><span class="l">{m.label}</span><strong class="num">{r!.text}</strong></li>{/each}
  </ul>
{/if}

<style>
  .edit { height: 100%; overflow: auto; display: flex; flex-direction: column; gap: 10px; font-size: 0.86rem; }
  .title { display: flex; gap: 8px; align-items: center; }
  .title input {
    flex: 1; height: 30px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong);
    background: var(--surface-2); color: var(--text); font: inherit;
  }
  fieldset { border: 0; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 4px 12px; }
  legend { color: var(--text-faint); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 4px; }
  .pick { display: flex; gap: 6px; align-items: center; }
  .tiles { list-style: none; margin: 0; padding: 0; height: 100%; display: grid; grid-template-columns: repeat(auto-fit, minmax(110px, 1fr)); gap: var(--space-2); align-content: start; }
  .tiles li { display: flex; flex-direction: column; gap: 2px; padding: 10px 12px; border-radius: var(--radius); background: color-mix(in oklab, var(--surface-2) 80%, transparent); border: 1px solid var(--border); }
  .tiles li.warn { border-color: color-mix(in oklab, var(--warn) 55%, var(--border)); }
  .tiles .l { font-size: 0.74rem; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600; }
  .tiles strong { font-size: 1.3rem; font-weight: 650; letter-spacing: -0.02em; }
  .warn strong, strong.warn { color: var(--warn); }
  .list { list-style: none; margin: 0; padding: 0; height: 100%; overflow: auto; display: flex; flex-direction: column; }
  .list li { display: flex; justify-content: space-between; gap: 8px; padding: 7px 0; border-bottom: 1px solid var(--border); font-size: 0.9rem; }
  .list li:last-child { border-bottom: 0; }
</style>
