// Every action the panel can take, as a confirmation (PLAN §3.3, rules 2–3).
// Buttons, the command palette and Grok all go through these.
import { live } from './live.svelte';
import { allowSleep, stayAwake, wake } from './power.svelte';
import { ask, type Confirm } from './ui.svelte';
import { now } from '../time.svelte';
import { api, mode } from '../api.svelte';
import { loadChangelog, refreshSoon } from '../sync.svelte';
import { toast } from './ui.svelte';

/** Actions after wake come with Phase 2; live mode says so instead of faking them. */
function phase2(what: string): boolean {
  if (!mode.live) return false;
  toast(`${what} arrives in Phase 2. For now the panel only reads and wakes.`, 'info');
  return true;
}

async function liveWake(): Promise<void> {
  try {
    await api('/api/wake', { method: 'POST' });
    toast('Wake sent. Logged to CHANGELOG.', 'ok');
    void loadChangelog();
  } catch (e) {
    toast((e as Error).message, 'warn');
  }
  refreshSoon();
}

type Opts = { source?: 'grok' };

export function confirmWake(o: Opts = {}): void {
  ask({
    ...o,
    title: 'Wake cloud64?',
    body: 'Sends a magic packet through the router. The server is usually reachable in 20–30 seconds.',
    command: '~/bin/cloud64-wake',
    confirmLabel: 'Wake',
    changelog: 'Woke cloud64',
    run: () => (mode.live ? void liveWake() : wake()),
  });
}

export function confirmStayAwake(minutes: number | 'forever', o: Opts = {}): void {
  if (phase2('Stay awake')) return;
  const label = minutes === 'forever' ? 'until you allow sleep' : `for ${minutes >= 60 ? minutes / 60 + ' h' : minutes + ' min'}`;
  ask({
    ...o,
    title: `Keep cloud64 awake ${label}?`,
    body: 'The idle check will not suspend the server while this is set.',
    command: minutes === 'forever' ? 'cirrusux-helper stay-awake forever' : `cirrusux-helper stay-awake ${minutes}`,
    undo: 'cirrusux-helper allow-sleep',
    confirmLabel: 'Keep awake',
    changelog: `Kept cloud64 awake ${label}`,
    run: () => stayAwake(minutes),
  });
}

export function confirmAllowSleep(o: Opts = {}): void {
  if (phase2('Allow sleep')) return;
  ask({
    ...o,
    title: 'Allow cloud64 to sleep?',
    body: 'Removes the stay-awake hold. The normal 30-minute idle timer applies again.',
    command: 'cirrusux-helper allow-sleep',
    confirmLabel: 'Allow sleep',
    changelog: 'Allowed cloud64 to sleep',
    run: allowSleep,
  });
}

export function confirmContainer(id: string, verb: 'restart' | 'stop' | 'start', o: Opts = {}): void {
  if (phase2('Starting and stopping containers')) return;
  const c = live.containers.find((x) => x.id === id);
  if (!c) return;
  const undo: Record<string, string | undefined> = { stop: `docker start ${c.name}`, start: `docker stop ${c.name}`, restart: undefined };
  const confirm: Confirm = {
    ...o,
    title: `${verb[0].toUpperCase() + verb.slice(1)} ${c.app}?`,
    body:
      verb === 'stop'
        ? `${c.app} will be unavailable until it is started again.`
        : verb === 'restart'
          ? `${c.app} will be unavailable for a few seconds.`
          : `Starts ${c.app} with its pinned image ${c.image}:${c.tag}.`,
    command: `docker ${verb} ${c.name}`,
    undo: undo[verb],
    danger: verb === 'stop',
    confirmLabel: verb[0].toUpperCase() + verb.slice(1),
    changelog: `${verb[0].toUpperCase() + verb.slice(1)}ed ${c.app}`.replace('Stoped', 'Stopped'),
    run: () => {
      c.health = verb === 'stop' ? 'exited' : 'starting';
      c.upSince = verb === 'stop' ? null : now();
      if (verb !== 'stop') setTimeout(() => (c.health = c.id === 'jf' ? 'healthy' : 'running'), 2500);
    },
  };
  ask(confirm);
}

export function confirmBackup(o: Opts = {}): void {
  if (phase2('Run backup now')) return;
  ask({
    ...o,
    title: 'Run a backup now?',
    body: 'Takes about 40 seconds and ~900 MB of memory. The result is encrypted with age. Media is not included.',
    command: 'cirrusux-helper backup-run',
    confirmLabel: 'Run backup',
    changelog: 'Started a system backup',
    run: () => {
      if (live.lastBackupRun) live.lastBackupRun.running = true;
      setTimeout(() => {
        live.lastBackupRun = { start: now(), end: now(), ok: true, sizeGb: 2.0, running: false };
      }, 4000);
    },
  });
}

