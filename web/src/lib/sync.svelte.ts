// Live mode: keeps the UI in step with the gateway. Polls only the gateway
// (on cloud-box), never cloud64, and only while the tab is visible.
import { api, ApiError } from './api.svelte';
import { live } from './state/live.svelte';
import { power, type PowerState } from './state/power.svelte';
import { settings } from './state/settings.svelte';
import { boards } from './state/layout.svelte';
import { toast } from './state/ui.svelte';
import { useRealClock } from './time.svelte';
import { setPersistence } from './fake/store';
import type { Health } from './fake/data';

// ---- wire types (dates arrive as strings) ----

interface Snap {
  takenAt: string;
  bootAt: string;
  power: {
    since: string; lastActivity?: string; idleMin: number; graceMin: number;
    stayAwakeUntil?: string; stayAwakeForever?: boolean; reasons: string[];
    nextWakes: { at: string; unit: string }[];
  };
  containers: { name: string; image: string; tag: string; state: string; health: string; status: string; ports: number[]; upSince?: string }[];
  backups: { files: { name: string; at: string; bytes: number; encrypted: boolean }[]; lastRun?: { start: string; end: string; ok: boolean; summary: string } };
  disk?: { totalBytes: number; freeBytes: number; linkMbps: number };
  temps: number[];
  security?: { banned: number; lockdown: boolean; panelDrop: boolean; clamav?: { at: string; files: number; infected: number } };
  timeline: { from: string; to?: string; state: 'awake' | 'asleep'; reason?: string }[];
  errors?: string[];
}

interface StateResp {
  power: { state: PowerState; since?: string; wakeStep?: number; wakeError?: string; notice?: string };
  snapshot: Snap | null;
}

const d = (s?: string | null) => (s ? new Date(s) : null);

const NAMES: Record<string, string> = {
  jellyfin: 'Jellyfin', navidrome: 'Navidrome', audiobookshelf: 'Audiobookshelf', kavita: 'Kavita',
  tinymediamanager: 'tinyMediaManager', 'musicbrainz-picard': 'Picard', openresty: 'OpenResty',
};

/** "1Panel-jellyfin-a1B2" → "Jellyfin". */
function appName(container: string): string {
  const base = container.replace(/^1Panel-/i, '').replace(/-[A-Za-z0-9]{4}$/, '').replace(/-\d+$/, '').toLowerCase();
  return NAMES[base] ?? base.charAt(0).toUpperCase() + base.slice(1);
}

function clearSampleData(): void {
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
  live.backups = s.backups.files.map((f) => ({ file: f.name, at: new Date(f.at), sizeGb: Math.round(f.bytes / 1e8) / 10, encrypted: f.encrypted }));
  const r = s.backups.lastRun;
  live.lastBackupRun = r
    ? { start: new Date(r.start), end: new Date(r.end), ok: r.ok, running: false, summary: r.summary, sizeGb: live.backups[0]?.sizeGb ?? 0 }
    : null;
  live.disk = s.disk
    ? { totalGb: s.disk.totalBytes / 1e9, usedGb: (s.disk.totalBytes - s.disk.freeBytes) / 1e9, linkMbps: s.disk.linkMbps }
    : null;
  live.temps = s.temps ?? [];
  live.security = s.security
    ? { banned: s.security.banned, lockdown: s.security.lockdown, panelDrop: s.security.panelDrop,
        clamav: s.security.clamav ? { ...s.security.clamav, at: new Date(s.security.clamav.at) } : null }
    : null;
  live.timeline = s.timeline.map((t) => ({ from: new Date(t.from), to: d(t.to), state: t.state, reason: t.reason }));
  live.problems = s.errors ?? [];
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
    const entries = await api<{ at: string; who: string; what: string; undo?: string }[]>('/api/changelog');
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
  }, power.state === 'waking' ? 2000 : 30_000);
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
    boards.list = doc.data.boards.list;
    boards.active = doc.data.boards.active ?? boards.list[0]?.id;
  }
  lastSaved = JSON.stringify(currentUI());
}

async function saveUI(json: string): Promise<void> {
  const res = await fetch('/api/ui', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'X-Cirrus-Request': '1' },
    body: JSON.stringify({ baseVersion: uiVersion, data: JSON.parse(json) }),
  });
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
