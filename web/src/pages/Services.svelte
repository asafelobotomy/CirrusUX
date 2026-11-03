<script lang="ts">
  import { mode } from '../lib/api.svelte';
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import ContainersWidget from '../lib/widgets/ContainersWidget.svelte';
  import AppsWidget from '../lib/widgets/AppsWidget.svelte';
  import { live } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { fmtTime } from '../lib/time.svelte';

  const running = $derived(live.containers.filter((c) => c.health !== 'exited').length);
  const memMb = $derived(live.containers.reduce((n, c) => n + c.memMb, 0));
  const unpinned = $derived(live.containers.filter((c) => c.tag === 'latest').length);
</script>

<div class="stack">
  <div class="stats">
    <div class="stat"><span class="faint">Running</span><strong class="num">{running}<small>/{live.containers.length}</small></strong></div>
    {#if memMb}<div class="stat"><span class="faint">Memory used</span><strong class="num">{(memMb / 1024).toFixed(1)}<small> GB</small></strong></div>{/if}
    <div class="stat">
      <span class="faint">Images</span>
      <strong>{unpinned ? `${unpinned} on :latest` : 'All pinned'}</strong>
      {#if unpinned}<Badge tone="warn">pin a version</Badge>{/if}
    </div>
  </div>
  <Card title="Apps" subtitle={`Opens on ${mode.serverHost}.`}>
    <div class="apps"><AppsWidget /></div>
  </Card>
  <Card title="Containers" subtitle={power.state === 'awake' ? 'Live from the agent' : `As of ${fmtTime(power.lastSnapshot)} · actions need the server awake`}>
    <ContainersWidget full />
  </Card>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: var(--space-4); }
  .stat { display: flex; flex-direction: column; gap: 4px; padding: var(--space-4) var(--space-5); background: var(--card-bg, var(--surface)); border: 1px solid var(--border); border-radius: var(--radius-lg); align-items: flex-start; }
  .stat strong { font-size: 1.6rem; letter-spacing: -0.02em; }
  .stat small { font-size: 0.9rem; color: var(--text-muted); font-weight: 500; }
  .apps { height: auto; }
</style>
