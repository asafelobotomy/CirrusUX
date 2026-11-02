// One time zone everywhere: the gateway's (CIRRUS_TZ), labelled with its
// abbreviation (GMT/BST, CET/CEST). Demo mode runs on a fake clock that
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

import { mode } from './api.svelte';

// Formatters for the current zone, rebuilt when the gateway names another.
let fmt = makeFormatters(mode.tz);

function makeFormatters(timeZone: string) {
  return {
    tz: timeZone,
    hm: new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit', minute: '2-digit' }),
    dayHm: new Intl.DateTimeFormat('en-GB', { timeZone, weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }),
    dayOnly: new Intl.DateTimeFormat('en-GB', { timeZone, day: 'numeric', month: 'short' }),
    zone: new Intl.DateTimeFormat('en-GB', { timeZone, timeZoneName: 'short' }),
    ymd: new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }),
    offset: new Intl.DateTimeFormat('en-US', { timeZone, timeZoneName: 'longOffset' }),
  };
}

function f() {
  if (fmt.tz !== mode.tz) {
    try {
      fmt = makeFormatters(mode.tz);
    } catch {
      fmt = makeFormatters('UTC'); // unknown zone name
    }
  }
  return fmt;
}

/** The zone's short name for the given moment: "GMT", "BST", "CET"… */
export function tzLabel(d: Date = now()): string {
  const name = f().zone.formatToParts(d).find((p) => p.type === 'timeZoneName')?.value ?? '';
  // Some ICU builds say "GMT+1" for UK summer time instead of BST.
  if (mode.tz === 'Europe/London') return name === 'GMT+1' || name === 'BST' ? 'BST' : 'GMT';
  return name;
}

/** The zone's offset from UTC at d, in ms. */
function offsetMs(d: Date): number {
  const name = f().offset.formatToParts(d).find((p) => p.type === 'timeZoneName')?.value ?? 'GMT';
  const m = /GMT([+-])(\d{2}):(\d{2})/.exec(name);
  return m ? (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3])) * 60_000 : 0;
}

export const fmtTime = (d: Date) => f().hm.format(d);
export const fmtDayTime = (d: Date) => f().dayHm.format(d);
export const fmtDay = (d: Date) => f().dayOnly.format(d);

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

/** Start of the local day containing d. */
export function localMidnight(d: Date): Date {
  const [y, m, day] = f().ymd.format(d).split('-').map(Number);
  const utc = Date.UTC(y, m - 1, day);
  return new Date(utc - offsetMs(new Date(utc + 12 * 3600_000)));
}

/** Fixture helper: a demo wall-clock time on 1 Nov 2026 (London is on GMT, so UTC = local). */
export const at = (hhmm: string, day = '2026-11-01') => new Date(`${day}T${hhmm}:00Z`);
