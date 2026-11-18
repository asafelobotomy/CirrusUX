<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Badge from '../components/Badge.svelte';
  import Button from '../components/Button.svelte';
  import { mode } from '../api.svelte';
  import { agentCan, live } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { store } from '../state/store.svelte';
  import { checkRelease, loadRelease, release } from '../state/release.svelte';
  import { confirmSelfRollback, confirmSelfUpgrade, type Where } from '../state/actionsRelease';
  import { machineState, parseVersion, type Install } from '../release';
  import { fmtAgo, fmtDayTime } from '../time.svelte';
  import { ExternalLink, RefreshCw, TriangleAlert, Undo2 } from '../icons';

  const built = __CIRRUSUX_VERSION__;
  const ready = $derived(!mode.live || release.loaded);
  const serverJob = $derived(store.jobs.find((j) => j.app === 'cirrusux' && j.state === 'running'));
  const gatewayJob = $derived(release.gateway.job?.state === 'running' ? release.gateway.job : undefined);

  // While a job runs here the gateway may restart, so ask again soon.
  onMount(() => {
    void loadRelease();
    const t = setInterval(() => void loadRelease(), 5000);
    return () => clearInterval(t);
  });

  interface Row { where: Where; name: string; note?: string; inst: Install | null; busy?: string; blocked?: string }

  /** Why the server's buttons are off, if they are. */
  const serverBlocked = $derived.by(() => {
    if (!mode.live) return undefined;
    if (live.install && !agentCan('self_upgrade')) return `${mode.server} is too old to be upgraded from here: run sudo cirrusux-setup upgrade there once`;
    if (power.state !== 'awake') return `${mode.server} is asleep: wake it first`;
    return undefined;
  });
  const gatewayBlocked = $derived(
    mode.live && release.gateway.managed && !release.gateway.canUpgrade ? `${mode.gateway} has no upgrade helper yet: run sudo cirrusux-setup upgrade there once` : undefined,
  );
  const busyText = (action?: string) => (action === 'self-rollback' ? 'going back…' : action ? 'upgrading…' : undefined);

  const rows = $derived.by((): Row[] => {
    const server: Row = { where: 'server', name: mode.server, inst: live.install, busy: busyText(serverJob?.action), blocked: serverBlocked };
    if (mode.setup === 'single') return [{ ...server, note: 'runs the gateway too' }];
    const gateway: Row = { where: 'gateway', name: mode.gateway, note: 'the gateway', inst: release.gateway, busy: busyText(gatewayJob?.action), blocked: gatewayBlocked };
    return [gateway, { ...server, note: 'the server' }];
  });

  const subtitle = $derived(
    release.checked
      ? `Checked ${fmtAgo(release.checked)} (${fmtDayTime(release.checked)})${release.auto ? ', and once a day by itself' : ''}`
      : release.auto ? 'Not checked yet. The gateway checks once a day.' : 'Not checked yet. The daily check is off.',
  );
  // After the gateway's upgrade this page is still the old panel.
  const stale = $derived(mode.live && !!parseVersion(release.current) && !!parseVersion(built) && !built.startsWith(release.current));
  const lastGateway = $derived(release.gateway.job && release.gateway.job.state !== 'running' ? release.gateway.job : undefined);
</script>

<Card title="CirrusUX" {subtitle}>
  {#snippet actions()}
    <Button size="sm" disabled={release.checking} onclick={checkRelease}>{#snippet icon()}<RefreshCw />{/snippet}{release.checking ? 'Checking…' : 'Check now'}</Button>
  {/snippet}
  {#if stale}
    <p class="warn"><TriangleAlert size={15} />The gateway now runs {release.current}, and this page is still {built}. <button class="link" onclick={() => location.reload()}>Reload</button></p>
  {/if}
  {#if release.error}
    <p class="warn"><TriangleAlert size={15} />The last check failed: {release.error}</p>
  {/if}
  {#if ready}
    {#if release.latest}
      <p class="summary">
        The latest release is <strong>{release.latest}</strong>.
        {#if release.url}<a href={release.url} target="_blank" rel="noreferrer">Release notes <ExternalLink size={13} /></a>{/if}
      </p>
    {/if}
    <ul class="machines">
      {#each rows as r (r.where)}
        {@const st = machineState(r.inst, release.latest)}
        <li>
          <div class="name">
            <strong>{r.name}</strong>
            {#if r.inst?.current}<code class="faint">{r.inst.current}</code>{/if}
            {#if r.note}<span class="faint small">{r.note}</span>{/if}
          </div>
          <div class="state">
            {#if r.busy}
              <Badge tone="info">{r.busy}</Badge>
            {:else if st === 'no-report'}
              <span class="faint">no report yet</span>
            {:else if st === 'by-hand'}
              <span class="faint" title="Update the checkout, make build, then sudo cirrusux-setup apply">built from source: upgraded by hand</span>
            {:else}
              {#if st === 'offer'}
                <Badge tone="warn">{release.latest} available</Badge>
                <Button size="sm" variant="primary" disabled={!!r.blocked} title={r.blocked} onclick={() => confirmSelfUpgrade(r.where, release.latest)}>Upgrade</Button>
              {:else if release.latest}
                <span class="faint">up to date</span>
              {/if}
              {#if r.inst?.previous}
                <Button size="sm" variant="ghost" disabled={!!r.blocked} title={r.blocked} onclick={() => confirmSelfRollback(r.where)}>
                  {#snippet icon()}<Undo2 />{/snippet}Back to {r.inst.previous}
                </Button>
              {/if}
            {/if}
          </div>
        </li>
        {#if r.blocked && st !== 'by-hand' && st !== 'no-report'}<li class="why faint small">{r.blocked}.</li>{/if}
      {/each}
    </ul>
    {#if lastGateway}
      <p class="small" class:err={lastGateway.state === 'failed'}>
        {mode.gateway}, {fmtAgo(lastGateway.started)}: {lastGateway.step}{#if lastGateway.error} ({lastGateway.error}){/if}
      </p>
    {/if}
  {/if}
  <p class="faint small note">
    The gateway asks GitHub which release is the latest{release.auto ? ' once a day' : ' only when you press Check'}; CIRRUS_UPDATE_CHECK in gateway.env switches the daily check off or on.
    Nothing is installed until you confirm it here. An upgrade keeps the release before it, so you can go back.
  </p>
</Card>

<style>
  .summary { margin-bottom: var(--space-2); }
  .summary a { margin-left: 6px; font-size: 0.86rem; white-space: nowrap; }
  .machines { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .machines li { display: flex; justify-content: space-between; align-items: center; gap: var(--space-3); padding: 10px 0; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .machines li:last-child, .machines li:has(+ .why) { border-bottom: 0; }
  .machines li.why { padding: 0 0 10px; border-bottom: 1px solid var(--border); }
  .machines li.why:last-child { border-bottom: 0; }
  .name { display: flex; gap: 10px; align-items: baseline; flex-wrap: wrap; }
  .state { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .small { font-size: 0.8rem; }
  .note { margin-top: var(--space-3); }
  .err { color: var(--bad); }
  .warn { display: flex; gap: 6px; align-items: flex-start; color: var(--warn); font-size: 0.88rem; margin-bottom: var(--space-2); }
  .link { background: none; border: 0; padding: 0; color: var(--accent); cursor: pointer; font: inherit; text-decoration: underline; }
</style>
