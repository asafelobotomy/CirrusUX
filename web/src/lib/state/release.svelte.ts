// CirrusUX's own release: what the gateway's last check found, and the
// gateway machine's install. The server's install arrives in its snapshot
// (live.install). Sample data until a gateway answers.
import { api, mode } from '../api.svelte';
import { now } from '../time.svelte';
import { toast } from './ui.svelte';
import type { Install } from '../release';

export interface SelfJob { job: string; action: 'self-upgrade' | 'self-rollback'; state: 'running' | 'done' | 'failed'; step: string; error?: string; started: Date }

export const release = $state({
  /** The gateway has answered (always true for sample data). */
  loaded: false,
  checked: new Date(now().getTime() - 5 * 3600_000) as Date | null,
  /** The newest release's tag, e.g. v0.9.0; empty before the first check. */
  latest: 'v0.9.0',
  url: 'https://github.com/asafelobotomy/CirrusUX/releases',
  error: '',
  /** The gateway's own version; empty for a development build. */
  current: '0.8.1',
  /** The gateway checks once a day by itself. */
  auto: true,
  gateway: { managed: true, current: 'v0.8.1', previous: 'v0.8.0', canUpgrade: true, job: null as SelfJob | null } as Install & { canUpgrade: boolean; job: SelfJob | null },
  checking: false,
});

interface Wire {
  checked: string; latest?: string; url?: string; error?: string; current: string; auto: boolean;
  gateway: Install & { canUpgrade: boolean; job?: Omit<SelfJob, 'started'> & { started: string } };
}

function take(w: Wire): void {
  release.loaded = true;
  release.checked = w.checked && !w.checked.startsWith('0001') ? new Date(w.checked) : null;
  release.latest = w.latest ?? '';
  release.url = w.url ?? '';
  release.error = w.error ?? '';
  release.current = w.current;
  release.auto = w.auto;
  release.gateway = { ...w.gateway, job: w.gateway.job ? { ...w.gateway.job, started: new Date(w.gateway.job.started) } : null };
}

/** Reads the last check from the gateway; asks nobody else. Quiet when the gateway is restarting. */
export async function loadRelease(): Promise<void> {
  if (!mode.live) return;
  try {
    take(await api<Wire>('/api/release'));
  } catch {
    // An upgrade restarts the gateway: the next poll answers.
  }
}

/** The Check button: the gateway asks GitHub now. */
export async function checkRelease(): Promise<void> {
  release.checking = true;
  try {
    if (!mode.live) {
      await new Promise((r) => setTimeout(r, 700));
      release.checked = now();
    } else {
      take(await api<Wire>('/api/release/check', { method: 'POST' }));
      if (release.error) toast(`Couldn't check for a new CirrusUX: ${release.error}`, 'warn');
    }
  } catch (e) {
    toast(`Couldn't check for a new CirrusUX: ${(e as Error).message}`, 'warn');
  }
  release.checking = false;
}
