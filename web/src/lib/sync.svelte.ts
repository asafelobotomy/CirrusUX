// Live mode: keeps the UI in step with the gateway. Polls only the gateway
// (wherever it runs), never a sleeping server, and only while the tab is visible.
import { api, ApiError, mode } from './api.svelte';
import { live } from './state/live.svelte';
import { power, type PowerState } from './state/power.svelte';
import { settings } from './state/settings.svelte';
import { boards, migrateBoards } from './state/layout.svelte';
import { runningJob, store, type AppsHost, type ArchiveEntry, type StorageResult } from './state/store.svelte';
import { toast } from './state/ui.svelte';
import { fmtTime, useRealClock } from './time.svelte';
import { setPersistence } from './demo/store';
import { appName } from './apps';
import type { Health } from './state/live.svelte';

// ---- wire types (dates arrive as strings) ----

interface Snap {
  takenAt: string;
  agentVersion?: string;
  bootAt: string;
  power: {
    since: string; lastActivity?: string; idleMin: number; graceMin: number;
    stayAwakeUntil?: string; stayAwakeForever?: boolean; reasons: string[];
    nextWakes: { at: string; unit: string }[];
  };
  containers: { name: string; image: string; tag: string; state: string; health: string; status: string; ports: number[]; upSince?: string }[];
  backups: { files: { name: string; at: string; bytes: number; encrypted: boolean }[]; lastRun?: { start: string; end: string; ok: boolean; summary: string }; nextRun?: string };
  disk?: { totalBytes: number; freeBytes: number; linkMbps: number };
  temps: number[];
  security?: { banned?: number; firewall: { chain: string; ok: boolean }[]; clamav?: { at: string; files: number; infected: number } };
  features?: string[];
  timeline: { from: string; to?: string; state: 'awake' | 'asleep'; reason?: string }[];
  errors?: string[];
  apps?: { id: string; name: string; tag: string; digest?: string; previousTag?: string; previousDigest?: string; runtime: string; mainPort: number; https?: boolean; installed: string }[];
  storage?: { measured: string; roots: StorageResult['roots']; archive: (Omit<ArchiveEntry, 'modified'> & { modified: string })[] };
  osUpdates?: { checked: string; packages: { name: string; from: string; to: string; security?: boolean }[]; rebootRequired?: boolean; upgraded?: string; upgradedCount?: number };
  jobs?: { id: string; action: 'install' | 'remove' | 'update' | 'os-check' | 'os-upgrade' | 'storage-scan' | 'archive-delete'; app: string; name: string; state: 'running' | 'done' | 'failed'; step: string; detail?: string; error?: string; started: string; finished?: string }[];
  appsHost?: AppsHost;
}

interface StateResp {
  power: { state: PowerState; since?: string; wakeStep?: number; wakeError?: string; notice?: string };
  snapshot: Snap | null;
}

const d = (s?: string | null) => (s ? new Date(s) : null);

function clearSampleData(): void {
  // Nothing from the sample data may pass for this server's: no report yet.
  power.since = new Date();
  power.lastSnapshot = new Date(0);
  Object.assign(live, {
    containers: [], changelog: [], lastBackupRun: null, backups: [], nextWakes: [], disk: null,
    folders: [], temps: [], timeline: [], security: null, updates: [], problems: [],
  });
}

let lastWakeError = '';

function apply(st: StateResp): void {
  const p = st.power;
  power.state = p.state;
  if (p.since) power.since = new Date(p.since);
  power.wakeStep = p.wakeStep ?? 0;
  power.notice = p.notice ?? '';
  if (p.wakeError && p.wakeError !== lastWakeError) toast(p.wakeError, 'warn');
  lastWakeError = p.wakeError ?? '';

  const s = st.snapshot;
  if (!s) return;
  const taken = new Date(s.takenAt);
  power.lastSnapshot = taken;
  if (mode.offline) {
    power.notice = `You're offline, so this is ${mode.server} as of ${fmtTime(taken)}, saved on this device. It may have changed since.`;
  }
  power.lastActivity = d(s.power.lastActivity) ?? new Date(s.power.since);
  power.reasons = s.power.reasons ?? [];
  power.stayAwakeUntil = s.power.stayAwakeForever ? 'forever' : d(s.power.stayAwakeUntil);
  if (p.state === 'awake') power.since = new Date(s.power.since);

  live.idle = { idleMin: s.power.idleMin, graceMin: s.power.graceMin };
  live.nextWakes = s.power.nextWakes.map((w) => ({ at: new Date(w.at), unit: w.unit }));
  live.containers = s.containers.map((c) => ({
    id: c.name, name: c.name, app: appName(c.name), image: c.image, tag: c.tag,
    port: c.ports[0] ?? null, health: c.health as Health, upSince: d(c.upSince),
    restart: '', memMb: 0, leftover: c.tag === 'latest' && c.state !== 'running',
  }));
  live.backups = s.backups.files.map((f) => ({ file: f.name, at: new Date(f.at), sizeGb: f.bytes / 1e9, encrypted: f.encrypted }));
  live.nextBackup = d(s.backups.nextRun);
  const r = s.backups.lastRun;
  live.lastBackupRun = r
    ? { start: new Date(r.start), end: new Date(r.end), ok: r.ok, running: false, summary: r.summary, sizeGb: live.backups[0]?.sizeGb ?? 0 }
    : null;
  live.disk = s.disk
    ? { totalGb: s.disk.totalBytes / 1e9, usedGb: (s.disk.totalBytes - s.disk.freeBytes) / 1e9, linkMbps: s.disk.linkMbps }
    : null;
  live.temps = s.temps ?? [];
  live.security = s.security
    ? { banned: s.security.banned ?? null, firewall: s.security.firewall ?? [],
        clamav: s.security.clamav ? { ...s.security.clamav, at: new Date(s.security.clamav.at) } : null }
    : null;
  live.timeline = s.timeline.map((t) => ({ from: new Date(t.from), to: d(t.to), state: t.state, reason: t.reason }));
  live.problems = s.errors ?? [];
  live.agentVersion = s.agentVersion ?? '';
  mode.features = s.features ?? [];
  store.installed = (s.apps ?? []).map((a) => ({ ...a, installed: new Date(a.installed) }));
  store.jobs = (s.jobs ?? []).map((j) => ({ ...j, started: new Date(j.started), finished: d(j.finished) }));
  store.host = s.appsHost ?? null;
  store.storage = s.storage ? { ...s.storage, measured: new Date(s.storage.measured), archive: (s.storage.archive ?? []).map((e) => ({ ...e, modified: new Date(e.modified) })) } : null;
  store.os = s.osUpdates ? { ...s.osUpdates, checked: new Date(s.osUpdates.checked), upgraded: d(s.osUpdates.upgraded) } : null;
}

export async function refresh(): Promise<void> {
  try {
    apply(await api<StateResp>('/api/state'));
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) toast('The gateway refused this login', 'warn');
  }
}

export async function loadChangelog(): Promise<void> {
  try {
    const entries = await api<{ at: string; who: string; what: string; undo?: string; undoAction?: { action: string; args?: Record<string, string> } }[]>('/api/changelog');
    live.changelog = entries.map((e) => ({ ...e, at: new Date(e.at) }));
  } catch {
    /* keep what we have */
  }
}

// ---- polling: fast while waking, slow otherwise, paused when hidden ----

let timer: ReturnType<typeof setTimeout> | undefined;

function schedule(): void {
  clearTimeout(timer);
  if (document.visibilityState !== 'visible') return;
  timer = setTimeout(async () => {
    await refresh();
    schedule();
  }, power.state === 'waking' || runningJob() ? 2000 : 30_000);
}

export function refreshSoon(): void {
  void refresh().then(schedule);
}

// ---- settings and boards live on the gateway (PLAN §4.5) ----

interface UIDoc { version: number; data: { settings?: typeof settings; boards?: typeof boards } | null }

let uiVersion = 0;
let lastSaved = '';

function currentUI() {
  return { settings: $state.snapshot(settings), boards: $state.snapshot(boards) };
}

function applyUI(doc: UIDoc): void {
  uiVersion = doc.version;
  if (doc.data?.settings) Object.assign(settings, doc.data.settings);
  if (doc.data?.boards?.list) {
    boards.list = migrateBoards(doc.data.boards.list);
    boards.active = doc.data.boards.active ?? boards.list[0]?.id;
  }
  lastSaved = JSON.stringify(currentUI());
}

// A save that couldn't reach the gateway is retried every 30 s and when the
// browser comes back online; the newest settings win.
let unsaved: string | null = null;
let retryTimer: ReturnType<typeof setTimeout> | undefined;

async function saveUI(json: string): Promise<void> {
  clearTimeout(retryTimer);
  let res: Response | null = null;
  try {
    res = await fetch('/api/ui', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-Cirrus-Request': '1' },
      body: JSON.stringify({ baseVersion: uiVersion, data: JSON.parse(json) }),
    });
  } catch {
    /* offline */
  }
  // No answer, or tailscale serve answering for a gateway that's down.
  if (!res || res.status >= 500) {
    if (unsaved === null) toast("Can't reach the gateway. Your settings will be saved when it's back.", 'warn');
    unsaved = json;
    retryTimer = setTimeout(retrySave, 30_000);
    return;
  }
  const retried = unsaved !== null;
  unsaved = null;
  if (res.status === 409) {
    applyUI(await res.json());
    toast('Settings were changed on another device, so this one reloaded them', 'warn');
    return;
  }
  if (!res.ok) {
    toast('Could not save settings to the gateway', 'warn');
    return;
  }
  uiVersion = (await res.json()).version;
  lastSaved = json;
  if (retried) toast('Settings saved to the gateway', 'ok');
}

function retrySave(): void {
  if (unsaved !== null) void saveUI(JSON.stringify(currentUI()));
}

export async function startLive(): Promise<void> {
  useRealClock();
  setPersistence(false);
  clearSampleData();
  try {
    const doc = await api<UIDoc>('/api/ui');
    if (doc.version > 0) applyUI(doc);
    else uiVersion = 0; // first run: this browser's settings become the gateway's
  } catch {
    /* use local defaults */
  }
  await Promise.all([refresh(), loadChangelog()]);
  schedule();
  window.addEventListener('online', retrySave);
  document.addEventListener('visibilitychange', () => (document.visibilityState === 'visible' ? refreshSoon() : clearTimeout(timer)));

  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  $effect.root(() => {
    $effect(() => {
      const json = JSON.stringify(currentUI());
      if (json === lastSaved) return;
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => void saveUI(json), 800);
    });
  });
}
