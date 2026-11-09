<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import Button from '../lib/components/Button.svelte';
  import { live } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { sendAction } from '../lib/state/actions';
  import { has, mode } from '../lib/api.svelte';
  import { security as sample } from '../lib/demo/data';
  import { fmtAgo, fmtDayTime } from '../lib/time.svelte';
  import { fmtNum } from '../lib/format';
  import { Lock, ShieldCheck, Activity, RefreshCw } from '../lib/icons';

  const sec = $derived(live.security);
  let report = $state<string[] | null>(null);
  let running = $state(false);

  async function runReport() {
    if (!mode.live) {
      report = sample.egressTop.map((e) => `${Math.round(e.share * 100)}%  ${e.dest}`);
      return;
    }
    running = true;
    const c = await sendAction({ action: 'egress_report' }, 'Egress report', true);
    running = false;
    report = c?.state === 'done' ? (c.output ?? []) : [`Couldn't get the report: ${c?.error ?? `no answer from ${mode.server}`}`];
  }
</script>

<div class="stack">
  {#if sec}
    <div class="grid">
      <Card>
        <div class="stat">
          <span class="ic"><ShieldCheck size={20} /></span>
          <div>
            <h3>Virus scan</h3>
            {#if sec.clamav}
              <p class="muted">ClamAV, {fmtAgo(sec.clamav.at)} · {fmtNum(sec.clamav.files)} files</p>
              <Badge tone={sec.clamav.infected ? 'bad' : 'ok'}>{sec.clamav.infected} infected</Badge>
            {:else}
              <p class="muted">No scan summary found.</p>
            {/if}
          </div>
        </div>
      </Card>
      {#if sec.firewall.length}
        <Card>
          <div class="stat">
            <span class="ic"><Lock size={20} /></span>
            <div>
              <h3>Firewall rules</h3>
              <ul class="checks">
                {#each sec.firewall as f (f.chain)}
                  <li><code>{f.chain}</code><Badge tone={f.ok ? 'ok' : 'bad'}>{f.ok ? 'intact' : 'missing'}</Badge></li>
                {/each}
              </ul>
            </div>
          </div>
        </Card>
      {/if}
      {#if sec.banned !== null}
        <Card>
          <div class="stat">
            <span class="ic"><Activity size={20} /></span>
            <div>
              <h3>fail2ban</h3>
              <Badge tone={sec.banned ? 'warn' : 'neutral'}>{sec.banned} banned now</Badge>
            </div>
          </div>
        </Card>
      {/if}
    </div>
  {:else}
    <Card><p class="muted">No security data from {mode.server} yet.</p></Card>
  {/if}

  {#if has('egress')}
  <Card title="Outbound traffic" subtitle={`Where ${mode.server} connects to, from the egress report. Runs on demand: it can take a while.`}>
    {#snippet actions()}
      <Button size="sm" disabled={running || (mode.live && power.state !== 'awake')} onclick={runReport}>
        {#snippet icon()}<RefreshCw />{/snippet}{running ? 'Reading logs…' : report ? 'Run again' : 'Run report'}
      </Button>
    {/snippet}
    {#if report}
      <pre>{report.join('\n') || '(empty report)'}</pre>
    {:else if mode.live && power.state !== 'awake'}
      <p class="muted">{mode.server} is asleep. Wake it to run the report.</p>
    {:else}
      <p class="muted">Not run yet.</p>
    {/if}
  </Card>
  {/if}

  <p class="faint note">Reads only: nothing here changes firewall rules. Last data {fmtDayTime(power.lastSnapshot)}.</p>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: var(--space-4); }
  .stat { display: flex; gap: var(--space-3); align-items: flex-start; }
  .stat h3 { font-size: 1rem; margin-bottom: 4px; }
  .stat p { font-size: 0.86rem; margin-bottom: 8px; }
  .ic { display: grid; place-items: center; width: 40px; height: 40px; border-radius: 12px; background: var(--accent-soft); color: var(--accent); flex: none; }
  pre { margin: 0; padding: var(--space-3); border-radius: var(--radius-sm); background: var(--surface-2); border: 1px solid var(--border); overflow-x: auto; font-size: 0.82rem; max-height: 420px; }
  .note { font-size: 0.8rem; }
  .checks { list-style: none; margin: 6px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .checks li { display: flex; align-items: center; gap: 8px; justify-content: space-between; }
</style>
