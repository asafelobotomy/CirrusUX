// Server data the widgets show. Starts as the sample data (demo mode) and
// is replaced from the gateway's snapshot in live mode (lib/sync.svelte.ts).
import * as data from '../demo/data';

export type Health = 'healthy' | 'running' | 'starting' | 'unhealthy' | 'exited';

export interface BackupRun { start: Date; end: Date; ok: boolean; sizeGb: number; running: boolean; summary?: string }
export interface DiskInfo { totalGb: number; usedGb: number; linkMbps: number }
export interface SecurityInfo {
  /** Addresses banned by fail2ban; null when no jail is set up. */
  banned: number | null;
  /** Configured firewall rule checks. */
  firewall: { chain: string; ok: boolean }[];
  clamav: { at: Date; files: number; infected: number } | null;
}

export const live = $state({
  containers: data.containers.map((c) => ({ ...c })),
  changelog: data.changelog.map((c) => ({ ...c })),
  lastBackupRun: { ...data.lastBackupRun, running: false } as BackupRun | null,
  backups: data.backups.map((b) => ({ ...b })),
  nextWakes: [
    { at: data.nextBackup, unit: 'system-backup.timer' },
    { at: data.nextAvScan, unit: 'clamav-weekly-scan.timer' },
  ],
  disk: { ...data.disk } as DiskInfo | null,
  folders: data.folders.map((f) => ({ ...f })),
  temps: [...data.temps],
  timeline: data.timeline.map((s) => ({ ...s })),
  security: {
    banned: data.security.fail2ban.banned,
    firewall: data.security.firewall.map((f) => ({ ...f })),
    clamav: { ...data.security.clamav },
  } as SecurityInfo | null,
  updates: data.updates.map((u) => ({ ...u })),
  idle: { ...data.idleConf },
  /** Sections the agent could not collect. */
  problems: [] as string[],
  /** Version of the agent that sent the last snapshot. */
  agentVersion: '',
});
