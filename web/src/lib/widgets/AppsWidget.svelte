<script lang="ts">
  import * as Icons from '../icons';
  import StatusDot from '../components/StatusDot.svelte';
  import { onMount } from 'svelte';
  import { tilesFor, type AppTile } from '../apps';
  import { loadCatalogue, store } from '../state/store.svelte';
  import { live } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { confirmWake } from '../state/actions';
  import { toast } from '../state/ui.svelte';
  import { has, mode } from '../api.svelte';

  let { config }: { config?: Record<string, string> } = $props();
  const tiles = $derived(tilesFor(live.containers, store.installed, store.catalogue));
  onMount(() => {
    if (store.installed.length) void loadCatalogue();
  });
  const shown = $derived(config?.group ? tiles.filter((a) => a.group === config.group) : tiles);

  function status(app: AppTile) {
    if (power.state === 'unreachable') return { tone: 'warn' as const, word: 'no reports' };
    if (power.state === 'waking') return { tone: 'info' as const, word: 'waking' };
    if (power.state !== 'awake') return { tone: 'sleep' as const, word: 'asleep' };
    const c = live.containers.find((x) => x.name === app.id);
    if (!c || c.health === 'exited') return { tone: 'bad' as const, word: 'stopped' };
    if (c.health === 'starting') return { tone: 'warn' as const, word: 'starting' };
    return { tone: 'ok' as const, word: 'up' };
  }

  function open(e: MouseEvent, name: string) {
    if (power.state === 'asleep') {
      e.preventDefault();
      if (mode.wake) return confirmWake();
      return toast(`${mode.server} is asleep, and no wake method is set up`, 'warn');
    }
    // Demo mode has no server to open.
    if (!mode.live) {
      e.preventDefault();
      toast(`Would open ${name} on ${mode.serverHost} (demo)`, 'info');
    }
  }
</script>

{#if !shown.length}
  <p class="empty muted">{config?.group ? `No apps in ${config.group} yet.` : 'No apps yet.'} {#if has('apps')}<a href="#/apps">Open the app store</a>{/if}</p>
{/if}
<ul class="tiles">
  {#each shown as a (a.id)}
    {@const Icon = Icons[a.icon as keyof typeof Icons]}
    {@const s = status(a)}
    <li>
      <a
        href="{a.https ? 'https' : 'http'}://{mode.serverHost}:{a.port}/"
        class="tile" style="--tile: {a.color}" onclick={(e) => open(e, a.name)} target="_blank" rel="noopener noreferrer"
        aria-label="{a.name}, {s.word}"
      >
        <span class="icon"><Icon size={22} /></span>
        <span class="name">{a.name}</span>
        <span class="st"><StatusDot tone={s.tone} /><span class="word">{s.word}</span></span>
      </a>
    </li>
  {/each}
</ul>

<style>
  .empty { font-size: 0.9rem; }
  .tiles {
    list-style: none; margin: 0; padding: 0; height: 100%; overflow: auto;
    display: grid; grid-template-columns: repeat(auto-fill, minmax(80px, 1fr)); gap: var(--space-2);
    align-content: start;
  }
  .tile {
    display: flex; flex-direction: column; align-items: center; gap: 6px; text-align: center;
    padding: var(--space-3) var(--space-2); border-radius: var(--radius); color: var(--text);
    transition: background var(--dur-1) var(--ease), transform var(--dur-1) var(--ease);
  }
  .tile:hover { background: var(--hover); text-decoration: none; }
  .tile:active { transform: scale(0.97); }
  .icon {
    display: grid; place-items: center; width: 46px; height: 46px; border-radius: calc(var(--radius) * 0.9);
    color: white;
    background: linear-gradient(145deg, color-mix(in oklab, var(--tile) 85%, white), var(--tile) 55%, color-mix(in oklab, var(--tile) 70%, black));
    box-shadow: 0 8px 18px -10px var(--tile), inset 0 1px 0 rgb(255 255 255 / 0.25);
  }
  :global([data-power='asleep']) .icon { filter: saturate(0.35) brightness(0.8); }
  .name { font-size: 0.84rem; font-weight: 550; line-height: 1.2; overflow: hidden; text-overflow: ellipsis; max-width: 100%; white-space: nowrap; }
  .st { display: inline-flex; align-items: center; gap: 5px; font-size: 0.72rem; color: var(--text-faint); }
</style>
