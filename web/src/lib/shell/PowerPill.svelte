<script lang="ts">
  import StatusDot from '../components/StatusDot.svelte';
  import { power, sleepsInMs } from '../state/power.svelte';
  import { confirmWake } from '../state/actions';
  import { fmtDuration } from '../time.svelte';
  import { go } from '../state/ui.svelte';

  const s = $derived(sleepsInMs());
  const label = $derived(
    power.state === 'awake'
      ? s === null ? 'Awake' : `Awake · sleeps in ${fmtDuration(s)}`
      : power.state === 'asleep' ? 'Asleep · Wake' : power.state === 'unreachable' ? 'No reports' : 'Waking…',
  );
</script>

<button class="pill {power.state}" onclick={() => (power.state === 'asleep' ? confirmWake() : go('home'))} aria-label="cloud64 is {label}">
  <StatusDot tone={power.state === 'awake' ? 'ok' : power.state === 'asleep' ? 'sleep' : power.state === 'unreachable' ? 'warn' : 'info'} pulse={power.state === 'waking'} />
  <span class="num">{label}</span>
</button>

<style>
  .pill {
    display: inline-flex; align-items: center; gap: 9px; height: 34px; padding: 0 14px 0 12px;
    border-radius: 999px; border: 1px solid var(--border); background: var(--surface-2);
    font-size: 0.86rem; font-weight: 550; white-space: nowrap;
    transition: background var(--dur-2) var(--ease), border-color var(--dur-2) var(--ease);
  }
  .pill:hover { border-color: var(--border-strong); }
  .asleep { border-color: color-mix(in oklab, var(--sleep) 45%, transparent); background: var(--sleep-soft); }
  @media (max-width: 420px) { .pill span.num { max-width: 9.5em; overflow: hidden; text-overflow: ellipsis; } }
</style>
