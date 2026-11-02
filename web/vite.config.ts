import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';

// ../VERSION is the project's single version number (see `make bump`).
const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim();

const devCsp =
  "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; " +
  "connect-src 'self' ws://127.0.0.1:* ws://localhost:*; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";

export default defineConfig({
  plugins: [svelte()],
  define: { __CIRRUSUX_VERSION__: JSON.stringify(version) },
  build: { target: 'es2022', assetsInlineLimit: 0 },
  // With a local gateway running (make dev-gateway) the dev server uses it;
  // without one the UI falls back to demo data. In production the gateway
  // sends the CSP header (internal/gateway/server.go); this one adds only
  // the websocket Vite's hot reload needs.
  server: { proxy: { '/api': 'http://127.0.0.1:8064' }, headers: { 'Content-Security-Policy': devCsp } },
  preview: { headers: { 'Content-Security-Policy': devCsp } },
});
