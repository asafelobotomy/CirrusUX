// Confirmations for CirrusStore apps, storage and the archive, and OS updates.
import { ask } from './ui.svelte';
import { now } from '../time.svelte';
import { mode } from '../api.svelte';
import { refreshSoon } from '../sync.svelte';
import { BUILT_IN, cleanOptions, sourceLabel, type AppOptions, type CatalogueApp } from '../catalogue';
import { demoJob, store, type ArchiveEntry } from './store.svelte';
import { d3, offline, sendAction } from './send';


// ---- CirrusStore (docs/history/PHASES-4-6 §5c) ----

/** Installs an app: the confirmation shows the exact file the server writes. */
export function confirmInstall(app: CatalogueApp, options: AppOptions, preview: { file: string; content: string } | null): void {
  if (offline()) return;
  const signed = app.source && app.source !== BUILT_IN ? ` Its definition is signed by ${sourceLabel(app.source)}, and ${mode.server} checks the signature before anything runs.` : '';
  ask({
    title: `Install ${app.name}?`,
    body: `Downloads ${app.image}:${app.tag} (pinned to its digest), writes the file below, starts it and waits until it answers.${signed}${d3()}`,
    command: `cirrusux-helper app-job (install ${app.id})`,
    undo: `Remove ${app.name}; its folder is archived`,
    confirmLabel: 'Install',
    changelog: `Started installing ${app.name}`,
    preview: preview ?? undefined,
    run: () => {
      if (!mode.live) return demoJob('install', app);
      void sendAction({ action: 'app_install', args: { app: app.id, store: app.source || BUILT_IN, options: JSON.stringify(cleanOptions(app, options)) } }, `Started installing ${app.name}`).then(() =>
        refreshSoon(),
      );
    },
  });
}

/** Removes an app: stopped, and its folder moved to the archive. */
export function confirmRemove(app: CatalogueApp): void {
  if (offline()) return;
  ask({
    title: `Remove ${app.name}?`,
    body: `Stops ${app.name} and moves its folder to the archive; its settings and data are kept there until you delete them in archive review. Its downloaded image goes, as a reinstall downloads it again.${d3()}`,
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

/** Moves an installed app to another version: an update, or back to the
 *  previous one. With definition, it moves to the newer signed definition
 *  CirrusStore offers from that source (new settings, ports or version). */
export function confirmUpdate(app: CatalogueApp, from: string, tag: string, digest: string, rollback = false, definition?: { source: string }): void {
  if (offline()) return;
  const what = definition && tag === from ? `${app.name}'s new definition` : tag;
  ask({
    title: rollback ? `Put ${app.name} back to ${tag}?` : `Update ${app.name} to ${what}?`,
    body:
      (definition
        ? `Uses the newer definition ${sourceLabel(definition.source)} signed (its ports, settings and version), checked again on ${mode.server}. `
        : '') +
      `Downloads ${app.image}:${tag} (pinned to its digest), backs up ${app.name}'s settings and data, then restarts it on the new version. ` +
      `If it doesn't answer afterwards, ${from} and its settings come back by themselves.${d3()}`,
    command: `cirrusux-helper app-job (update ${app.id}: ${from} → ${tag})`,
    undo: tag === from ? undefined : `Update ${app.name} back to ${from}`,
    confirmLabel: rollback ? 'Go back' : 'Update',
    changelog: `Started updating ${app.name} to ${tag}`,
    run: () => {
      if (!mode.live) return demoJob('update', app, tag);
      const args: Record<string, string> = { app: app.id, tag, digest };
      if (definition) args.store = definition.source;
      void sendAction({ action: 'app_update', args }, `Started updating ${app.name} to ${tag}`).then(() => refreshSoon());
    },
  });
}

// ---- storage and the archive (docs/history/PHASES-4-6 §6c) ----

/** Measures the folders and lists the archive: a job, since it reads everything. */
export async function measureStorage(): Promise<void> {
  if (offline()) return;
  if (!mode.live) {
    const at = demoOSJob('storage-scan', ['Measuring folders (it takes a while on a big disk)'], 'Measured 2 folder(s); 3 item(s) in the archive');
    setTimeout(() => store.storage && (store.storage.measured = now()), at);
    return;
  }
  await sendAction({ action: 'storage_scan' }, 'Measured the storage', true);
  refreshSoon();
}

/** The second step of deleting from the archive: type its name. The first
 *  step, the review of exactly what goes, is on the Storage page. */
export function confirmArchiveDelete(e: ArchiveEntry, what: string): void {
  if (offline()) return;
  ask({
    title: `Delete ${what}?`,
    body: `${e.name} goes for good: it can't be undone, and nothing else in CirrusUX deletes anything.${d3()}`,
    command: `cirrusux-helper archive-delete ${e.name}`,
    confirmLabel: 'Delete for good',
    danger: true,
    typed: e.name,
    changelog: `Deleted ${e.name} from the archive`,
    run: () => {
      if (!mode.live) {
        const at = demoOSJob('archive-delete', [`Deleting ${e.name}`], `${e.name} is deleted`);
        setTimeout(() => store.storage && (store.storage.archive = store.storage.archive.filter((x) => x.name !== e.name)), at);
        return;
      }
      void sendAction({ action: 'archive_delete', args: { name: e.name, confirm: e.name } }, `Deleted ${e.name} from the archive`).then(() => refreshSoon());
    },
  });
}

// ---- OS updates (docs/history/PHASES-4-6 §6b) ----

/** Refreshes the package lists and lists what's upgradable: a job on the server. */
export async function checkOS(): Promise<void> {
  if (offline()) return;
  if (!mode.live) {
    const job = demoOSJob('os-check', ['Refreshing the package lists', 'Listing upgradable packages'], '3 package update(s) available');
    setTimeout(() => {
      store.os = {
        checked: now(),
        packages: [
          { name: 'openssl', from: '3.5.1-1', to: '3.5.1-1+deb13u1', security: true },
          { name: 'tzdata', from: '2026a-0+deb13u1', to: '2026b-0+deb13u1' },
          { name: 'systemd', from: '257.8-1~deb13u1', to: '257.9-1~deb13u1' },
        ],
      };
    }, job);
    return;
  }
  await sendAction({ action: 'os_check' }, 'Checked for OS updates', true);
  refreshSoon();
}

/** Installs the pending OS updates. apt can't undo them, so the dialog says so. */
export function confirmOSUpgrade(count: number): void {
  if (offline()) return;
  ask({
    title: `Install ${count} OS package update${count === 1 ? '' : 's'}?`,
    body:
      `Runs apt-get upgrade on ${mode.server}: it updates installed packages, adds a new one only when an update needs it (a new kernel, for one), never removes any, and keeps config files you changed. ` +
      `Package upgrades can't be undone from the panel; the nightly system backup is the way back.${d3()}`,
    command: 'cirrusux-helper os-upgrade (apt-get upgrade --with-new-pkgs)',
    confirmLabel: 'Install updates',
    changelog: 'Started installing OS package updates',
    run: () => {
      if (!mode.live) {
        const n = store.os?.packages.length ?? 0;
        const at = demoOSJob('os-upgrade', ['Refreshing the package lists', `Installing ${n} packages`, 'Listing upgradable packages'], `Installed ${n} package update(s)`);
        setTimeout(() => (store.os = { checked: now(), packages: [], rebootRequired: true, upgraded: now(), upgradedCount: n }), at);
        return;
      }
      void sendAction({ action: 'os_upgrade' }, 'Started installing OS package updates').then(() => refreshSoon());
    },
  });
}

function demoOSJob(action: 'os-check' | 'os-upgrade' | 'storage-scan' | 'archive-delete', steps: string[], done: string): number {
  const job = { id: Math.random().toString(16).slice(2, 10), action, app: action.startsWith('os') ? 'os' : 'storage', name: action.startsWith('os') ? 'Operating system' : 'Storage', state: 'running' as const, step: steps[0], started: now(), finished: null };
  store.jobs.unshift(job);
  steps.forEach((s, i) => setTimeout(() => (store.jobs.find((j) => j.id === job.id)!.step = s), i * 1000));
  const end = steps.length * 1000;
  setTimeout(() => Object.assign(store.jobs.find((j) => j.id === job.id)!, { state: 'done', step: done, finished: now() }), end);
  return end;
}
