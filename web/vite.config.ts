import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';

// ../VERSION is the project's single version number (see `make bump`).
const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim();

export default defineConfig({
  plugins: [svelte()],
  define: { __CIRRUSUX_VERSION__: JSON.stringify(version) },
  build: { target: 'es2022', assetsInlineLimit: 0 },
  // With a local gateway running (make dev-gateway) the dev server uses it;
  // without one the UI falls back to demo data.
  server: { proxy: { '/api': 'http://127.0.0.1:8064' } },
});
