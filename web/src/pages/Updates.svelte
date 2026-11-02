<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import Button from '../lib/components/Button.svelte';
  import JobList from '../lib/store/JobList.svelte';
  import { mode } from '../lib/api.svelte';
  import { power } from '../lib/state/power.svelte';
  import { confirmUpdate } from '../lib/state/actions';
  import { checkUpdates, findApp, loadCatalogue, loadUpdates, runningJob, store } from '../lib/state/store.svelte';
  import { fmtAgo, fmtDayTime } from '../lib/time.svelte';
  import { RefreshCw, Undo2 } from '../lib/icons';

  let checking = $state(false);
  onMount(() => {
    void loadCatalogue();
    void loadUpdates();
  });

  async function check() {
    checking = true;
    await checkUpdates();
    checking = false;
  }

  const rows = $derived(
    store.installed.map((a) => ({ a, u: store.updates.apps.find((u) => u.app === a.id && u.current === a.tag) })),
  );
  const waiting = $derived(rows.filter((r) => r.u?.latest).length);
  const updateJobs = $derived(store.jobs.filter((j) => j.action === 'update'));
  // Updates need the server awake; checking doesn't.
  const canAct = $derived(!mode.live || power.state === 'awake');
</script>

<div class="stack">
  {#if updateJobs.length}
    <Card title={updateJobs.some((j) => j.state === 'running') ? 'Updates in progress' : 'Recent updates'}><JobList /></Card>
  {/if}

  <Card title="App updates" subtitle={store.updates.checked ? `Checked ${fmtAgo(store.updates.checked)} (${fmtDayTime(store.updates.checked)})` : 'Not checked yet. Nothing is checked unless you ask.'}>
    {#snippet actions()}
      <Button size="sm" disabled={checking || !store.installed.length} onclick={check}>{#snippet icon()}<RefreshCw />{/snippet}{checking ? 'Checking…' : 'Check now'}</Button>
    {/snippet}
    {#if !store.installed.length}
      <p class="muted">No apps from the app store yet.</p>
    {:else}
      {#if store.updates.checked}
        <p class="summary">{waiting ? `${waiting} update${waiting === 1 ? '' : 's'} waiting.` : 'Everything is up to date.'}</p>
      {/if}
      <ul class="apps">
        {#each rows as { a, u } (a.id)}
          {@const cat = findApp(a.id)}
          <li>
            <div class="name">
              <strong>{a.name}</strong>
              <code class="faint">{a.tag}</code>
            </div>
            <div class="state">
              {#if runningJob(a.id)}
                <Badge tone="info">updating…</Badge>
              {:else if u?.error}
                <span class="err" title={u.error}>couldn't check</span>
              {:else if u?.latest}
                <Badge tone="warn">{u.latest} available</Badge>
                <Button size="sm" variant="primary" disabled={!canAct || !cat} onclick={() => cat && confirmUpdate(cat, a.tag, u.latest!, u.digest!)}>Update</Button>
              {:else if u}
                <span class="faint">up to date</span>
              {/if}
              {#if a.previousTag && a.previousDigest && cat && !runningJob(a.id)}
                <Button size="sm" variant="ghost" disabled={!canAct} onclick={() => confirmUpdate(cat, a.tag, a.previousTag!, a.previousDigest!, true)}>
                  {#snippet icon()}<Undo2 />{/snippet}Back to {a.previousTag}
                </Button>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
      {#if mode.live && power.state !== 'awake'}
        <p class="faint small">{mode.server} is asleep: checking works, updating needs it awake.</p>
      {/if}
    {/if}
  </Card>

  <Card title="Operating system" subtitle="Pending packages, checked and installed only when you ask.">
    <p class="muted">Coming next (Phase 6b).</p>
  </Card>

  <p class="faint note">
    Checks ask the registries from the gateway, so a sleeping server isn't woken. An update backs up the app's settings and data first, and if the
    new version doesn't answer, the old one and its settings come back by themselves. Only same-shaped release versions are offered: never betas, never :latest.
  </p>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .summary { margin-bottom: var(--space-2); }
  .apps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .apps li { display: flex; justify-content: space-between; align-items: center; gap: var(--space-3); padding: 10px 0; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .apps li:last-child { border-bottom: 0; }
  .name { display: flex; gap: 10px; align-items: baseline; flex-wrap: wrap; }
  .state { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .err { color: var(--bad); font-size: 0.85rem; }
  .small, .note { font-size: 0.8rem; }
</style>
