// Sample data shaped after the cloud64 brief (1 Nov 2026). Prototype only.
import { at } from '../time.svelte';

export type Health = 'healthy' | 'running' | 'starting' | 'unhealthy' | 'exited';

export interface Container {
  id: string;
  name: string;
  app: string;
  image: string;
  tag: string;
  port: number | null;
  health: Health;
  upSince: Date | null;
  restart: string;
  memMb: number;
  leftover?: boolean;
}

export const containers: Container[] = [
  { id: 'jf', name: '1Panel-jellyfin-a1B2', app: 'Jellyfin', image: 'jellyfin/jellyfin', tag: '10.11.11', port: 8096, health: 'healthy', upSince: at('20:20'), restart: 'always', memMb: 412 },
  { id: 'nd', name: '1Panel-navidrome-c3D4', app: 'Navidrome', image: 'deluan/navidrome', tag: '0.64.2', port: 40108, health: 'running', upSince: at('20:20'), restart: 'always', memMb: 58 },
  { id: 'abs', name: '1Panel-audiobookshelf-e5F6', app: 'Audiobookshelf', image: 'advplyr/audiobookshelf', tag: '2.37.2', port: 40096, health: 'running', upSince: at('20:20'), restart: 'always', memMb: 96 },
  { id: 'kv', name: 'kavita', app: 'Kavita', image: 'lscr.io/linuxserver/kavita', tag: 'v0.9.1.4-ls130', port: 5000, health: 'running', upSince: at('20:20'), restart: 'unless-stopped', memMb: 131 },
  { id: 'tmm', name: 'tinymediamanager', app: 'tinyMediaManager', image: 'tinymediamanager/tinymediamanager', tag: '5.3.5', port: 4000, health: 'running', upSince: at('20:20'), restart: 'unless-stopped', memMb: 388 },
  { id: 'pic', name: 'musicbrainz-picard-1', app: 'Picard', image: 'mikenye/picard', tag: '2.13.3', port: 5800, health: 'running', upSince: at('20:20'), restart: 'unless-stopped', memMb: 174 },
  { id: 'or', name: '1Panel-openresty-g7H8', app: 'OpenResty', image: '1panel/openresty', tag: '1.31.8.1-2-1-noble', port: null, health: 'running', upSince: at('20:20'), restart: 'always', memMb: 22 },
  { id: 'ng', name: 'nginx_server', app: 'nginx (leftover)', image: 'nginx', tag: 'latest', port: null, health: 'exited', upSince: null, restart: 'no', memMb: 0, leftover: true },
];

export interface AppTile {
  id: string;
  name: string;
  port: number;
  https?: boolean;
  icon: string;
  color: string;
  container?: string;
  group: 'Watch & listen' | 'Manage';
}

export const apps: AppTile[] = [
  { id: 'jellyfin', name: 'Jellyfin', port: 8096, icon: 'Clapperboard', color: '#a35ee8', container: 'jf', group: 'Watch & listen' },
  { id: 'navidrome', name: 'Navidrome', port: 40108, icon: 'Music', color: '#3fa7ff', container: 'nd', group: 'Watch & listen' },
  { id: 'abs', name: 'Audiobookshelf', port: 40096, icon: 'Headphones', color: '#e9a23b', container: 'abs', group: 'Watch & listen' },
  { id: 'kavita', name: 'Kavita', port: 5000, icon: 'BookOpen', color: '#4cc38a', container: 'kv', group: 'Watch & listen' },
  { id: 'tmm', name: 'tinyMediaManager', port: 4000, icon: 'Film', color: '#e56b6f', container: 'tmm', group: 'Manage' },
  { id: 'picard', name: 'Picard', port: 5800, https: true, icon: 'Tags', color: '#8d7cf6', container: 'pic', group: 'Manage' },
  { id: '1panel', name: '1Panel', port: 10086, icon: 'LayoutPanelLeft', color: '#5b8def', group: 'Manage' },
];

export interface Backup {
  file: string;
  at: Date;
  sizeGb: number;
  encrypted: boolean;
}

export const backups: Backup[] = [
  { file: 'cloud64-system-2026-11-01_0330.tar.zst.age', at: at('03:30'), sizeGb: 2.1, encrypted: true },
  { file: 'cloud64-system-2026-10-31_0331.tar.zst.age', at: at('03:31', '2026-10-31'), sizeGb: 2.1, encrypted: true },
  { file: 'cloud64-system-2026-10-30_0935.tar.zst', at: at('09:35', '2026-10-30'), sizeGb: 2.3, encrypted: false },
];

export const lastBackupRun = { start: at('03:30'), end: at('03:31'), ok: true, sizeGb: 2.0 };
export const nextBackup = at('03:30', '2026-11-02');
export const nextAvScan = at('04:30', '2026-11-08');

export interface Folder { name: string; path: string; gb: number; note?: string }

export const disk = { totalGb: 1900, usedGb: 1346, linkMbps: 480, expectedMbps: 5000 };
export const folders: Folder[] = [
  { name: 'TV Shows', path: 'Video/TV Shows', gb: 857, note: '38 series' },
  { name: 'Movies', path: 'Video/Movies', gb: 371, note: '178 titles' },
  { name: 'Music', path: 'Audio/Music', gb: 36, note: '≈1,393 tracks' },
  { name: 'Audiobooks', path: 'Audio/Audiobooks', gb: 24, note: '≈34 items' },
  { name: 'Other video', path: 'Video/…', gb: 9.5, note: 'not in a library' },
  { name: 'System backups', path: 'BackUp', gb: 6.1 },
  { name: 'Archive', path: '_Archive-pending-deletion', gb: 1.6, note: 'awaiting review' },
  { name: 'Books', path: 'Books', gb: 0.27 },
];

/** CPU temperature samples (°C), one per 5 min over the last 2 hours. */
export const temps = [36, 35, 35, 34, 34, 35, 38, 41, 44, 43, 40, 39, 38, 39, 41, 46, 52, 49, 44, 42, 41, 41, 40, 41];

export type Segment = { from: Date; to: Date | null; state: 'awake' | 'asleep'; reason?: string };

export const timeline: Segment[] = [
  { from: at('00:00'), to: at('03:27'), state: 'asleep' },
  { from: at('03:27'), to: at('04:02'), state: 'awake', reason: 'RTC: nightly backup' },
  { from: at('04:02'), to: at('04:35'), state: 'asleep' },
  { from: at('04:35'), to: at('05:41'), state: 'awake', reason: 'RTC: weekly ClamAV scan' },
  { from: at('05:41'), to: at('09:12'), state: 'asleep' },
  { from: at('09:12'), to: at('12:40'), state: 'awake', reason: 'Woken from the panel' },
  { from: at('12:40'), to: at('17:24'), state: 'asleep' },
  { from: at('17:24'), to: at('18:08'), state: 'awake', reason: 'Woken by admin box' },
  { from: at('18:08'), to: at('18:16'), state: 'asleep' },
  { from: at('18:16'), to: at('18:33'), state: 'awake', reason: 'TV streamer (ignored, 2-min blip)' },
  { from: at('18:33'), to: at('19:24'), state: 'asleep' },
  { from: at('19:24'), to: at('19:56'), state: 'awake', reason: 'Woken by admin box' },
  { from: at('19:56'), to: at('20:20'), state: 'asleep' },
  { from: at('20:20'), to: null, state: 'awake', reason: 'Woken by admin box' },
];

export const security = {
  fail2ban: { jail: 'sshd', banned: 0, totalBans: 3 },
  clamav: { at: at('04:38'), files: 24098, infected: 0 },
  lockdown: true,
  panelDrop: true,
  egressTop: [
    { dest: 'Tailscale DERP / peers', share: 0.71 },
    { dest: 'DNS-over-HTTPS (tailnet resolver)', share: 0.22 },
    { dest: 'repo.jellyfin.org', share: 0.04 },
    { dest: 'Other', share: 0.03 },
  ],
};

export const updates = [
  { name: 'tailscale', from: '1.104.2', to: '1.104.4', kind: 'apt' },
  { name: 'tzdata', from: '2026e', to: '2026f', kind: 'apt' },
];

export interface ChangeEntry {
  at: Date;
  who: string;
  what: string;
  undo?: string;
}

export const changelog: ChangeEntry[] = [
  { at: at('20:20'), who: 'admin box', what: 'Woke cloud64 (router)' },
  { at: at('16:05'), who: 'admin box', what: 'Switched Wake-on-LAN to magic packet only (g)', undo: 'ethtool -s enp1s0 wol ug' },
  { at: at('03:31'), who: 'cloud64-backup', what: 'Nightly backup finished, 2.0 GB, encrypted' },
  { at: at('11:12', '2026-10-31'), who: 'admin box', what: 'Encrypted backups with age (recipients file)', undo: 'restore age-recipients.txt.bak-20261031-1110' },
  { at: at('15:40', '2026-10-30'), who: 'admin box', what: 'Removed the x-bit from 7,015 media files' },
];

/** Idle-check config from /etc/cloud64-idle.conf. */
export const idleConf = { idleMin: 30, graceMin: 15 };
