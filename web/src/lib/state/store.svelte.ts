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
  runtime: string;
  mainPort: number;
  https?: boolean;
  installed: Date;
}

export interface Job {
  id: string;
  action: 'install' | 'remove';
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
});

/** Demo mode starts with two apps installed. */
export function startDemoStore(): void {
  store.catalogue = demoCatalogue;
  store.host = { runtime: 'podman', appsDir: '/srv/cirrusux/apps', mediaRoots: ['/srv/media'], tz: 'Europe/London' };
  store.installed = ['jellyfin', 'navidrome'].map((id) => {
    const a = demoCatalogue.find((x) => x.id === id)!;
    const main = a.ports.find((p) => p.main) ?? a.ports[0];
    return { id, name: a.name, tag: a.tag, runtime: 'podman', mainPort: main.host, https: main.https, installed: new Date(now().getTime() - 9 * 86400_000) };
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
      toast(`Couldn't load the app store: ${(e as Error).message}`, 'warn');
    });
  return loading;
}

export const findApp = (id: string) => store.catalogue.find((a) => a.id === id);
export const isInstalled = (id: string) => store.installed.some((a) => a.id === id);
export const runningJob = (id?: string) => store.jobs.find((j) => j.state === 'running' && (!id || j.app === id));

// ---- demo jobs: the same steps the server's job reports ----

export function demoJob(action: 'install' | 'remove', app: CatalogueApp): void {
  const job: Job = { id: Math.random().toString(16).slice(2, 10), action, app: app.id, name: app.name, state: 'running', step: '', started: now(), finished: null };
  store.jobs.unshift(job);
  const steps =
    action === 'install'
      ? ["Creating the app's folders", `Downloading ${app.image}:${app.tag}`, `Starting ${app.name}`, `Waiting for ${app.name} to answer`, 'Recording the install']
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
    if (action === 'install') {
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
