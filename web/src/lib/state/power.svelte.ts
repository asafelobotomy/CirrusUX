// Power state shown in the UI (PLAN §3.1): set from the gateway in live mode
// (sync.svelte.ts), simulated in demo mode.
import { at, clock, now } from '../time.svelte';
import { live } from './live.svelte';
import { mode } from '../api.svelte';

/** unreachable: cloud64 is on the tailnet but its agent isn't reporting. */
export type PowerState = 'awake' | 'waking' | 'asleep' | 'unreachable';

export const WAKE_STEPS = [
  'Magic packet sent through the router',
  'Network link up',
  'Tailscale connected',
  'Agent reporting',
];

interface Power {
  state: PowerState;
  since: Date;
  /** Last moment cloud64 saw activity; the idle timer counts from here. */
  lastActivity: Date;
  /** Snapshot time shown on cached values while asleep. */
  lastSnapshot: Date;
  stayAwakeUntil: Date | 'forever' | null;
  wakeStep: number;
  /** What the idle check counts as activity right now (its last-msg file). */
  reasons: string[];
  /** State changes since the page loaded, for the timeline. */
  transitions: { at: Date; state: 'awake' | 'asleep'; reason?: string }[];
  /** A problem on the gateway's side, e.g. cloud-box offline from Tailscale. */
  notice: string;
}

export const power: Power = $state({
  state: 'awake',
  since: at('20:20'),
  lastActivity: at('20:28'),
  lastSnapshot: at('20:40'),
  stayAwakeUntil: null,
  wakeStep: 0,
  reasons: [],
  transitions: [],
  notice: '',
});

/** Minutes until suspend, or null when something is holding it awake. */
export function sleepsInMs(): number | null {
  // Offline the data is old, so a countdown would be a guess.
  if (power.state !== 'awake' || power.reasons.length || mode.offline) return null;
  const t = clock.now;
  const stay = power.stayAwakeUntil;
  if (stay === 'forever') return null;
  if (stay && stay.getTime() > t) return null;
  const graceEnd = power.since.getTime() + live.idle.graceMin * 60_000;
  const idleEnd = power.lastActivity.getTime() + live.idle.idleMin * 60_000;
  return Math.max(graceEnd, idleEnd) - t;
}

let wakeTimer: ReturnType<typeof setTimeout> | undefined;

export function wake(): void {
  if (power.state !== 'asleep') return;
  power.state = 'waking';
  power.wakeStep = 0;
  const step = () => {
    power.wakeStep += 1;
    if (power.wakeStep < WAKE_STEPS.length) {
      wakeTimer = setTimeout(step, 1600);
    } else {
      power.state = 'awake';
      power.since = now();
      power.lastActivity = now();
      power.lastSnapshot = now();
      power.transitions.push({ at: now(), state: 'awake', reason: 'Woken from the panel' });
    }
  };
  wakeTimer = setTimeout(step, 1400);
}

export function sleep(): void {
  clearTimeout(wakeTimer);
  power.state = 'asleep';
  power.since = now();
  power.lastSnapshot = now();
  power.stayAwakeUntil = null;
  power.transitions.push({ at: now(), state: 'asleep' });
}

export function setAwake(): void {
  clearTimeout(wakeTimer);
  if (power.state === 'awake') return;
  power.state = 'awake';
  power.since = now();
  power.lastSnapshot = now();
  power.transitions.push({ at: now(), state: 'awake', reason: 'Simulated' });
}

export function stayAwake(minutes: number | 'forever'): void {
  power.stayAwakeUntil = minutes === 'forever' ? 'forever' : new Date(clock.now + minutes * 60_000);
}

export function allowSleep(): void {
  power.stayAwakeUntil = null;
}

$effect.root(() => {
  $effect(() => {
    document.documentElement.dataset.power = power.state;
  });
});
