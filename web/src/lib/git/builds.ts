// Pure helpers for builds: their shapes as the gateway sends them, and the
// words and tones the panel shows. No imports, so the unit tests run them.

export type BuildState = 'queued' | 'running' | 'passed' | 'failed' | 'cancelled';
export type StepState = 'waiting' | 'running' | 'passed' | 'failed' | 'cancelled' | 'skipped';

export interface StepStatus {
  name: string;
  image: string;
  state: StepState;
  started?: string;
  finished?: string;
  exit?: number;
}

export interface BuildRun {
  id: string;
  repo: string;
  ref: string;
  commit: string;
  subject: string;
  who: string;
  created: string;
  state: BuildState;
  steps: StepStatus[];
  error?: string;
  started?: string;
  finished?: string;
  logSize: number;
  /** A push started it. */
  auto?: boolean;
}

export interface BuildSettings { onPush: boolean; wake: boolean }

/** The three choices for building on push, as one setting. */
export type PushMode = 'off' | 'wait' | 'wake';
export const pushMode = (s: BuildSettings): PushMode => (!s.onPush ? 'off' : s.wake ? 'wake' : 'wait');
export const modeArgs = (m: PushMode): { on_push: string; wake: string } => ({ on_push: m === 'off' ? 'no' : 'yes', wake: m === 'wake' ? 'yes' : 'no' });

export interface BuildLog { text: string; next: number }

/** The pipeline file a repository keeps (build.File). */
export const PIPELINE_FILE = '.cirrus/build.yml';

export const isOver = (s: BuildState | StepState): boolean =>
  s === 'passed' || s === 'failed' || s === 'cancelled' || s === 'skipped';

const TEXT: Record<BuildState | StepState, string> = {
  queued: 'Waiting to start', running: 'Running', passed: 'Passed', failed: 'Failed',
  cancelled: 'Cancelled', waiting: 'Not started', skipped: 'Skipped',
};
export const stateText = (s: BuildState | StepState): string => TEXT[s] ?? s;

export type Tone = 'ok' | 'bad' | 'info' | 'neutral' | 'warn';
export function stateTone(s: BuildState | StepState): Tone {
  switch (s) {
    case 'passed': return 'ok';
    case 'failed': return 'bad';
    case 'running': return 'info';
    case 'queued': return 'warn';
    default: return 'neutral';
  }
}

/** A length of time in words: "45 s", "3 min 12 s", "1 h 4 min". */
export function fmtBuildTime(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `${m} min ${s % 60} s` : `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

/** How long something ran: to its end, or to now while it runs. */
export function took(started: string | undefined, finished: string | undefined, now: Date): string {
  if (!started) return '';
  const end = finished ? new Date(finished) : now;
  return fmtBuildTime(end.getTime() - new Date(started).getTime());
}
