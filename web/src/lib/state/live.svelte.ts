// Server data the widgets show. Starts as the sample data (demo mode) and
// is replaced from the gateway's snapshot in live mode (lib/sync.svelte.ts).
import * as data from '../demo/data';
import { agentRuns } from './agentActions';

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
  /** When the backup timer fires next; null when unknown. */
  nextBackup: data.nextBackup as Date | null,
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
  /** Actions the agent runs; empty when it didn't say (an older agent), which allows all. */
  actions: [] as string[],
  /** The agent speaks an older protocol than the gateway, or a newer one. */
  agentTooOld: false,
  gatewayTooOld: false,
});

/** Why a button is disabled when the agent can't run an action. */
export const UPDATE_HINT = 'Update CirrusUX on the server to use this';

/** Whether the server's agent will run this action. Always true for sample data. */
export function agentCan(action: string): boolean {
  return agentRuns(live.actions, live.agentTooOld || live.gatewayTooOld, action);
}

/** A tooltip for a control that needs the action: the hint when it can't be used, else nothing. */
export const needsAgent = (action: string): string | undefined => (agentCan(action) ? undefined : UPDATE_HINT);
