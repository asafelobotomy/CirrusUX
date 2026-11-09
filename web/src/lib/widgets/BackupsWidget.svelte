<script lang="ts">
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import { agentCan, live, needsAgent } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { confirmBackup } from '../state/actions';
  import { fmtAgo, fmtDay, fmtDayTime } from '../time.svelte';
  import { Archive, CircleCheck, Loader, Lock, TriangleAlert } from '../icons';
  import { fmtGb } from '../format';

  let { full = false }: { full?: boolean } = $props();
  const run = $derived(live.lastBackupRun);
  const next = $derived(live.nextBackup ?? live.nextWakes.find((w) => /backup/i.test(w.unit))?.at ?? null);
</script>

<div class="b">
  <div class="hero">
    {#if run}
      <span class="ic" class:running={run.running} class:failed={!run.ok && !run.running}>
        {#if run.running}<Loader size={22} />{:else if run.ok}<CircleCheck size={22} />{:else}<TriangleAlert size={22} />{/if}
      </span>
      <div>
        <strong>{run.running ? 'Backing up…' : run.ok ? 'Last backup OK' : 'Last backup failed'}</strong>
        <p class="muted small num">{fmtAgo(run.end)}{run.sizeGb ? ` · ${fmtGb(run.sizeGb, true)}` : ''}{live.backups[0]?.encrypted ? ' · encrypted' : ''}</p>
      </div>
    {:else if live.backups.length}
      <span class="ic failed"><TriangleAlert size={22} /></span>
      <div><strong>No backup run found</strong><p class="muted small">There are archives, but nothing in the backup unit's journal.</p></div>
    {:else}
      <span class="ic none"><Archive size={22} /></span>
      <div><strong>No backups yet</strong><p class="muted small">{next ? `The first one runs ${fmtDayTime(next)}, or press Run now.` : 'Press Run now for the first one.'}</p></div>
    {/if}
  </div>
  <div class="foot">
    <span class="faint small">{next ? `Next: ${fmtDayTime(next)}` : 'Next run unknown'}</span>
    <Button size="sm" disabled={power.state !== 'awake' || !!run?.running || !agentCan('backup_run')} title={needsAgent('backup_run')} onclick={() => confirmBackup()}>Run now</Button>
  </div>
  {#if full}
    <ul class="files">
      {#each live.backups as f}
        <li>
          <code>{f.file}</code>
          <span class="num muted">{fmtDay(f.at)}</span>
          <span class="num muted">{fmtGb(f.sizeGb, true)}</span>
          {#if f.encrypted}<Badge tone="ok">{#snippet icon()}<Lock />{/snippet}encrypted</Badge>{:else}<Badge tone="warn">not encrypted</Badge>{/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .b { height: 100%; display: flex; flex-direction: column; gap: var(--space-3); justify-content: space-between; }
  .hero { display: flex; gap: var(--space-3); align-items: center; }
  .ic { display: grid; place-items: center; width: 40px; height: 40px; border-radius: 12px; background: var(--ok-soft); color: var(--ok); flex: none; }
  .ic.failed { background: var(--bad-soft); color: var(--bad); }
  .ic.none { background: var(--surface-2); color: var(--text-muted); }
  .ic.running { background: color-mix(in oklab, var(--info) 16%, transparent); color: var(--info); }
  .ic.running :global(svg) { animation: spin 1.1s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .small { font-size: 0.82rem; }
  .foot { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
  .files { list-style: none; padding: 0; margin: var(--space-3) 0 0; display: flex; flex-direction: column; gap: 6px; }
  .files li { display: grid; grid-template-columns: 1fr auto auto auto; gap: var(--space-3); align-items: center; padding: var(--space-2) 0; border-top: 1px solid var(--border); font-size: 0.88rem; }
  .files code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  @media (max-width: 639px) { .files li { grid-template-columns: 1fr auto; } }
</style>
