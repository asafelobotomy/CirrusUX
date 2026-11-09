<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import Button from '../lib/components/Button.svelte';
  import JobList from '../lib/store/JobList.svelte';
  import { mode } from '../lib/api.svelte';
  import { agentCan, needsAgent } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { checkOS, confirmOSUpgrade, confirmUpdate } from '../lib/state/actions';
  import { has } from '../lib/api.svelte';
  import { checkUpdates, findApp, installedApp, loadCatalogue, loadUpdates, runningJob, store } from '../lib/state/store.svelte';
  import { needsMessage, sourceLabel } from '../lib/catalogue';
  import { fmtAgo, fmtDayTime } from '../lib/time.svelte';
  import { RefreshCw, TriangleAlert, Undo2 } from '../lib/icons';

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
  const updateJobs = $derived(store.jobs.filter((j) => j.action === 'update' || j.action.startsWith('os-')));
  const osJob = $derived(store.jobs.find((j) => j.app === 'os' && j.state === 'running'));
  const security = $derived(store.os?.packages.filter((p) => p.security).length ?? 0);
  // Updates need the server awake; checking doesn't.
  const canAct = $derived(!mode.live || power.state === 'awake');
  const canApp = $derived(canAct && agentCan('app_update'));
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
      <p class="muted">No apps from CirrusStore yet.</p>
    {:else}
      {#if store.updates.checked}
        <p class="summary">{waiting ? `${waiting} update${waiting === 1 ? '' : 's'} waiting.` : 'Everything is up to date.'}</p>
      {/if}
      <ul class="apps">
        {#each rows as { a, u } (a.id)}
          {@const cat = installedApp(a.id)}
          {@const next = u?.definition && u.source ? findApp(a.id, u.source) : undefined}
          <li>
            <div class="name">
              <strong>{a.name}</strong>
              <code class="faint">{a.tag}</code>
            </div>
            <div class="state">
              {#if runningJob(a.id)}
                <Badge tone="info">updating…</Badge>
              {:else if u?.needsVersion}
                <span title={needsMessage(u.needsVersion)}><Badge tone="warn">Update needs CirrusUX {u.needsVersion}</Badge></span>
              {:else if u?.error}
                <span class="err" title={u.error}>couldn't check</span>
              {:else if u?.definition && u.latest}
                <span title="A newer definition, signed by {sourceLabel(u.source)}: settings, ports or version">
                  <Badge tone="warn">{u.latest === a.tag ? 'new definition' : `${u.latest} available`} · {sourceLabel(u.source)}</Badge>
                </span>
                <Button size="sm" variant="primary" disabled={!canApp || !next} title={needsAgent('app_update')} onclick={() => next && confirmUpdate(next, a.tag, u.latest!, u.digest!, false, { source: u.source! })}>Update</Button>
              {:else if u?.latest}
                <Badge tone="warn">{u.latest} available</Badge>
                <Button size="sm" variant="primary" disabled={!canApp || !cat} title={needsAgent('app_update')} onclick={() => cat && confirmUpdate(cat, a.tag, u.latest!, u.digest!)}>Update</Button>
              {:else if u}
                <span class="faint">up to date</span>
              {/if}
              {#if a.previousTag && a.previousDigest && cat && !runningJob(a.id)}
                <Button size="sm" variant="ghost" disabled={!canApp} title={needsAgent('app_update')} onclick={() => confirmUpdate(cat, a.tag, a.previousTag!, a.previousDigest!, true)}>
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

  {#if has('os')}
    <Card title="Operating system" subtitle={store.os ? `Checked ${fmtAgo(store.os.checked)} (${fmtDayTime(store.os.checked)})` : 'Not checked yet. Nothing is checked or installed unless you ask.'}>
      {#snippet actions()}
        <Button size="sm" disabled={!canAct || !!osJob || !agentCan('os_check')} title={needsAgent('os_check')} onclick={checkOS}>{#snippet icon()}<RefreshCw />{/snippet}{osJob?.action === 'os-check' ? 'Checking…' : 'Check now'}</Button>
      {/snippet}
      {#if store.os}
        {#if store.os.upgraded}<p class="faint small">Last upgrade: {store.os.upgradedCount} package(s), {fmtAgo(store.os.upgraded)}.</p>{/if}
        {#if store.os.rebootRequired}<p class="warn"><TriangleAlert size={15} />A restart is needed to finish the last upgrade. Restart {mode.server} when it suits you.</p>{/if}
        {#if store.os.packages.length}
          <p class="summary">
            {store.os.packages.length} package update{store.os.packages.length === 1 ? '' : 's'} waiting{#if security}, <strong>{security} for security</strong>{/if}.
          </p>
          <details>
            <summary>Show the packages</summary>
            <ul class="pkgs">
              {#each store.os.packages as p}
                <li><span>{p.name}{#if p.security} <Badge tone="warn">security</Badge>{/if}</span><code class="faint">{p.from} → {p.to}</code></li>
              {/each}
            </ul>
          </details>
          <div class="row"><Button variant="primary" disabled={!canAct || !!osJob || !agentCan('os_upgrade')} title={needsAgent('os_upgrade')} onclick={() => confirmOSUpgrade(store.os!.packages.length)}>Install updates</Button></div>
        {:else}
          <p class="summary">The system is up to date.</p>
        {/if}
      {/if}
    </Card>
  {/if}

  <p class="faint note">
    Checks ask the registries from the gateway, so a sleeping server isn't woken. Newer app definitions appear here after Check CirrusStore on the CirrusStore page.
    An update backs up the app's settings and data first, and if the new version doesn't answer, the old one and its settings come back by themselves.
    Only same-shaped release versions are offered: never betas, never :latest.
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
  .warn { display: flex; gap: 6px; align-items: flex-start; color: var(--warn); font-size: 0.88rem; margin-bottom: var(--space-2); }
  details { margin: var(--space-2) 0; }
  summary { cursor: pointer; color: var(--text-muted); font-size: 0.88rem; }
  .pkgs { list-style: none; margin: var(--space-2) 0 0; padding: 0; display: flex; flex-direction: column; gap: 4px; font-size: 0.86rem; max-height: 260px; overflow: auto; }
  .pkgs li { display: flex; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
  .row { display: flex; justify-content: flex-end; }
</style>
