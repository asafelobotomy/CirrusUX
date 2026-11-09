<script lang="ts">
  // Folder sizes, measured only when asked (it reads every file), and the
  // archive: removed apps, backups taken before updates, system backups.
  // Deleting from it is the only deletion in CirrusUX, in two steps: this
  // page's review of exactly what goes, then typing its name.
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import Dialog from '../lib/components/Dialog.svelte';
  import Meter from '../lib/components/Meter.svelte';
  import JobList from '../lib/store/JobList.svelte';
  import { mode } from '../lib/api.svelte';
  import { agentCan, needsAgent } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { store, type ArchiveEntry } from '../lib/state/store.svelte';
  import { confirmArchiveDelete, measureStorage } from '../lib/state/actions';
  import { fmtAgo, fmtDay, fmtDayTime } from '../lib/time.svelte';
  import { Archive, HardDrive, RefreshCw, Trash2 } from '../lib/icons';
  import { fmtBytes } from '../lib/format';
  const size = (b: number) => fmtBytes(b, true);

  const s = $derived(store.storage);
  const jobs = $derived(store.jobs.filter((j) => j.app === 'storage'));
  const busy = $derived(jobs.some((j) => j.state === 'running'));
  const canAct = $derived(!mode.live || power.state === 'awake');
  const archiveTotal = $derived(s?.archive.reduce((n, e) => n + e.bytes, 0) ?? 0);

  function what(e: ArchiveEntry): string {
    if (e.kind === 'pre-update') return `${e.app}'s settings before its update from ${e.tag}`;
    if (e.kind === 'removed-app') return `${e.app}'s folder, kept when it was removed`;
    return 'a system backup';
  }

  let reviewing = $state<ArchiveEntry | null>(null);
  let reviewOpen = $state(false);
  $effect(() => {
    reviewOpen = !!reviewing;
  });
  $effect(() => {
    if (!reviewOpen) reviewing = null;
  });
  const kindLabel = { 'removed-app': 'removed app', 'pre-update': 'before an update', backup: 'system backup' };
  const where = (e: ArchiveEntry) => (e.kind === 'backup' ? 'the backups folder' : 'the archive folder');
</script>

<div class="stack">
  {#if jobs.length}
    <Card title={busy ? 'Working' : 'Recent'}><JobList /></Card>
  {/if}

  <Card title="Folders" subtitle={s ? `Measured ${fmtAgo(s.measured)} (${fmtDayTime(s.measured)})` : 'Not measured yet. Measuring reads every file, so it runs only when you ask.'}>
    {#snippet actions()}
      <Button size="sm" disabled={!canAct || busy || !agentCan('storage_scan')} title={needsAgent('storage_scan')} onclick={measureStorage}>{#snippet icon()}<RefreshCw />{/snippet}{busy ? 'Measuring…' : 'Measure now'}</Button>
    {/snippet}
    {#if s}
      <div class="roots">
        {#each s.roots as r (r.path)}
          {@const max = Math.max(1, ...r.children.map((c) => c.bytes))}
          <section>
            <header>
              <span class="ic"><HardDrive size={16} /></span>
              <strong>{r.label}</strong>
              <code class="faint">{r.path}</code>
              <span class="num total">{size(r.bytes)}</span>
            </header>
            {#if r.diskSize}
              <Meter value={r.diskSize - r.diskFree} max={r.diskSize} label="Disk used" tone={(r.diskSize - r.diskFree) / r.diskSize > 0.85 ? 'warn' : 'accent'} />
              <p class="faint small">{size(r.diskFree)} free of {size(r.diskSize)} on this disk</p>
            {/if}
            <ul class="kids">
              {#each r.children as c}
                <li><span class="n">{c.name}</span><span class="bar"><span style="width: {(c.bytes / max) * 100}%"></span></span><span class="v num muted">{size(c.bytes)}</span></li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>
    {:else if mode.live && !canAct}
      <p class="muted">{mode.server} is asleep. Wake it to measure.</p>
    {/if}
  </Card>

  <Card title="Archive" subtitle={s ? `${s.archive.length} item${s.archive.length === 1 ? '' : 's'}, ${size(archiveTotal)}. Nothing here goes unless you delete it.` : 'Measure first to list the archive.'}>
    {#if s?.archive.length}
      <ul class="archive">
        {#each s.archive as e (e.name)}
          <li>
            <span class="ic"><Archive size={16} /></span>
            <div class="txt">
              <span class="t">{what(e)}</span>
              <code class="faint mono small">{e.name}</code>
            </div>
            <Badge tone="neutral">{kindLabel[e.kind]}</Badge>
            <span class="num muted">{size(e.bytes)}</span>
            <span class="faint small">{fmtDay(e.modified)}</span>
            <Button size="sm" variant="ghost" disabled={!canAct || busy || !agentCan('archive_delete')} title={needsAgent('archive_delete')} aria-label="Delete {what(e)} ({e.name})…" onclick={() => (reviewing = e)}>{#snippet icon()}<Trash2 />{/snippet}Delete…</Button>
          </li>
        {/each}
      </ul>
    {:else if s}
      <p class="muted">The archive is empty.</p>
    {/if}
  </Card>
  <p class="faint note">System backups beyond the number to keep (BACKUP_KEEP) are the only thing CirrusUX removes by itself. Everything else waits here for you.</p>
</div>

<Dialog bind:open={reviewOpen} label="Review what goes">
  {#if reviewing}
    <h2>Delete {what(reviewing)}?</h2>
    <p class="muted step">Step 1 of 2: check this is what you mean.</p>
    <dl class="review">
      <dt>Name</dt><dd class="mono">{reviewing.name}</dd>
      <dt>Where</dt><dd>{where(reviewing)}</dd>
      <dt>Size</dt><dd class="num">{size(reviewing.bytes)}</dd>
      <dt>From</dt><dd>{fmtDayTime(reviewing.modified)}</dd>
    </dl>
    <p class="warn">Once deleted, it's gone for good.{reviewing.kind === 'pre-update' ? ` You couldn't go back to ${reviewing.tag} with these settings any more.` : ''}</p>
    <div class="row">
      <Button variant="ghost" onclick={() => (reviewOpen = false)}>Keep it</Button>
      <Button variant="danger" onclick={() => { const e = reviewing!; reviewOpen = false; confirmArchiveDelete(e, what(e)); }}>Continue</Button>
    </div>
  {/if}
</Dialog>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .roots { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: var(--space-5); }
  header { display: flex; gap: 8px; align-items: baseline; margin-bottom: 8px; flex-wrap: wrap; }
  .ic { color: var(--text-muted); display: inline-flex; align-self: center; }
  .total { margin-left: auto; font-weight: 600; }
  .small { font-size: 0.78rem; }
  .kids { list-style: none; margin: 10px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; font-size: 0.86rem; }
  .kids li { display: grid; grid-template-columns: minmax(0, 9rem) 1fr auto; gap: 10px; align-items: center; }
  .n { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .bar { height: 6px; border-radius: 999px; background: var(--surface-3); overflow: hidden; }
  .bar span { display: block; height: 100%; background: var(--accent); border-radius: inherit; }
  .archive { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .archive li { display: flex; gap: var(--space-3); align-items: center; padding: 10px 0; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .archive li:last-child { border-bottom: 0; }
  .txt { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: 2px; }
  .txt code { overflow-wrap: anywhere; }
  .note { font-size: 0.8rem; }
  h2 { font-size: 1.2rem; }
  .step { margin-top: 4px; font-size: 0.88rem; }
  .review { display: grid; grid-template-columns: auto 1fr; gap: 6px 14px; margin: var(--space-4) 0; font-size: 0.9rem; }
  .review dt { color: var(--text-faint); }
  .review dd { margin: 0; overflow-wrap: anywhere; }
  .warn { color: var(--warn); font-size: 0.9rem; }
  .row { display: flex; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
</style>
