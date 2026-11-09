// Every action the panel can take, as a confirmation (PLAN §3.3, rules 2–3).
// Buttons, the command palette, Undo and the assistant all go through these.
// Live, the gateway queues the action for the server's agent and logs it; in demo
// mode it's simulated.
import { live } from './live.svelte';
import { allowSleep, power, stayAwake, wake } from './power.svelte';
import { ask, toast, type Confirm } from './ui.svelte';
import { now } from '../time.svelte';
import { api, mode } from '../api.svelte';
import { loadChangelog, refreshSoon } from '../sync.svelte';
import { confirmGit } from '../git/actions';
import { installedApp, loadCatalogue, store } from './store.svelte';
import { d3, offline, sendAction, type ActionReq, type Opts } from './send';
import { confirmRemove, confirmUpdate } from './actionsStore';

export { sendAction, type ActionReq } from './send';
export * from './actionsStore';

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

export function confirmWake(o: Opts = {}): void {
  if (offline()) return;
  if (!mode.wake) {
    toast(`No wake method is set up for ${mode.server} (CIRRUS_WAKE on the gateway).`, 'warn');
    return;
  }
  ask({
    ...o,
    title: `Wake ${mode.server}?`,
    body:
      mode.wakeVia === 'command'
        ? `Runs the gateway's wake command (CIRRUS_WAKE_CMD). ${mode.server} usually answers in 20–30 seconds.`
        : `Sends a Wake-on-LAN magic packet from ${mode.gateway}. ${mode.server} usually answers in 20–30 seconds.`,
    command: mode.wakeVia === 'command' ? 'CIRRUS_WAKE_CMD' : 'Wake-on-LAN magic packet',
    confirmLabel: 'Wake',
    changelog: `Woke ${mode.server}`,
    run: () => (mode.live ? void liveWake() : wake()),
  });
}

const holdLabel = (m: number | 'forever') => (m === 'forever' ? 'until you allow sleep' : `for ${m >= 60 ? m / 60 + ' h' : m + ' min'}`);

export function confirmStayAwake(minutes: number | 'forever', o: Opts = {}): void {
  if (offline('stay_awake')) return;
  const label = holdLabel(minutes);
  ask({
    ...o,
    title: `Keep ${mode.server} awake ${label}?`,
    body: 'The idle check will not suspend the server while this is set.',
    command: `cirrusux-helper stay-awake ${minutes}`,
    undo: 'cirrusux-helper allow-sleep',
    confirmLabel: 'Keep awake',
    changelog: `Kept ${mode.server} awake ${label}`,
    run: () =>
      mode.live ? void sendAction({ action: 'stay_awake', args: { minutes: String(minutes) } }, `Kept ${mode.server} awake ${label}`) : stayAwake(minutes),
  });
}

export function confirmAllowSleep(o: Opts = {}): void {
  if (offline('allow_sleep')) return;
  ask({
    ...o,
    title: `Allow ${mode.server} to sleep?`,
    body: `Removes the stay-awake hold. The ${live.idle.idleMin}-minute idle timer applies again.`,
    command: 'cirrusux-helper allow-sleep',
    confirmLabel: 'Allow sleep',
    changelog: `Allowed ${mode.server} to sleep`,
    run: () => (mode.live ? void sendAction({ action: 'allow_sleep' }, `Allowed ${mode.server} to sleep`) : allowSleep()),
  });
}

export function confirmContainer(id: string, verb: 'restart' | 'stop' | 'start', o: Opts = {}): void {
  if (offline(`container_${verb}`)) return;
  const c = live.containers.find((x) => x.id === id || x.name === id);
  if (!c) return;
  const Verb = verb[0].toUpperCase() + verb.slice(1);
  const done = { start: 'Started', stop: 'Stopped', restart: 'Restarted' }[verb];
  const undo: Record<string, string | undefined> = { stop: `docker start ${c.name}`, start: `docker stop ${c.name}`, restart: undefined };
  const confirm: Confirm = {
    ...o,
    title: `${Verb} ${c.app}?`,
    body:
      (verb === 'stop'
        ? `${c.app} will be unavailable until it is started again.`
        : verb === 'restart'
          ? `${c.app} will be unavailable for a few seconds.`
          : `Starts ${c.app} with its pinned image ${c.image}:${c.tag}.`) + d3(),
    command: `docker ${verb} ${c.name}`,
    undo: undo[verb],
    danger: verb === 'stop',
    confirmLabel: Verb,
    changelog: `${done} ${c.app}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: `container_${verb}`, args: { name: c.name } }, `${done} ${c.app}`);
        return;
      }
      c.health = verb === 'stop' ? 'exited' : 'starting';
      c.upSince = verb === 'stop' ? null : now();
      if (verb !== 'stop') setTimeout(() => (c.health = c.id === 'jf' ? 'healthy' : 'running'), 2500);
    },
  };
  ask(confirm);
}

export function confirmBackup(o: Opts = {}): void {
  if (offline('backup_run')) return;
  ask({
    ...o,
    title: 'Run a backup now?',
    body: `Runs the backup unit on ${mode.server} now.${d3()}`,
    command: 'cirrusux-helper backup-run',
    confirmLabel: 'Run backup',
    changelog: 'Started a system backup',
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'backup_run' }, 'Started a system backup');
        return;
      }
      if (live.lastBackupRun) live.lastBackupRun.running = true;
      setTimeout(() => {
        live.lastBackupRun = { start: now(), end: now(), ok: true, sizeGb: 2.0, running: false };
      }, 4000);
    },
  });
}

/** Runs an action given by name, e.g. from an assistant proposal or an Undo. */
export async function confirmAction(req: ActionReq, o: Opts = {}): Promise<void> {
  const a = req.args ?? {};
  switch (req.action) {
    case 'wake':
      return confirmWake(o);
    case 'stay_awake':
      return confirmStayAwake(a.minutes === 'forever' ? 'forever' : Number(a.minutes), o);
    case 'allow_sleep':
      return confirmAllowSleep(o);
    case 'container_start':
    case 'container_stop':
    case 'container_restart':
      return confirmContainer(a.name, req.action.slice('container_'.length) as 'start' | 'stop' | 'restart', o);
    case 'backup_run':
      return confirmBackup(o);
    case 'app_update': {
      // The catalogue loads on first use, so an Undo from Home may need it.
      await loadCatalogue();
      const app = installedApp(a.app);
      const rec = store.installed.find((x) => x.id === a.app);
      if (app && rec) return confirmUpdate(app, rec.tag, a.tag, a.digest, true);
      toast(`Can't undo: ${a.app} isn't in the app catalogue or isn't installed`, 'warn');
      return;
    }
    case 'app_remove': {
      await loadCatalogue();
      const app = installedApp(a.app);
      if (app) return confirmRemove(app);
      toast(`Can't undo: ${a.app} isn't in the app catalogue`, 'warn');
      return;
    }
  }
  if (req.action.startsWith('git_')) return confirmGit(req, undefined, o);
}

/** Undo for a changelog entry the gateway logged with an undo action. */
export function confirmUndo(undo: ActionReq): void {
  // Git hosting runs on the gateway: no need to wake the server.
  if (power.state !== 'awake' && !undo.action.startsWith('git_')) {
    toast(`${mode.server} is asleep. Wake it first, then undo.`, 'warn');
    return;
  }
  void confirmAction(undo);
}
