// CirrusUX's own releases: pure helpers for the Updates page (no state).

/** A machine's install, as the agent or the gateway reports it (selfupgrade.Info). */
export interface Install { managed: boolean; current?: string; previous?: string }

/** Reads "v0.9.0", "0.9.0" or "0.9.0 (a1b2c3d)"; null when it isn't a version. */
export function parseVersion(s: string): [number, number, number] | null {
  const m = /^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})(?:[-+ ].*)?$/.exec(s.trim());
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

/** Whether release tag is newer than what a machine has. Unknown versions are never offered anything. */
export function isNewer(tag: string, have: string): boolean {
  const a = parseVersion(tag);
  const b = parseVersion(have);
  if (!a || !b) return false;
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] > b[i];
  return false;
}

/** no-report: nothing known yet; by-hand: built from source; offer: latest is newer; current: nothing newer known. */
export type MachineState = 'no-report' | 'by-hand' | 'offer' | 'current';

export function machineState(inst: Install | null, latest: string): MachineState {
  if (!inst) return 'no-report';
  if (!inst.managed) return 'by-hand';
  return latest && isNewer(latest, inst.current ?? '') ? 'offer' : 'current';
}
