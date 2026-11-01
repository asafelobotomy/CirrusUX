<script lang="ts">
  import Button from '../components/Button.svelte';
  import PowerPill from './PowerPill.svelte';
  import Logo from './Logo.svelte';
  import { ALL_NAV } from './nav';
  import { ui } from '../state/ui.svelte';
  import { activeBoard, addBoard, boards } from '../state/layout.svelte';
  import { Check, Command, FlaskConical, Pencil, Plus, Search, Sparkles } from '../icons';
  import { mode } from '../api.svelte';

  let { onAddWidget }: { onAddWidget: () => void } = $props();
  const title = $derived(ALL_NAV.find((n) => n.route === ui.route)?.label ?? '');
  const phone = $derived(ui.device === 'phone');
  const mac = /Mac|iPhone|iPad/.test(navigator.platform);

  function newBoard() {
    const name = prompt('Name for the new board', 'My board');
    if (name?.trim()) addBoard(name.trim());
  }
</script>

<header class="top" class:phone>
  {#if phone}<a href="#/home" class="brand" aria-label="Home"><Logo compact /></a>{/if}

  {#if ui.route === 'home'}
    <div class="tabs" role="tablist" aria-label="Boards">
      {#each boards.list as b (b.id)}
        <button role="tab" aria-selected={boards.active === b.id} class:on={boards.active === b.id} onclick={() => (boards.active = b.id)}>{b.name}</button>
      {/each}
      {#if ui.editLayout}
        <button class="add" onclick={newBoard} aria-label="Add board"><Plus size={15} /></button>
      {/if}
    </div>
  {:else}
    <h1>{title}</h1>
  {/if}

  <div class="right">
    {#if !mode.live && !phone}
      <span class="demo" title="No gateway answered, so the panel shows sample data"><FlaskConical size={13} />Demo data</span>
    {/if}
    {#if ui.route === 'home' && ui.editLayout}
      <Button size="sm" variant="secondary" onclick={onAddWidget}>{#snippet icon()}<Plus />{/snippet}{phone ? '' : 'Add widget'}</Button>
    {/if}
    {#if ui.route === 'home'}
      <Button
        size="sm" variant={ui.editLayout ? 'primary' : 'ghost'} aria-pressed={ui.editLayout}
        aria-label={ui.editLayout ? 'Done editing layout' : `Edit ${activeBoard().name} layout`}
        onclick={() => (ui.editLayout = !ui.editLayout)}
      >{#snippet icon()}{#if ui.editLayout}<Check />{:else}<Pencil />{/if}{/snippet}{phone ? '' : ui.editLayout ? 'Done' : 'Edit layout'}</Button>
    {/if}
    {#if !phone}
      <button class="search" onclick={() => (ui.paletteOpen = true)}>
        <Search size={15} /><span>Search or run…</span><kbd>{#if mac}<Command size={11} />{:else}Ctrl{/if} K</kbd>
      </button>
      <Button variant="ghost" aria-label="Ask Grok" onclick={() => (ui.grokOpen = !ui.grokOpen)}>{#snippet icon()}<Sparkles />{/snippet}</Button>
    {/if}
    <PowerPill />
  </div>
</header>

{#if ui.editLayout && ui.route === 'home'}
  <p class="hint">Editing the <strong>{ui.boardDevice}</strong> layout of <strong>{activeBoard().name}</strong>. Drag the grip to move, the corner to resize. Phone, tablet and desktop each keep their own layout.</p>
{/if}

<style>
  .top {
    position: sticky; top: 0; z-index: 20;
    display: flex; align-items: center; gap: var(--space-4);
    padding: var(--space-3) var(--space-6); min-height: 64px;
    background: color-mix(in oklab, var(--bg) 78%, transparent);
    backdrop-filter: blur(16px) saturate(1.3);
    border-bottom: 1px solid color-mix(in oklab, var(--border) 60%, transparent);
  }
  .top.phone { padding: var(--space-2) var(--space-4); gap: var(--space-2); min-height: 56px; }
  h1 { font-size: 1.3rem; letter-spacing: -0.02em; }
  .tabs { display: flex; gap: 4px; overflow-x: auto; min-width: 0; scrollbar-width: none; }
  .tabs button {
    border: 0; background: transparent; color: var(--text-muted); white-space: nowrap;
    height: 34px; padding: 0 14px; border-radius: 999px; font-weight: 560; font-size: 0.93rem;
  }
  .tabs button:hover { color: var(--text); background: var(--hover); }
  .tabs button.on { color: var(--text); background: var(--surface-2); box-shadow: inset 0 0 0 1px var(--border); }
  .tabs .add { width: 34px; padding: 0; display: grid; place-items: center; }
  .right { margin-left: auto; display: flex; align-items: center; gap: var(--space-2); flex: none; }
  .demo { display: inline-flex; align-items: center; gap: 5px; height: 26px; padding: 0 10px; border-radius: 999px; font-size: 0.76rem; font-weight: 600; color: var(--warn); background: var(--warn-soft); }
  .search {
    display: inline-flex; align-items: center; gap: 8px; height: 34px; padding: 0 8px 0 12px;
    border-radius: var(--radius-sm); border: 1px solid var(--border); background: var(--surface-2); color: var(--text-faint);
    font-size: 0.86rem; min-width: 210px;
  }
  .search:hover { border-color: var(--border-strong); color: var(--text-muted); }
  .search span { flex: 1; text-align: left; }
  kbd { display: inline-flex; align-items: center; gap: 2px; font-size: 0.72rem; padding: 2px 6px; border-radius: 5px; border: 1px solid var(--border-strong); color: var(--text-muted); }
  .hint {
    margin: var(--space-4) var(--space-6) 0; padding: var(--space-2) var(--space-3); border-radius: var(--radius-sm);
    background: var(--accent-soft); color: var(--text-muted); font-size: 0.86rem;
  }
  @media (max-width: 1099px) { .search { min-width: 0; } .search span, .search kbd { display: none; } }
  @media (max-width: 639px) { .hint { margin: var(--space-3) var(--space-4) 0; } }
</style>
