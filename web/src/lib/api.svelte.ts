// Gateway API client. Every state-changing call carries X-Cirrus-Request,
// which the gateway requires as CSRF protection.

export const mode = $state({
  /** true when a gateway answered; false runs the demo with sample data. */
  live: false,
  user: '',
  grok: false,
  dev: false,
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
  if (res.status === 204 || res.status === 202) return undefined as T;
  return res.json() as Promise<T>;
}

/** Looks for a gateway. Anything but a proper answer means demo mode. */
export async function detectGateway(): Promise<boolean> {
  try {
    const me = await api<{ user: string; grok: boolean; dev: boolean; version: string }>('/api/me');
    Object.assign(mode, { live: true, user: me.user, grok: me.grok, dev: me.dev, version: me.version });
    return true;
  } catch {
    return false;
  }
}
