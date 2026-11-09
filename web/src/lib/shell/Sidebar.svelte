<script lang="ts">
  import Logo from './Logo.svelte';
  import { MAIN_NAV, SECONDARY_NAV, shown } from './nav';
  import { ui } from '../state/ui.svelte';

  let { collapsed = false }: { collapsed?: boolean } = $props();
</script>

<nav class="side" class:collapsed aria-label="Main">
  <a class="brand" href="#/home" aria-label="CirrusUX home"><Logo compact={collapsed} /></a>
  <ul>
    {#each MAIN_NAV.filter(shown) as n}
      {@const Icon = n.icon}
      <li>
        <a href="#/{n.route}" class:on={ui.route === n.route} aria-current={ui.route === n.route ? 'page' : undefined} title={collapsed ? n.label : undefined}>
          <Icon size={18} /><span class="lbl">{n.label}</span>
          {#if n.phase && !collapsed}<span class="ph">{n.phase.replace('Phase ', 'P')}</span>{/if}
        </a>
      </li>
    {/each}
  </ul>
  <ul class="bottom">
    {#each SECONDARY_NAV as n}
      {@const Icon = n.icon}
      <li>
        <a href="#/{n.route}" class:on={ui.route === n.route} aria-current={ui.route === n.route ? 'page' : undefined} title={collapsed ? n.label : undefined}>
          <Icon size={18} /><span class="lbl">{n.label}</span>
        </a>
      </li>
    {/each}
  </ul>
</nav>

<style>
  .side {
    position: sticky; top: 0; height: 100dvh; width: 236px; flex: none;
    display: flex; flex-direction: column; gap: var(--space-2);
    padding: var(--space-4) var(--space-3);
    border-right: 1px solid var(--border);
    background: color-mix(in oklab, var(--surface) 70%, transparent);
    backdrop-filter: var(--card-blur, none);
  }
  .collapsed { width: 72px; align-items: center; }
  .brand { padding: 6px 8px 14px; }
  .brand:hover { text-decoration: none; }
  ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; width: 100%; }
  .bottom { margin-top: auto; }
  a:not(.brand) {
    display: flex; align-items: center; gap: 12px; height: 38px; padding: 0 12px;
    border-radius: var(--radius-sm); color: var(--text-muted); font-weight: 520; font-size: 0.93rem;
    transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  }
  a:not(.brand):hover { background: var(--hover); color: var(--text); text-decoration: none; }
  a.on { background: var(--accent-soft); color: var(--text); }
  a.on :global(svg) { color: var(--accent); }
  .collapsed a:not(.brand) { justify-content: center; padding: 0; width: 44px; }
  .collapsed .lbl { display: none; }
  .ph { margin-left: auto; font-size: 0.68rem; color: var(--text-faint); border: 1px solid var(--border); border-radius: 6px; padding: 0 5px; }
</style>
