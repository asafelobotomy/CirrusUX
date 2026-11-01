// UK time everywhere (brief §9 rule 8). Demo mode runs on a fake clock that
// starts at the sample data's time and then ticks in real time, so the data
// and the countdowns agree; live mode switches to the real clock.

const FIXTURE_NOW = Date.parse('2026-11-01T20:40:00Z');
let base = FIXTURE_NOW;
let loadedAt = Date.now();

export const clock = $state({ now: FIXTURE_NOW });

setInterval(() => {
  clock.now = base + (Date.now() - loadedAt);
}, 1000);

/** Live mode: follow the real clock instead of the sample-data clock. */
export function useRealClock(): void {
  base = loadedAt = Date.now();
  clock.now = base;
}

export const now = () => new Date(clock.now);

const TZ = 'Europe/London';
const hm = new Intl.DateTimeFormat('en-GB', { timeZone: TZ, hour: '2-digit', minute: '2-digit' });
const dayHm = new Intl.DateTimeFormat('en-GB', {
  timeZone: TZ, weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit',
});
const dayOnly = new Intl.DateTimeFormat('en-GB', { timeZone: TZ, day: 'numeric', month: 'short' });
const zone = new Intl.DateTimeFormat('en-GB', { timeZone: TZ, timeZoneName: 'short' });

/** "GMT" or "BST" for the given moment. */
export function tzLabel(d: Date = now()): string {
  // Some ICU builds say "GMT+0" / "GMT+1" instead of GMT / BST.
  const name = zone.formatToParts(d).find((p) => p.type === 'timeZoneName')?.value ?? '';
  return name === 'GMT+1' || name === 'BST' ? 'BST' : 'GMT';
}

export const fmtTime = (d: Date) => hm.format(d);
export const fmtDayTime = (d: Date) => dayHm.format(d);
export const fmtDay = (d: Date) => dayOnly.format(d);

export function fmtAgo(d: Date, from: Date = now()): string {
  const s = Math.round((from.getTime() - d.getTime()) / 1000);
  if (s < 45) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return m % 60 ? `${h} h ${m % 60} min ago` : `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}

export function fmtDuration(ms: number): string {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

const ymd = new Intl.DateTimeFormat('en-CA', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit' });

/** Start of the UK day containing d. */
export function ukMidnight(d: Date): Date {
  const [y, m, day] = ymd.format(d).split('-').map(Number);
  const utc = Date.UTC(y, m - 1, day);
  return new Date(utc - (tzLabel(new Date(utc + 12 * 3600_000)) === 'BST' ? 3600_000 : 0));
}

/** Fixture helper: a UK wall-clock time on 1 Nov 2026 (GMT, so UTC = UK). */
export const at = (hhmm: string, day = '2026-11-01') => new Date(`${day}T${hhmm}:00Z`);
