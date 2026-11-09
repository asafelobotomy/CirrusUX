// The parts of the gateway client that don't touch app state.

/** State-changing calls carry X-Cirrus-Request (the gateway's CSRF check); bodies default to JSON. */
export function requestHeaders(init: { method?: string; body?: unknown; headers?: HeadersInit }): Headers {
  const headers = new Headers(init.headers);
  if (init.method && init.method !== 'GET') headers.set('X-Cirrus-Request', '1');
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  return headers;
}

/** Only reads tell whether the answer came from the service worker's saved copy. */
export function offlineFromResponse(method: string | undefined, res: { headers: Headers }): boolean | null {
  if (method && method !== 'GET') return null;
  return res.headers.get('X-Cirrus-Offline') === '1';
}

/** 202/204 may come without a body (wake); actions answer 202 with JSON. */
export function parseBody<T>(text: string): T {
  return (text ? JSON.parse(text) : undefined) as T;
}
