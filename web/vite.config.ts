import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync, readdirSync } from 'node:fs';
import type { Plugin } from 'vite';

// ../VERSION is the project's single version number (see `make bump`).
const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim();

// The app store's catalogue for demo mode, read from the same JSON files
// the Go binaries embed (internal/catalogue/apps), so there is one source.
function catalogue(): Plugin {
  const id = 'virtual:catalogue';
  const dir = new URL('../internal/catalogue/apps/', import.meta.url);
  return {
    name: 'cirrusux-catalogue',
    resolveId: (i) => (i === id ? '\0' + id : undefined),
    load(i) {
      if (i !== '\0' + id) return;
      const apps = readdirSync(dir)
        .filter((f) => f.endsWith('.json'))
        .sort()
        .map((f) => {
          const file = new URL(f, dir);
          this.addWatchFile(file.pathname);
          return JSON.parse(readFileSync(file, 'utf8'));
        });
      return `export default ${JSON.stringify(apps)};`;
    },
  };
}

const devCsp =
  "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; " +
  "connect-src 'self' ws://127.0.0.1:* ws://localhost:*; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";

export default defineConfig({
  plugins: [svelte(), catalogue()],
  define: { __CIRRUSUX_VERSION__: JSON.stringify(version) },
  build: { target: 'es2022', assetsInlineLimit: 0 },
  // With a local gateway running (make dev-gateway) the dev server uses it;
  // without one the UI falls back to demo data. In production the gateway
  // sends the CSP header (internal/gateway/server.go); this one adds only
  // the websocket Vite's hot reload needs.
  // The proxy presents the gateway's own origin, so its same-origin check
  // (on every change) passes for requests made through the dev server.
  server: {
    proxy: { '/api': { target: 'http://127.0.0.1:8064', changeOrigin: true, headers: { Origin: 'http://127.0.0.1:8064' } } },
    headers: { 'Content-Security-Policy': devCsp },
  },
  preview: { headers: { 'Content-Security-Policy': devCsp } },
});
