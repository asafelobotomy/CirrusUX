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

export default mount(App, { target: document.getElementById('app')! });
