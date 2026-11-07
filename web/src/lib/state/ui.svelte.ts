// App-wide UI state: routing, overlays, confirmations and toasts.
import { live } from './live.svelte';
import { now } from '../time.svelte';
import { mode } from '../api.svelte';

export type Route =
  | 'home' | 'services' | 'apps' | 'backups' | 'storage' | 'security'
  | 'updates' | 'media' | 'logs' | 'changelog' | 'git'
  | 'settings' | 'theme-editor' | 'gallery';

/** The page is the address's first part; the rest (ui.sub) is for the
 *  page itself, e.g. #/git/notes/commits. */
function routeFromHash(): Route {
  const r = location.hash.replace(/^#\/?/, '').split('/')[0] as Route;
  return r || 'home';
}

function subFromHash(): string {
  const h = location.hash.replace(/^#\/?/, '');
  const i = h.indexOf('/');
  return i < 0 ? '' : h.slice(i + 1);
}

export const ui = $state({
  route: routeFromHash(),
  sub: subFromHash(),
  paletteOpen: false,
  assistantOpen: false,
  editLayout: false,
  device: 'desktop' as Device,
  /** Layout used by the board: follows the board's own width, so opening
   *  the assistant panel on a laptop switches to the tablet layout. */
  boardDevice: 'desktop' as Device,
});

addEventListener('hashchange', () => {
  ui.route = routeFromHash();
  ui.sub = subFromHash();
});

export function go(route: Route, sub = ''): void {
  location.hash = '/' + route + (sub ? '/' + sub : '');
}

// ---- device class, used for per-device layouts (PLAN §4.3) ----

export type Device = 'phone' | 'tablet' | 'desktop';
const phoneQ = matchMedia('(max-width: 639px)');
const tabletQ = matchMedia('(max-width: 1099px)');
const updateDevice = () => (ui.device = phoneQ.matches ? 'phone' : tabletQ.matches ? 'tablet' : 'desktop');
phoneQ.addEventListener('change', updateDevice);
tabletQ.addEventListener('change', updateDevice);
updateDevice();

// ---- confirmations: every action shows what runs and how to undo it ----

export interface Confirm {
  title: string;
  body: string;
  command: string;
  undo?: string;
  confirmLabel: string;
  danger?: boolean;
  /** When set, the user must type this word to confirm. */
  typed?: string;
  source?: 'assistant';
  /** A file the action writes, shown in full before confirming. */
  preview?: { file: string; content: string };
  run: () => void;
  changelog: string;
}

export const confirmState = $state<{ current: Confirm | null }>({ current: null });

export function ask(c: Confirm): void {
  confirmState.current = c;
}

export function runConfirmed(): void {
  const c = confirmState.current;
  if (!c) return;
  confirmState.current = null;
  c.run();
  // Live, the gateway writes the changelog and the action reports back.
  if (mode.live) return;
  live.changelog.unshift({ at: now(), who: c.source === 'assistant' ? 'you (proposed by the assistant)' : 'you', what: c.changelog, undo: c.undo });
  toast(`${c.changelog}. Logged to CHANGELOG.`, 'ok');
}

// ---- toasts ----

export interface Toast { id: number; text: string; tone: 'ok' | 'info' | 'warn' }
export const toasts = $state<Toast[]>([]);
let toastId = 0;

export function toast(text: string, tone: Toast['tone'] = 'info'): void {
  const id = ++toastId;
  toasts.push({ id, text, tone });
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id);
    if (i >= 0) toasts.splice(i, 1);
  }, 4200);
}
