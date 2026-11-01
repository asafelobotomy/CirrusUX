import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: { target: 'es2022', assetsInlineLimit: 0 },
  // With a local gateway running (make dev-gateway) the dev server uses it;
  // without one the UI falls back to demo data.
  server: { proxy: { '/api': 'http://127.0.0.1:8064' } },
});
