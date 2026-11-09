// Values a custom widget can show (docs/history/PHASES-4-6 §6d, customisation level 5):
// a fixed, named list, worked out from what the panel already knows. A
// widget only picks names from here, so nothing anyone types runs as code.
import { live } from './state/live.svelte';
import { power, powerLabel } from './state/power.svelte';
import { pendingUpdates, store } from './state/store.svelte';
import { has, mode, sleeps } from './api.svelte';
import { clock, fmtAgo, fmtDayTime, fmtDuration } from './time.svelte';
import { fmtBytes, fmtGb } from './format';

export interface Reading {
  text: string;
  /** Worth a look: shown in the warning colour. */
  warn?: boolean;
}

export interface Metric {
  id: string;
  label: string;
  /** Whether this server has what the value needs. */
  available?: () => boolean;
  read: () => Reading | null;
}

export const METRICS: Metric[] = [
  {
    id: 'power',
    label: 'Power',
    read: () => ({ text: powerLabel(power.state), warn: power.state === 'unreachable' }),
  },
  {
    id: 'uptime',
    label: 'Up for',
    read: () => (power.state === 'awake' ? { text: fmtDuration(clock.now - power.since.getTime()) } : null),
  },
  {
    id: 'containers',
    label: 'Containers running',
    read: () => {
      const all = live.containers.filter((c) => !c.leftover);
      const up = all.filter((c) => c.health !== 'exited').length;
      return { text: `${up} / ${all.length}`, warn: up < all.length || all.some((c) => c.health === 'unhealthy') };
    },
  },
  {
    id: 'disk-free',
    label: 'Disk free',
    read: () => (live.disk ? { text: fmtGb(live.disk.totalGb - live.disk.usedGb), warn: live.disk.usedGb / live.disk.totalGb > 0.9 } : null),
  },
  {
    id: 'disk-used',
    label: 'Disk used',
    read: () => (live.disk ? { text: `${Math.round((live.disk.usedGb / live.disk.totalGb) * 100)}%`, warn: live.disk.usedGb / live.disk.totalGb > 0.85 } : null),
  },
  {
    id: 'cpu-temp',
    label: 'CPU temperature',
    read: () => {
      const t = live.temps.at(-1);
      return t === undefined ? null : { text: `${Math.round(t)} °C`, warn: t >= 80 };
    },
  },
  {
    id: 'last-backup',
    label: 'Last backup',
    available: () => has('backup'),
    read: () => {
      const r = live.lastBackupRun;
      if (!r) return { text: 'none found', warn: true };
      return { text: r.ok ? fmtAgo(r.end) : 'failed', warn: !r.ok || clock.now - r.end.getTime() > 26 * 3600_000 };
    },
  },
  {
    id: 'next-wake',
    label: 'Next scheduled wake',
    available: sleeps,
    read: () => (live.nextWakes[0] ? { text: fmtDayTime(live.nextWakes[0].at) } : null),
  },
  {
    id: 'apps-installed',
    label: 'Apps installed',
    available: () => has('apps'),
    read: () => ({ text: String(store.installed.length) }),
  },
  {
    id: 'app-updates',
    label: 'App updates waiting',
    available: () => has('apps'),
    read: () => (store.updates.checked ? { text: String(pendingUpdates().length), warn: pendingUpdates().length > 0 } : { text: 'not checked' }),
  },
  {
    id: 'os-updates',
    label: 'OS updates waiting',
    available: () => has('os'),
    read: () => {
      if (!store.os) return { text: 'not checked' };
      const sec = store.os.packages.filter((p) => p.security).length;
      return { text: sec ? `${store.os.packages.length} (${sec} security)` : String(store.os.packages.length), warn: sec > 0 || !!store.os.rebootRequired };
    },
  },
  {
    id: 'archive-size',
    label: 'Archive',
    available: () => has('storage'),
    read: () => (store.storage ? { text: fmtBytes(store.storage.archive.reduce((n, e) => n + e.bytes, 0)) } : { text: 'not measured' }),
  },
  {
    id: 'banned',
    label: 'Banned by fail2ban',
    available: () => has('security'),
    read: () => (live.security?.banned != null ? { text: String(live.security.banned) } : null),
  },
  {
    id: 'last-report',
    label: 'Last report',
    available: () => mode.live,
    read: () => ({ text: fmtAgo(power.lastSnapshot), warn: clock.now - power.lastSnapshot.getTime() > 15 * 60_000 && power.state === 'awake' }),
  },
];

export const metric = (id: string) => METRICS.find((m) => m.id === id);
export const usableMetrics = () => METRICS.filter((m) => m.available?.() ?? true);
