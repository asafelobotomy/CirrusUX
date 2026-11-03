<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import JobList from '../lib/store/JobList.svelte';
  import AppPage from '../lib/store/AppPage.svelte';
  import Sources from '../lib/store/Sources.svelte';
  import Button from '../lib/components/Button.svelte';
  import * as Icons from '../lib/icons';
  import { mode } from '../lib/api.svelte';
  import { clock, fmtAgo, fmtDayTime } from '../lib/time.svelte';
  import { appKey, BUILT_IN, sourceLabel } from '../lib/catalogue';
  import { installedApp, isInstalled, loadCatalogue, runningJob, store } from '../lib/state/store.svelte';
  import { checkStore, loadSources, openApp } from '../lib/state/cirrusstore.svelte';

  let q = $state('');
  let group = $state('All');

  let checking = $state(false);
  onMount(() => {
    void loadCatalogue();
    void loadSources();
  });

  async function check() {
    checking = true;
    await checkStore();
    checking = false;
  }

  const groups = $derived(['All', ...new Set(store.catalogue.map((a) => a.group))]);
  const shown = $derived(
    store.catalogue.filter(
      (a) => (group === 'All' || a.group === group) && (!q.trim() || `${a.name} ${a.description}`.toLowerCase().includes(q.trim().toLowerCase())),
    ),
  );
  const recentJobs = $derived(store.jobs.filter((j) => j.state === 'running' || (j.finished && clock.now - j.finished.getTime() < 3600_000)));
  const icon = (name: string) => Icons[name as keyof typeof Icons] ?? Icons.Package;
</script>

<div class="stack">
  {#if recentJobs.length}
    <Card title="Jobs" subtitle="Installs and removals run on {mode.server}; this page follows them.">
      <JobList />
    </Card>
  {/if}

  <Card title="Installed" subtitle={store.host ? `${store.host.runtime === 'podman' ? 'Podman' : 'Docker'} · apps in ${store.host.appsDir} · media from ${store.host.mediaRoots.join(', ')}` : `${mode.server} hasn't reported its CirrusStore settings yet.`}>
    {#if store.installed.length}
      <ul class="installed">
        {#each store.installed as a (a.id)}
          {@const cat = installedApp(a.id)}
          {@const Icon = icon(cat?.icon ?? 'Package')}
          <li>
            <button class="row" onclick={() => openApp(a.id, a.source || undefined)}>
              <span class="mini" style="--tile: {cat?.color ?? '#5b8def'}"><Icon size={16} /></span>
              <span class="name">{a.name}</span>
              <code class="faint">{a.tag}</code>
              {#if a.source}<Badge tone="neutral">{sourceLabel(a.source)}</Badge>{/if}
              <span class="faint mono">:{a.mainPort}</span>
              {#if runningJob(a.id)}<Badge tone="info">working…</Badge>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">Nothing installed from CirrusStore yet.</p>
    {/if}
  </Card>

  <Card
    title="Apps"
    subtitle={store.sources.fetched
      ? `CirrusStore checked ${fmtAgo(store.sources.fetched)} (${fmtDayTime(store.sources.fetched)})`
      : 'The built-in apps. Check CirrusStore for new apps and versions; nothing is fetched unless you ask.'}
  >
    {#snippet actions()}
      <Button size="sm" disabled={checking} onclick={check}>{#snippet icon()}<Icons.RefreshCw />{/snippet}{checking ? 'Checking…' : 'Check CirrusStore'}</Button>
      <label class="search"><Icons.Search size={15} /><input bind:value={q} placeholder="Search apps" aria-label="Search apps" /></label>
    {/snippet}
    <div class="groups" role="group" aria-label="Group">
      {#each groups as g}<button class:on={group === g} onclick={() => (group = g)}>{g}</button>{/each}
    </div>
    {#if !store.catalogue.length}
      <p class="muted">Loading the catalogue…</p>
    {:else if !shown.length}
      <p class="muted">No app matches “{q}”.</p>
    {/if}
    <ul class="grid">
      {#each shown as a (appKey(a))}
        {@const Icon = icon(a.icon)}
        <li>
          <button class="card" class:off={!!a.unavailable} onclick={() => openApp(a.id, a.source)} title={a.unavailable}>
            <span class="tile" style="--tile: {a.color}"><Icon size={22} /></span>
            <span class="body">
              <span class="title">
                {a.name}
                {#if a.unavailable}<Badge tone="warn">unavailable</Badge>{:else if isInstalled(a.id)}<Badge tone="ok">installed</Badge>{/if}
              </span>
              <span class="muted d">{a.unavailable ?? a.description}</span>
              <span class="faint meta">{a.group} · <code>{a.tag}</code>{#if a.source && a.source !== BUILT_IN} · <span class="src">{sourceLabel(a.source)}</span>{/if}</span>
            </span>
          </button>
        </li>
      {/each}
    </ul>
  </Card>
  <Sources />
  <p class="faint note">
    Every app is pinned to an exact version. Apps from CirrusStore and other sources are signed, and {mode.server} checks the signature and the definition
    again before anything runs. Removing an app archives its folder, so its settings and data are kept; only its downloaded image goes.
  </p>
</div>

<AppPage />

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .installed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .row {
    width: 100%; display: flex; align-items: center; gap: var(--space-3); padding: 8px 4px; border: 0; border-bottom: 1px solid var(--border);
    background: none; color: var(--text); text-align: left; border-radius: 0; font: inherit;
  }
  .row:hover { background: var(--hover); }
  .installed li:last-child .row { border-bottom: 0; }
  .name { flex: 1; font-weight: 550; }
  .mini, .tile {
    flex: none; display: grid; place-items: center; color: white; border-radius: calc(var(--radius) * 0.8);
    background: linear-gradient(145deg, color-mix(in oklab, var(--tile) 85%, white), var(--tile) 55%, color-mix(in oklab, var(--tile) 70%, black));
  }
  .mini { width: 28px; height: 28px; }
  .tile { width: 46px; height: 46px; }
  .search { display: inline-flex; align-items: center; gap: 6px; padding: 0 10px; height: 32px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); }
  .search input { border: 0; background: none; outline: none; color: var(--text); font: inherit; width: 150px; }
  .groups { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: var(--space-3); }
  .groups button { padding: 4px 12px; border-radius: 999px; border: 1px solid var(--border); background: transparent; color: var(--text-muted); font: inherit; font-size: 0.85rem; }
  .groups button.on { background: var(--accent-soft); border-color: var(--accent); color: var(--text); }
  .grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: var(--space-3); }
  .card {
    width: 100%; height: 100%; display: flex; gap: var(--space-3); align-items: flex-start; text-align: left; padding: var(--space-3);
    border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface-2); color: var(--text); font: inherit;
    transition: border-color var(--dur-1) var(--ease), transform var(--dur-1) var(--ease);
  }
  .card:hover { border-color: var(--border-strong); }
  .card:active { transform: scale(0.99); }
  .card.off { opacity: 0.6; }
  .src { color: var(--accent); }
  .body { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .title { display: flex; gap: 6px; align-items: center; font-weight: 600; }
  .d { font-size: 0.86rem; line-height: 1.35; }
  .meta { font-size: 0.78rem; }
  .note { font-size: 0.8rem; }
</style>
