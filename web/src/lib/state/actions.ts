// Every action the panel can take, as a confirmation (PLAN §3.3, rules 2–3).
// Buttons, the command palette, Undo and the assistant all go through these.
// Live, the gateway queues the action for the server's agent and logs it; in demo
// mode it's simulated.
import { live } from './live.svelte';
import { allowSleep, power, stayAwake, wake } from './power.svelte';
import { ask, type Confirm } from './ui.svelte';
import { now } from '../time.svelte';
import { api, mode, sleeps } from '../api.svelte';
import { loadChangelog, refreshSoon } from '../sync.svelte';
import { toast } from './ui.svelte';
import { cleanOptions, type AppOptions, type CatalogueApp } from '../catalogue';
import { demoJob, findApp } from './store.svelte';

type Opts = { source?: 'assistant' };

export interface ActionReq { action: string; args?: Record<string, string> }

interface Command { id: string; state: 'pending' | 'sent' | 'done' | 'failed'; error?: string; output?: string[] }

/** Decision D3, for servers that sleep. */
const d3 = () => (sleeps() ? ` Keeps ${mode.server} awake for at least 10 minutes afterwards.` : '');

function offline(): boolean {
  if (!mode.offline) return false;
  toast("You're offline. Actions need the gateway.", 'warn');
  return true;
}

/** Sends an action to the server through the gateway and waits for the result. */
export async function sendAction(req: ActionReq, label: string, quiet = false): Promise<Command | null> {
  try {
    const { id } = await api<{ id: string }>('/api/actions', { method: 'POST', body: JSON.stringify(req) });
    if (!quiet) toast(`${label}: sent to ${mode.server}…`, 'info');
    const deadline = Date.now() + 5 * 60_000;
    let c: Command;
    do {
      await new Promise((r) => setTimeout(r, 1000));
      c = await api<Command>(`/api/actions/${id}`);
    } while ((c.state === 'pending' || c.state === 'sent') && Date.now() < deadline);
    if (!quiet) {
      if (c.state === 'done') toast(`${label}. Logged to CHANGELOG.`, 'ok');
      else toast(`${label} failed: ${c.error ?? `no answer from ${mode.server}`}`, 'warn');
    }
    void loadChangelog();
    refreshSoon();
    return c;
  } catch (e) {
    toast((e as Error).message, 'warn');
    return null;
  }
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

export function confirmWake(o: Opts = {}): void {
  if (offline()) return;
  ask({
    ...o,
    title: `Wake ${mode.server}?`,
    body: 'Sends a magic packet through the router. The server is usually reachable in 20–30 seconds.',
    command: 'cirrusux-wake',
    confirmLabel: 'Wake',
    changelog: `Woke ${mode.server}`,
    run: () => (mode.live ? void liveWake() : wake()),
  });
}

const holdLabel = (m: number | 'forever') => (m === 'forever' ? 'until you allow sleep' : `for ${m >= 60 ? m / 60 + ' h' : m + ' min'}`);

export function confirmStayAwake(minutes: number | 'forever', o: Opts = {}): void {
  if (offline()) return;
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
  if (offline()) return;
  ask({
    ...o,
    title: `Allow ${mode.server} to sleep?`,
    body: 'Removes the stay-awake hold. The normal 30-minute idle timer applies again.',
    command: 'cirrusux-helper allow-sleep',
    confirmLabel: 'Allow sleep',
    changelog: `Allowed ${mode.server} to sleep`,
    run: () => (mode.live ? void sendAction({ action: 'allow_sleep' }, `Allowed ${mode.server} to sleep`) : allowSleep()),
  });
}

export function confirmContainer(id: string, verb: 'restart' | 'stop' | 'start', o: Opts = {}): void {
  if (offline()) return;
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
  if (offline()) return;
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

// ---- the app store (PHASES-4-6 §5c) ----

/** Installs an app: the confirmation shows the exact file the server writes. */
export function confirmInstall(app: CatalogueApp, options: AppOptions, preview: { file: string; content: string } | null): void {
  if (offline()) return;
  ask({
    title: `Install ${app.name}?`,
    body: `Downloads ${app.image}:${app.tag} (pinned to its digest), writes the file below, starts it and waits until it answers.${d3()}`,
    command: `cirrusux-helper app-job (install ${app.id})`,
    undo: `Remove ${app.name}; its folder is archived`,
    confirmLabel: 'Install',
    changelog: `Started installing ${app.name}`,
    preview: preview ?? undefined,
    run: () => {
      if (!mode.live) return demoJob('install', app);
      void sendAction({ action: 'app_install', args: { app: app.id, options: JSON.stringify(cleanOptions(app, options)) } }, `Started installing ${app.name}`).then(() => refreshSoon());
    },
  });
}

/** Removes an app: stopped, and its folder moved to the archive. */
export function confirmRemove(app: CatalogueApp): void {
  if (offline()) return;
  ask({
    title: `Remove ${app.name}?`,
    body: `Stops ${app.name} and moves its folder to the archive. Nothing is deleted: archive review is where that happens, in two steps.${d3()}`,
    command: `cirrusux-helper app-job (remove ${app.id})`,
    confirmLabel: 'Remove',
    danger: true,
    changelog: `Started removing ${app.name} (its folder is archived)`,
    run: () => {
      if (!mode.live) return demoJob('remove', app);
      void sendAction({ action: 'app_remove', args: { app: app.id } }, `Started removing ${app.name}`).then(() => refreshSoon());
    },
  });
}

/** Runs an action given by name, e.g. from an assistant proposal or an Undo. */
export function confirmAction(req: ActionReq, o: Opts = {}): void {
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
    case 'app_remove': {
      const app = findApp(a.app);
      if (app) return confirmRemove(app);
      return;
    }
  }
}

/** Undo for a changelog entry the gateway logged with an undo action. */
export function confirmUndo(undo: ActionReq): void {
  if (power.state !== 'awake') {
    toast(`${mode.server} is asleep. Wake it first, then undo.`, 'warn');
    return;
  }
  confirmAction(undo);
}
