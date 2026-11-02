// Gateway API client. Every state-changing call carries X-Cirrus-Request,
// which the gateway requires as CSRF protection.

export const mode = $state({
  /** true when a gateway answered; false runs the demo with sample data. */
  live: false,
  user: '',
  dev: false,
  /** The managed server's name, and the host its apps open on. */
  server: 'homeserver',
  serverHost: 'homeserver',
  /** The machine the gateway runs on (split setup). */
  gateway: 'gateway',
  /** single: one machine that doesn't sleep; split: a sleeping server and an always-on gateway. */
  setup: 'split' as 'single' | 'split',
  /** IANA time zone every time is shown in. */
  tz: 'Europe/London',
  /** A wake method is set up. */
  wake: true,
  assistant: { enabled: true, name: 'Assistant' },
  /** Modules the server has (sleep, stay-awake, backup, av, security, egress); demo has all. */
  features: ['sleep', 'stay-awake', 'backup', 'av', 'security', 'egress'] as string[],
  /** The gateway's version, e.g. "0.1.0 (a1b2c3d)". */
  version: '',
  /** Showing answers the service worker saved, because the gateway is out of reach. */
  offline: false,
});

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.method && init.method !== 'GET') headers.set('X-Cirrus-Request', '1');
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin', cache: 'no-store' });
  if (!init.method || init.method === 'GET') mode.offline = res.headers.get('X-Cirrus-Offline') === '1';
  if (!res.ok) throw new ApiError(res.status, (await res.text()).trim() || res.statusText);
  // 202/204 may come without a body (wake); actions answer 202 with JSON.
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

/** Whether the server has a module. */
export const has = (mod: string): boolean => mode.features.includes(mod);

/** The server sleeps: a split setup with a sleep module. */
export const sleeps = (): boolean => mode.setup === 'split' && has('sleep');

interface Me {
  user: string; dev: boolean; version: string; server: string; serverHost: string; gateway: string;
  setup: 'single' | 'split'; tz: string; wake: boolean; assistant: { enabled: boolean; name: string };
}

/** Looks for a gateway. Anything but a proper answer means demo mode. */
export async function detectGateway(): Promise<boolean> {
  try {
    const me = await api<Me>('/api/me');
    Object.assign(mode, {
      live: true, user: me.user, dev: me.dev, version: me.version, server: me.server, serverHost: me.serverHost,
      gateway: me.gateway, setup: me.setup, tz: me.tz || 'UTC', wake: me.wake, assistant: me.assistant,
      features: [],
    });
    return true;
  } catch {
    return false;
  }
}
