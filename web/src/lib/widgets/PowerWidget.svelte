<script lang="ts">
  import Button from '../components/Button.svelte';
  import { power, sleepsInMs, WAKE_STEPS } from '../state/power.svelte';
  import { confirmAllowSleep, confirmStayAwake, confirmWake } from '../state/actions';
  import { clock, fmtDayTime, fmtDuration, fmtTime, tzLabel } from '../time.svelte';
  import { live } from '../state/live.svelte';
  import { has, mode, sleeps } from '../api.svelte';
  import { Check, Coffee, Loader, Moon, MoonStar, Power, Sun, TriangleAlert, Wifi, Zap } from '../icons';

  const LABEL = $derived({ awake: sleeps() ? 'Awake' : 'Online', asleep: 'Asleep', waking: 'Waking…', unreachable: 'No reports' });

  const sinceMs = $derived(clock.now - power.since.getTime());
  const sleepsIn = $derived(sleepsInMs());
  const idleFrac = $derived(sleepsIn === null ? 0 : 1 - Math.min(1, sleepsIn / (live.idle.idleMin * 60_000)));
  const next = $derived(live.nextWakes[0] ?? null);
  const stay = $derived(power.stayAwakeUntil);
  const holding = $derived(stay === 'forever' || (stay instanceof Date && stay.getTime() > clock.now));
</script>

<div class="power {power.state}">
  <div class="glow" aria-hidden="true"></div>

  <div class="top">
    <div class="orb" aria-hidden="true">
      {#if power.state === 'awake'}<Sun size={28} />{:else if power.state === 'asleep'}<MoonStar size={28} />{:else if power.state === 'unreachable'}<Wifi size={28} />{:else}<span class="spin"><Loader size={28} /></span>{/if}
    </div>
    <div class="head">
      <span class="eyebrow">{mode.server}</span>
      <h2 class="state" aria-live="polite">
        {LABEL[power.state]}
      </h2>
      <span class="since muted num">
        {#if power.state === 'waking'}
          usually 20–30 s
        {:else}
          since {fmtTime(power.since)} {tzLabel()} · {fmtDuration(sinceMs)}
        {/if}
      </span>
    </div>
  </div>

  {#if power.notice}
    <p class="notice" role="status"><TriangleAlert size={15} />{power.notice}</p>
  {/if}

  {#if power.state === 'awake'}
    <div class="status">
      {#if power.reasons.length}
        <p class="line"><Zap size={16} /> Kept awake by {power.reasons.join(', ')}</p>
      {:else if holding}
        <p class="line"><Coffee size={16} /> Staying awake {stay === 'forever' ? 'until you allow sleep' : `until ${fmtTime(stay as Date)}`}</p>
      {:else if sleepsIn !== null}
        <div class="countdown">
          <div class="cd-row">
            <span><Moon size={15} /> Sleeps in</span>
            <strong class="num">~{fmtDuration(sleepsIn)}</strong>
          </div>
          <div class="bar" role="progressbar" aria-label="Idle time before sleep" aria-valuenow={Math.round(idleFrac * 100)} aria-valuemin={0} aria-valuemax={100}>
            <div style="width: {idleFrac * 100}%"></div>
          </div>
          <span class="faint small">No activity · sleeps after {live.idle.idleMin} min idle</span>
        </div>
      {/if}
    </div>
    {#if sleeps() && has('stay-awake')}
    <div class="actions">
      {#if holding}
        <Button variant="secondary" onclick={() => confirmAllowSleep()}>{#snippet icon()}<Moon />{/snippet}Allow sleep</Button>
      {:else}
        <span class="lbl">Stay awake</span>
        <div class="chips">
          <Button size="sm" onclick={() => confirmStayAwake(30)}>30 min</Button>
          <Button size="sm" onclick={() => confirmStayAwake(120)}>2 h</Button>
          <Button size="sm" onclick={() => confirmStayAwake('forever')}>Until I say</Button>
        </div>
      {/if}
    </div>
    {/if}
  {:else if power.state === 'asleep'}
    <div class="status">
      <p class="muted">Showing data from {fmtTime(power.lastSnapshot)}. Opening an app or a live view needs the server awake.</p>
    </div>
    {#if mode.wake}
      <div class="actions">
        <Button variant="primary" size="lg" class="wake" onclick={() => confirmWake()}>{#snippet icon()}<Power />{/snippet}Wake {mode.server}</Button>
      </div>
    {:else}
      <p class="faint small">No wake method is set up on the gateway (CIRRUS_WAKE).</p>
    {/if}
  {:else if power.state === 'unreachable'}
    <div class="status">
      <p class="muted">
        {mode.setup === 'split' ? `${mode.server} is on the tailnet, but its agent` : 'The agent'} hasn't reported since {fmtTime(power.lastSnapshot)}.
        Check <code>cirrusux-agent.service</code> on {mode.server}.
      </p>
    </div>
  {:else}
    <ol class="steps">
      {#each WAKE_STEPS as s, i}
        <li class:done={i < power.wakeStep} class:now={i === power.wakeStep}>
          <span class="tick" aria-hidden="true">{#if i < power.wakeStep}<Check size={13} />{/if}</span>
          {s}
        </li>
      {/each}
    </ol>
  {/if}

  {#if next && sleeps()}<p class="next faint small">Next scheduled wake: {next.unit.replace(/\.timer$/, '').replace(/-/g, ' ')}, {fmtDayTime(next.at)}</p>{/if}
</div>

<style>
  .power {
    position: relative; height: 100%; display: flex; flex-direction: column; gap: var(--space-4);
    padding: var(--space-5); overflow: hidden; isolation: isolate;
    --c: var(--ok);
  }
  .asleep { --c: var(--sleep); }
  .waking { --c: var(--info); }
  .unreachable { --c: var(--warn); }
  .glow {
    position: absolute; inset: -40% -20% auto auto; width: 75%; aspect-ratio: 1; z-index: -1;
    background: radial-gradient(closest-side, color-mix(in oklab, var(--c) 30%, transparent), transparent);
    filter: blur(10px); transition: background var(--dur-3) var(--ease);
  }
  .top { display: flex; align-items: center; gap: var(--space-4); }
  .orb {
    flex: none; display: grid; place-items: center; width: 64px; height: 64px; border-radius: 50%;
    color: var(--c); background: color-mix(in oklab, var(--c) 14%, var(--surface-2));
    box-shadow: 0 0 0 6px color-mix(in oklab, var(--c) 8%, transparent), 0 0 40px -6px var(--c);
    transition: color var(--dur-3) var(--ease), background var(--dur-3) var(--ease), box-shadow var(--dur-3) var(--ease);
  }
  .spin { display: inline-flex; animation: spin 1.1s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .head { display: flex; flex-direction: column; min-width: 0; }
  .eyebrow { font-size: 0.75rem; font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-faint); }
  .state { font-size: clamp(1.9rem, 4.5cqi, 2.6rem); font-weight: 680; letter-spacing: -0.03em; color: var(--text); }
  .since { font-size: 0.9rem; }
  .notice {
    display: flex; gap: 8px; align-items: flex-start; padding: 8px 10px; border-radius: var(--radius-sm);
    background: var(--warn-soft); color: var(--text); font-size: 0.84rem; line-height: 1.35;
  }
  .notice :global(svg) { color: var(--warn); flex: none; margin-top: 2px; }
  .status { flex: 1; min-height: 0; display: flex; flex-direction: column; justify-content: center; }
  .line { display: flex; align-items: center; gap: 8px; color: var(--text-muted); }
  .line :global(svg) { color: var(--c); flex: none; }
  .countdown { display: flex; flex-direction: column; gap: 8px; }
  .cd-row { display: flex; justify-content: space-between; align-items: baseline; color: var(--text-muted); }
  .cd-row span { display: inline-flex; align-items: center; gap: 6px; }
  .cd-row strong { font-size: 1.25rem; color: var(--text); }
  .bar { height: 6px; border-radius: 999px; background: var(--surface-3); overflow: hidden; }
  .bar div { height: 100%; background: linear-gradient(90deg, var(--ok), var(--sleep)); border-radius: inherit; transition: width var(--dur-3) var(--ease); }
  .small { font-size: 0.8rem; }
  .actions { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; }
  .lbl { font-size: 0.85rem; color: var(--text-muted); }
  .chips { display: flex; gap: 6px; flex-wrap: wrap; }
  .actions :global(.wake) { width: 100%; }
  .steps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; flex: 1; }
  .steps li { display: flex; align-items: center; gap: 10px; color: var(--text-faint); transition: color var(--dur-2) var(--ease); }
  .steps li.done { color: var(--text-muted); }
  .steps li.now { color: var(--text); font-weight: 550; }
  .tick {
    display: grid; place-items: center; width: 20px; height: 20px; border-radius: 50%;
    border: 1.5px solid var(--border-strong); color: var(--on-accent);
  }
  .done .tick { background: var(--ok); border-color: var(--ok); color: var(--bg); }
  .now .tick { border-color: var(--info); border-top-color: transparent; animation: spin 0.9s linear infinite; }
  .next { margin-top: auto; }
  @media (prefers-reduced-motion: reduce) { .spin, .now .tick { animation: none; } }
</style>
