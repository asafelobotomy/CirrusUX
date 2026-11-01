// CirrusUX service worker (PLAN §4.6). The panel opens without a connection,
// showing the last data the gateway sent, marked as saved. Only reads are
// cached; anything that changes state always goes to the network.
//
// Registered as /sw.js?v=<version>, so a new release installs a new worker
// and drops the old app shell.

const VERSION = new URL(self.location.href).searchParams.get('v') || 'dev';
const SHELL = `cirrusux-shell-${VERSION}`;
// Last-known API answers. Kept across releases: they're data, not code.
const DATA = 'cirrusux-data';
const SHELL_URLS = ['/', '/manifest.webmanifest', '/favicon.svg', '/icon-192.png', '/apple-touch-icon.png'];
// GET endpoints worth showing offline. Everything else under /api is
// network-only, and /agent is never touched.
const SAVED_API = new Set(['/api/me', '/api/state', '/api/ui', '/api/changelog']);

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL).then((c) => c.addAll(SHELL_URLS)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('cirrusux-shell-') && k !== SHELL).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== self.location.origin) return;

  if (SAVED_API.has(url.pathname)) {
    event.respondWith(networkFirst(req, DATA, true));
  } else if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/agent/')) {
    return; // network only
  } else if (req.mode === 'navigate') {
    // Hash routing: every page is "/".
    event.respondWith(networkFirst(new Request('/'), SHELL, false));
  } else if (url.pathname.startsWith('/assets/')) {
    event.respondWith(cacheFirst(req, SHELL)); // hashed names never change
  } else {
    event.respondWith(networkFirst(req, SHELL, false));
  }
});

async function networkFirst(req, cacheName, markOffline) {
  const cache = await caches.open(cacheName);
  try {
    const res = await fetch(req);
    if (res.ok) await cache.put(req, res.clone());
    return res;
  } catch (err) {
    const saved = await cache.match(req);
    if (!saved) throw err;
    return markOffline ? marked(saved) : saved;
  }
}

async function cacheFirst(req, cacheName) {
  const cache = await caches.open(cacheName);
  const saved = await cache.match(req);
  if (saved) return saved;
  const res = await fetch(req);
  if (res.ok) await cache.put(req, res.clone());
  return res;
}

// Tells the page this answer came from the cache, not the gateway.
async function marked(res) {
  const headers = new Headers(res.headers);
  headers.set('X-Cirrus-Offline', '1');
  return new Response(await res.blob(), { status: res.status, statusText: res.statusText, headers });
}
