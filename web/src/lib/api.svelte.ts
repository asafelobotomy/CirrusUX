// Gateway API client. Every state-changing call carries X-C64-Request,
// which the gateway requires as CSRF protection.

export const mode = $state({
  /** true when a gateway answered; false runs the demo with sample data. */
  live: false,
  user: '',
  grok: false,
  dev: false,
});

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.method && init.method !== 'GET') headers.set('X-C64-Request', '1');
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin', cache: 'no-store' });
  if (!res.ok) throw new ApiError(res.status, (await res.text()).trim() || res.statusText);
  if (res.status === 204 || res.status === 202) return undefined as T;
  return res.json() as Promise<T>;
}

/** Looks for a gateway. Anything but a proper answer means demo mode. */
export async function detectGateway(): Promise<boolean> {
  try {
    const me = await api<{ user: string; grok: boolean; dev: boolean }>('/api/me');
    Object.assign(mode, { live: true, user: me.user, grok: me.grok, dev: me.dev });
    return true;
  } catch {
    return false;
  }
}
