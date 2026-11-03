// The app store (PHASES-4-6 §5c): the catalogue, what's installed, and jobs
// in progress. Live, installed apps and jobs come from the agent's reports
// (sync.svelte.ts); in demo mode they're simulated.
import demoCatalogue from 'virtual:catalogue';
import { api, mode } from '../api.svelte';
import type { CatalogueApp } from '../catalogue';
import { now } from '../time.svelte';
import { toast } from './ui.svelte';

export interface InstalledApp {
  id: string;
  name: string;
  tag: string;
  digest?: string;
  /** The version before the last update, for rolling back. */
  previousTag?: string;
  previousDigest?: string;
  runtime: string;
  mainPort: number;
  https?: boolean;
  installed: Date;
}

export interface Job {
  id: string;
  action: 'install' | 'remove' | 'update' | 'os-check' | 'os-upgrade' | 'storage-scan' | 'archive-delete';
  app: string;
  name: string;
  state: 'running' | 'done' | 'failed';
  step: string;
  detail?: string;
  error?: string;
  started: Date;
  finished: Date | null;
}

/** What installs use on the server (helperapi.AppsInfo). */
export interface AppsHost {
  runtime: string;
  appsDir: string;
  mediaRoots: string[];
  bind?: string;
  tz: string;
}

export const store = $state({
  catalogue: [] as CatalogueApp[],
  installed: [] as InstalledApp[],
  jobs: [] as Job[],
  host: null as AppsHost | null,
  /** The app whose page is open. */
  open: null as string | null,
  /** The last measurement and the archive, from the server (storage module). */
  storage: null as StorageResult | null,
  /** The last OS package check, from the server (os module). */
  os: null as OSUpdates | null,
  /** The last update check (gateway, on request). */
  updates: { checked: null as Date | null, apps: [] as AppUpdate[] },
});

/** One measurement (storage.Result). */
export interface StorageResult {
  measured: Date;
  roots: { path: string; label: string; bytes: number; diskSize: number; diskFree: number; children: { name: string; bytes: number }[] }[];
  archive: ArchiveEntry[];
}

export interface ArchiveEntry {
  name: string;
  kind: 'removed-app' | 'pre-update' | 'backup';
  app?: string;
  tag?: string;
  bytes: number;
  modified: Date;
}

/** The last OS package check (osupdate.Result). */
export interface OSUpdates {
  checked: Date;
  packages: { name: string; from: string; to: string; security?: boolean }[];
  rebootRequired?: boolean;
  upgraded?: Date | null;
  upgradedCount?: number;
}

/** One installed app's update check (gateway.AppUpdate). */
export interface AppUpdate {
  app: string;
  name: string;
  current: string;
  latest?: string;
  digest?: string;
  error?: string;
}

const toUpdates = (u: { checked: string; apps: AppUpdate[] }) => ({ checked: u.checked && !u.checked.startsWith('0001') ? new Date(u.checked) : null, apps: u.apps ?? [] });

/** The last check's result, kept by the gateway. */
export async function loadUpdates(): Promise<void> {
  if (!mode.live) return;
  try {
    store.updates = toUpdates(await api('/api/updates'));
  } catch {
    /* keep what we have */
  }
}

/** Asks the registries now: the gateway does it, so a sleeping server stays asleep. */
export async function checkUpdates(): Promise<void> {
  if (!mode.live) {
    // Demo: Jellyfin has a newer version.
    await new Promise((r) => setTimeout(r, 900));
    store.updates = {
      checked: now(),
      apps: store.installed.map((a) =>
        a.id === 'jellyfin'
          ? { app: a.id, name: a.name, current: a.tag, latest: '12.5.20261110-090000', digest: 'sha256:' + '7'.repeat(64) }
          : { app: a.id, name: a.name, current: a.tag },
      ),
    };
    return;
  }
  try {
    store.updates = toUpdates(await api('/api/updates/check', { method: 'POST' }));
  } catch (e) {
    toast(`Couldn't check for updates: ${(e as Error).message}`, 'warn');
  }
}

/** Updates waiting: newer than what runs now. */
export const pendingUpdates = () =>
  store.updates.apps.filter((u) => u.latest && store.installed.find((a) => a.id === u.app)?.tag === u.current);

/** Demo mode starts with two apps installed, and a measured disk. */
export function startDemoStore(): void {
  const GB = 1e9;
  const day = (n: number) => new Date(now().getTime() - n * 86400_000);
  store.storage = {
    measured: day(1),
    roots: [
      {
        path: '/srv/media', label: 'Media', bytes: 1308 * GB, diskSize: 1862 * GB, diskFree: 554 * GB,
        children: [
          { name: 'TV Shows', bytes: 857 * GB }, { name: 'Movies', bytes: 371 * GB }, { name: 'Music', bytes: 36 * GB },
          { name: 'Audiobooks', bytes: 24 * GB }, { name: 'Other video', bytes: 9.5 * GB }, { name: 'Books', bytes: 0.27 * GB },
        ],
      },
      { path: '/srv/cirrusux/apps', label: 'Apps', bytes: 3.4 * GB, diskSize: 240 * GB, diskFree: 181 * GB, children: [{ name: 'jellyfin', bytes: 3.1 * GB }, { name: 'navidrome', bytes: 0.3 * GB }] },
    ],
    archive: [
      { name: 'jellyfin-before-12.3.20261023-013714-20261026-210000', kind: 'pre-update', app: 'jellyfin', tag: '12.3.20261023-013714', bytes: 1.4 * GB, modified: day(7) },
      { name: 'kavita-20261020-181500', kind: 'removed-app', app: 'kavita', bytes: 0.2 * GB, modified: day(13) },
      { name: 'homeserver-system-2026-10-30_0935.tar.zst', kind: 'backup', bytes: 2.3 * GB, modified: day(3) },
    ],
  };
  store.catalogue = demoCatalogue;
  store.host = { runtime: 'podman', appsDir: '/srv/cirrusux/apps', mediaRoots: ['/srv/media'], tz: 'Europe/London' };
  store.installed = ['jellyfin', 'navidrome'].map((id) => {
    const a = demoCatalogue.find((x) => x.id === id)!;
    const main = a.ports.find((p) => p.main) ?? a.ports[0];
    return { id, name: a.name, tag: a.tag, digest: a.digest, runtime: 'podman', mainPort: main.host, https: main.https, installed: new Date(now().getTime() - 9 * 86400_000) };
  });
}

let loading: Promise<void> | null = null;

/** Fetches the catalogue once; demo mode already has it. */
export function loadCatalogue(): Promise<void> {
  if (!mode.live || store.catalogue.length) return Promise.resolve();
  loading ??= api<CatalogueApp[]>('/api/catalogue')
    .then((apps) => void (store.catalogue = apps))
    .catch((e) => {
      loading = null;
      toast(`Couldn't load CirrusStore: ${(e as Error).message}`, 'warn');
    });
  return loading;
}

export const findApp = (id: string) => store.catalogue.find((a) => a.id === id);
export const isInstalled = (id: string) => store.installed.some((a) => a.id === id);
export const runningJob = (id?: string) => store.jobs.find((j) => j.state === 'running' && (!id || j.app === id));

// ---- demo jobs: the same steps the server's job reports ----

export function demoJob(action: 'install' | 'remove' | 'update', app: CatalogueApp, tag?: string): void {
  const job: Job = { id: Math.random().toString(16).slice(2, 10), action, app: app.id, name: app.name, state: 'running', step: '', started: now(), finished: null };
  store.jobs.unshift(job);
  const steps =
    action === 'install'
      ? ["Creating the app's folders", `Downloading ${app.image}:${app.tag}`, `Starting ${app.name}`, `Waiting for ${app.name} to answer`, 'Recording the install']
      : action === 'update'
        ? [`Downloading ${app.image}:${tag}`, `Backing up ${app.name}'s settings and data`, `Starting ${app.name} ${tag}`, `Waiting for ${app.name} to answer`, 'Recording the update']
        : [`Stopping ${app.name}`, 'Archiving its folder'];
  steps.forEach((s, i) =>
    setTimeout(() => {
      const j = store.jobs.find((x) => x.id === job.id)!;
      j.step = s;
      j.detail = s.startsWith('Downloading') ? 'demo: nothing is downloaded' : undefined;
    }, i * 1100),
  );
  setTimeout(() => {
    const j = store.jobs.find((x) => x.id === job.id)!;
    j.state = 'done';
    j.finished = now();
    j.detail = undefined;
    if (action === 'update') {
      const rec = store.installed.find((a) => a.id === app.id)!;
      rec.previousTag = rec.tag;
      rec.previousDigest = rec.digest ?? app.digest;
      rec.tag = tag!;
      j.step = `${app.name} is updated`;
      j.detail = `backup in /srv/cirrusux/archive/${app.id}-before-${rec.previousTag}-…`;
    } else if (action === 'install') {
      const main = app.ports.find((p) => p.main) ?? app.ports[0];
      store.installed.push({ id: app.id, name: app.name, tag: app.tag, runtime: 'podman', mainPort: main.host, https: main.https, installed: now() });
      j.step = `${app.name} is installed`;
    } else {
      store.installed = store.installed.filter((a) => a.id !== app.id);
      j.step = `${app.name} is removed`;
      j.detail = `archived to /srv/cirrusux/archive/${app.id}-…`;
    }
  }, steps.length * 1100);
}
