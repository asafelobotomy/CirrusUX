import { mount } from 'svelte';
import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import './lib/styles/base.css';
import App from './App.svelte';
import { detectGateway } from './lib/api.svelte';
import { startLive } from './lib/sync.svelte';

// With a gateway the panel shows cloud64's real state; without one it runs
// the design prototype on sample data.
if (await detectGateway()) await startLive();

// Installable app with an offline shell (PLAN §4.6). Production builds only:
// in development the worker's caching would hide changes.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register(`/sw.js?v=${encodeURIComponent(__CIRRUSUX_VERSION__)}`).catch(() => {
    /* the panel works without it */
  });
}

export default mount(App, { target: document.getElementById('app')! });
