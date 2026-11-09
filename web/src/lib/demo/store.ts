// Demo-mode settings store: this browser only, and works the same when
// storage is blocked. In live mode the gateway stores settings instead
// (sync.svelte.ts) and saving here is switched off.

const PREFIX = 'cirrusux:';

// Off in live mode, where the gateway stores settings instead.
let persist = true;

export function setPersistence(on: boolean): void {
  persist = on;
}

export function load<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    return raw ? { ...fallback, ...JSON.parse(raw) } : fallback;
  } catch {
    return fallback;
  }
}

export function save(key: string, value: unknown): void {
  if (!persist) return;
  try {
    localStorage.setItem(PREFIX + key, JSON.stringify(value));
  } catch {
    /* storage blocked: settings last for this visit only */
  }
}

export function clearAll(): void {
  try {
    Object.keys(localStorage)
      .filter((k) => k.startsWith(PREFIX))
      .forEach((k) => localStorage.removeItem(k));
  } catch {
    /* nothing to clear */
  }
}
