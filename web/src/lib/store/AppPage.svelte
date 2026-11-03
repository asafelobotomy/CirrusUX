<script lang="ts">
  // One app's page: what it can reach, the owner's choices, the exact file
  // an install writes (from the gateway, with the server's own settings),
  // then Install or Remove. Everything is checked again on the server.
  import { untrack } from 'svelte';
  import Dialog from '../components/Dialog.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import Toggle from '../components/Toggle.svelte';
  import JobList from './JobList.svelte';
  import * as Icons from '../icons';
  import { api, mode, sleeps } from '../api.svelte';
  import { appKey, BUILT_IN, cleanOptions, defaultOptions, sourceLabel, type AppOptions } from '../catalogue';
  import { findApp, isInstalled, runningJob, store } from '../state/store.svelte';
  import { power } from '../state/power.svelte';
  import { confirmInstall, confirmRemove } from '../state/actions';

  let open = $state(false);
  let options = $state<AppOptions>({ ports: {}, media: {}, env: {}, devices: {} });
  let preview = $state<{ file?: string; content?: string; error?: string; warnings?: string[] } | null>(null);
  let previewing = $state(false);

  const app = $derived(store.open ? findApp(store.open, store.openSource) : undefined);
  const installed = $derived(app ? isInstalled(app.id) : false);
  const job = $derived(app ? runningJob(app.id) : undefined);
  const appsDir = $derived(store.host?.appsDir ?? '/srv/cirrusux/apps');
  const awake = $derived(power.state === 'awake');
  const Icon = $derived(app ? (Icons[app.icon as keyof typeof Icons] ?? Icons.Package) : Icons.Package);

  $effect(() => {
    open = !!store.open;
  });
  $effect(() => {
    if (!open) store.open = store.openSource = null;
  });
  // A fresh page starts from the app's suggestions, before it's drawn. Only
  // a different app resets them: each poll brings a new store.host.
  let lastId = '';
  $effect.pre(() => {
    if (app && appKey(app) !== lastId) {
      lastId = appKey(app);
      options = defaultOptions(app, untrack(() => store.host?.mediaRoots[0]));
      preview = null;
    } else if (!app) lastId = '';
  });

  // Live, the gateway renders the file with the server's settings, a
  // moment after the last change.
  let timer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    if (!app || installed || app.unavailable || !mode.live) return;
    const body = JSON.stringify({ app: app.id, store: app.source || BUILT_IN, options: cleanOptions(app, $state.snapshot(options)) });
    clearTimeout(timer);
    previewing = true;
    timer = setTimeout(async () => {
      try {
        preview = await api('/api/apps/preview', { method: 'POST', body });
      } catch (e) {
        preview = { error: (e as Error).message };
      }
      previewing = false;
    }, 400);
  });

  const blocked = $derived.by(() => {
    if (!app) return 'No app';
    if (app.unavailable) return app.unavailable;
    if (job) return `${job.action === 'install' ? 'Installing' : 'Removing'}…`;
    if (mode.live && !awake) return `${mode.server} is asleep: wake it first`;
    if (mode.live && (previewing || !preview)) return 'Checking…';
    if (preview?.error) return 'Fix the choices above first';
    return '';
  });

  const kindLabel = { config: 'settings', cache: 'cache', data: 'data', media: '' };
  const openUrl = $derived.by(() => {
    const rec = store.installed.find((a) => a.id === app?.id);
    return rec ? `${rec.https ? 'https' : 'http'}://${mode.serverHost}:${rec.mainPort}/` : '';
  });
</script>

<Dialog bind:open label={app ? app.name : 'App'} wide>
  {#if app}
    <header class="head">
      <span class="tile" style="--tile: {app.color}"><Icon size={26} /></span>
      <div>
        <h2>{app.name}</h2>
        <p class="muted">
          {app.group} · <code>{app.tag}</code> · <a href={app.website} target="_blank" rel="noopener noreferrer">website <Icons.ExternalLink size={12} /></a>
          · <Badge tone={app.source && app.source !== BUILT_IN ? 'accent' : 'neutral'}>{sourceLabel(app.source)}</Badge>
        </p>
      </div>
      {#if installed}<Badge tone="ok">installed</Badge>{/if}
    </header>
    <p class="desc">{app.description}</p>
    {#if app.source && app.source !== BUILT_IN}
      <p class="faint small">Signed by {sourceLabel(app.source)}{app.serial ? ` (definition ${app.serial})` : ''}; {mode.server} checks the signature again before installing.</p>
    {/if}
    {#if app.notes}<p class="notes"><Icons.Info size={15} />{app.notes}</p>{/if}

    {#if job || store.jobs.some((j) => j.app === app.id)}
      <section><h3>Progress</h3><JobList app={app.id} /></section>
    {/if}

    <section>
      <h3>What it can reach</h3>
      <ul class="reach">
        {#each app.ports as p}
          <li>
            <span class="k"><Icons.Plug size={15} />Port <strong>{p.name}</strong>{#if p.https}<Badge tone="neutral">https</Badge>{/if}</span>
            {#if installed}
              <span class="v mono">{p.host} → {p.container}/{p.protocol ?? 'tcp'}</span>
            {:else}
              <span class="v"><input class="num" type="number" min="1024" max="65535" bind:value={options.ports![p.name]} aria-label="Host port for {p.name}" /> <span class="faint mono">→ {p.container}/{p.protocol ?? 'tcp'}</span></span>
            {/if}
          </li>
        {/each}
        {#each app.volumes as v}
          <li>
            {#if v.kind === 'media'}
              <span class="k"><Icons.Folder size={15} />{v.label ?? v.name}<Badge tone={v.writable ? 'warn' : 'ok'}>{v.writable ? 'can write' : 'read-only'}</Badge></span>
              {#if installed}
                <span class="v mono">→ {v.path}</span>
              {:else}
                <span class="v"><input class="path" bind:value={options.media![v.name]} spellcheck="false" aria-label="Folder for {v.label ?? v.name}" /> <span class="faint mono">→ {v.path}</span></span>
              {/if}
            {:else}
              <span class="k"><Icons.FolderLock size={15} />Its own {kindLabel[v.kind]}</span>
              <span class="v faint mono">{appsDir}/{app.id}/{v.name} → {v.path}</span>
            {/if}
          </li>
        {/each}
        {#each app.devices ?? [] as dv}
          <li>
            <span class="k"><Icons.Cpu size={15} />{dv.why}</span>
            {#if dv.optional && !installed}
              <span class="v"><Toggle bind:checked={options.devices![dv.path]} label={dv.path} hint={dv.hostGroups?.length ? `joins ${dv.hostGroups.join(', ')}` : undefined} /></span>
            {:else}
              <span class="v mono">{dv.path}</span>
            {/if}
          </li>
        {/each}
        {#each (app.env ?? []).filter((e) => e.choices) as e}
          <li>
            <span class="k"><Icons.Settings size={15} />{e.label ?? e.name}</span>
            {#if installed}
              <span class="v mono">{e.name}</span>
            {:else}
              <select class="v" bind:value={options.env![e.name]} aria-label={e.label ?? e.name}>
                {#each e.choices ?? [] as c}<option value={c}>{c}</option>{/each}
              </select>
            {/if}
          </li>
        {/each}
      </ul>
      <p class="faint small">
        Pinned to <code>{app.image}:{app.tag}</code>{app.runAsUser ? ', running as the apps user' : ''}{app.minMemoryMB ? `, needs about ${app.minMemoryMB >= 1024 ? app.minMemoryMB / 1024 + ' GB' : app.minMemoryMB + ' MB'} of memory` : ''}.
        Nothing else: no host network, no extra privileges.
      </p>
    </section>

    {#if !installed}
      <section>
        <h3>What it writes</h3>
        {#if !mode.live}
          <p class="faint small">Demo: the real panel shows the exact compose file or Quadlet unit here, made with the server's own settings.</p>
        {:else if preview?.error}
          <p class="err"><Icons.TriangleAlert size={15} />{preview.error}</p>
        {:else if preview?.file}
          {#each preview.warnings ?? [] as w}<p class="warn"><Icons.TriangleAlert size={15} />{w}</p>{/each}
          <details>
            <summary><code>{preview.file}</code></summary>
            <pre>{preview.content}</pre>
          </details>
        {:else}
          <p class="faint small">Checking with {mode.server}…</p>
        {/if}
      </section>
    {/if}

    <div class="row">
      {#if installed}
        {#if openUrl}<a class="open" href={openUrl} target="_blank" rel="noopener noreferrer">Open {app.name} <Icons.ExternalLink size={14} /></a>{/if}
        <Button variant="danger" disabled={!!job || (mode.live && !awake)} onclick={() => confirmRemove(app)}>{#snippet icon()}<Icons.Trash2 />{/snippet}Remove</Button>
      {:else}
        {#if blocked}<span class="faint small">{blocked}</span>{/if}
        <Button variant="primary" disabled={!!blocked} onclick={() => confirmInstall(app, $state.snapshot(options), preview?.file ? { file: preview.file, content: preview.content ?? '' } : null)}>
          {#snippet icon()}<Icons.Download />{/snippet}Install
        </Button>
      {/if}
    </div>
    {#if sleeps() && !installed}<p class="faint small right">Installing keeps {mode.server} awake until it's done.</p>{/if}
  {/if}
</Dialog>

<style>
  .head { display: flex; gap: var(--space-3); align-items: center; }
  .head > div { flex: 1; min-width: 0; }
  .head p { font-size: 0.85rem; display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
  .head a { display: inline-flex; gap: 3px; align-items: center; }
  .tile {
    flex: none; display: grid; place-items: center; width: 52px; height: 52px; border-radius: calc(var(--radius) * 0.9); color: white;
    background: linear-gradient(145deg, color-mix(in oklab, var(--tile) 85%, white), var(--tile) 55%, color-mix(in oklab, var(--tile) 70%, black));
  }
  h2 { font-size: 1.25rem; }
  h3 { font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); margin-bottom: var(--space-2); }
  .desc { margin-top: var(--space-3); }
  .notes { display: flex; gap: 8px; margin-top: var(--space-3); padding: 8px 10px; border-radius: var(--radius-sm); background: var(--info-soft, var(--surface-2)); font-size: 0.88rem; }
  .notes :global(svg) { flex: none; margin-top: 2px; color: var(--info); }
  section { margin-top: var(--space-5); }
  .reach { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .reach li { display: flex; justify-content: space-between; align-items: center; gap: var(--space-3); padding: 8px 0; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .k { display: inline-flex; align-items: center; gap: 8px; }
  .k :global(svg) { color: var(--text-muted); flex: none; }
  .v { display: inline-flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
  input, select {
    height: 32px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong);
    background: var(--surface-2); color: var(--text); font: inherit; font-size: 0.88rem;
  }
  input.num { width: 90px; }
  input.path { width: min(260px, 60vw); font-family: var(--font-mono, monospace); }
  .small { font-size: 0.8rem; }
  .err, .warn { display: flex; gap: 6px; align-items: flex-start; font-size: 0.88rem; }
  .err { color: var(--bad); }
  .warn { color: var(--warn); }
  details { border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  summary { padding: 8px 10px; cursor: pointer; }
  pre { margin: 0; padding: 10px; border-top: 1px solid var(--border); max-height: 260px; overflow: auto; font-size: 0.76rem; line-height: 1.45; }
  .row { display: flex; justify-content: flex-end; align-items: center; gap: var(--space-3); margin-top: var(--space-5); flex-wrap: wrap; }
  .open { display: inline-flex; gap: 4px; align-items: center; margin-right: auto; }
  .right { text-align: right; margin-top: var(--space-2); }
</style>
