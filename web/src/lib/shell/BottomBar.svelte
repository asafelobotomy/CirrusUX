<script lang="ts">
  import { mode } from '../api.svelte';
  import { ui } from '../state/ui.svelte';
  import { House, Package, Search, Settings, Sparkles } from '../icons';
</script>

<nav class="bb" aria-label="Main">
  <a href="#/home" class:on={ui.route === 'home'}><House size={21} /><span>Home</span></a>
  <a href="#/services" class:on={ui.route === 'services'}><Package size={21} /><span>Services</span></a>
  <button class="assistant" onclick={() => (ui.assistantOpen = true)} aria-label="Ask {mode.assistant.name}"><Sparkles size={22} /></button>
  <button onclick={() => (ui.paletteOpen = true)}><Search size={21} /><span>Search</span></button>
  <a href="#/settings" class:on={ui.route === 'settings' || ui.route === 'theme-editor'}><Settings size={21} /><span>Settings</span></a>
</nav>

<style>
  .bb {
    position: fixed; z-index: 30; left: 0; right: 0; bottom: 0;
    display: grid; grid-template-columns: repeat(5, 1fr); align-items: center;
    padding: 6px 8px calc(6px + env(safe-area-inset-bottom));
    background: color-mix(in oklab, var(--surface) 85%, transparent);
    backdrop-filter: blur(18px) saturate(1.4);
    border-top: 1px solid var(--border);
  }
  a, button {
    display: flex; flex-direction: column; align-items: center; gap: 3px; padding: 6px 0;
    border: 0; background: transparent; color: var(--text-faint); font-size: 0.7rem; font-weight: 560;
  }
  a:hover { text-decoration: none; }
  .on { color: var(--accent); }
  .assistant {
    justify-self: center; width: 52px; height: 52px; border-radius: 18px; margin-top: -22px;
    background: var(--accent); color: var(--on-accent); justify-content: center;
    box-shadow: 0 10px 24px -8px var(--accent), 0 0 0 4px var(--bg);
  }
</style>
