<script lang="ts">
  import { has, mode, sleeps } from '../api.svelte';
  import type { Component } from 'svelte';
  import * as I from '../icons';
  import { ALL_NAV, shown } from './nav';
  import { go, ui } from '../state/ui.svelte';
  import { live } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { allThemes, settings } from '../state/settings.svelte';
  import { boards } from '../state/layout.svelte';
  import { confirmAllowSleep, confirmBackup, confirmContainer, confirmStayAwake, confirmWake } from '../state/actions';
  import { askAssistant } from '../state/assistant.svelte';

  interface Cmd { id: string; label: string; group: string; icon: Component; hint?: string; run: () => void }

  let q = $state('');
  let sel = $state(0);
  let dlg: HTMLDialogElement;
  let input: HTMLInputElement;

  const commands = $derived.by((): Cmd[] => {
    const awake = power.state === 'awake';
    const list: Cmd[] = [];
    if (power.state === 'asleep') list.push({ id: 'wake', label: `Wake ${mode.server}`, group: 'Power', icon: I.Power, run: () => confirmWake() });
    if (awake) {
      // Only what this server has: no sleep controls on one that doesn't sleep.
      if (sleeps() && has('stay-awake'))
        list.push(
          { id: 'stay30', label: 'Stay awake for 30 min', group: 'Power', icon: I.Coffee, run: () => confirmStayAwake(30) },
          { id: 'stay120', label: 'Stay awake for 2 h', group: 'Power', icon: I.Coffee, run: () => confirmStayAwake(120) },
          { id: 'allow', label: 'Allow sleep', group: 'Power', icon: I.Moon, run: () => confirmAllowSleep() },
        );
      if (has('backup')) list.push({ id: 'backup', label: 'Run a backup now', group: 'Actions', icon: I.Archive, run: () => confirmBackup() });
      for (const c of live.containers)
        list.push({ id: 'r-' + c.id, label: `${c.health === 'exited' ? 'Start' : 'Restart'} ${c.app}`, group: 'Containers', icon: c.health === 'exited' ? I.Play : I.RotateCcw, hint: c.tag, run: () => confirmContainer(c.id, c.health === 'exited' ? 'start' : 'restart') });
    }
    for (const n of ALL_NAV.filter(shown)) list.push({ id: 'go-' + n.route, label: n.label, group: 'Go to', icon: n.icon, run: () => go(n.route) });
    for (const b of boards.list) list.push({ id: 'b-' + b.id, label: `${b.name} board`, group: 'Go to', icon: I.LayoutDashboard, run: () => { boards.active = b.id; go('home'); } });
    for (const t of allThemes()) list.push({ id: 't-' + t.id, label: `Theme: ${t.name}`, group: 'Appearance', icon: t.scheme === 'dark' ? I.Moon : I.Sun, run: () => { settings.followSystem = false; settings.themeId = t.id; } });
    list.push({ id: 'edit', label: 'Edit layout', group: 'Appearance', icon: I.Pencil, run: () => { go('home'); ui.editLayout = true; } });
    return list;
  });

  const filtered = $derived.by(() => {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    const hits = commands.filter((c) => words.every((w) => `${c.label} ${c.group} ${c.hint ?? ''}`.toLowerCase().includes(w)));
    if (q.trim()) hits.push({ id: 'assistant', label: `Ask ${mode.assistant.name}: “${q.trim()}”`, group: 'Assistant', icon: I.Sparkles, run: () => askAssistant(q.trim()) });
    return hits.slice(0, 40);
  });

  $effect(() => {
    if (ui.paletteOpen && !dlg.open) { q = ''; sel = 0; dlg.showModal(); input.focus(); }
    if (!ui.paletteOpen && dlg.open) dlg.close();
  });
  $effect(() => { q; sel = 0; });

  function run(c: Cmd) {
    ui.paletteOpen = false;
    c.run();
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') { sel = Math.min(filtered.length - 1, sel + 1); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { sel = Math.max(0, sel - 1); e.preventDefault(); }
    else if (e.key === 'Enter' && filtered[sel]) { run(filtered[sel]); e.preventDefault(); }
    else return;
    queueMicrotask(() => dlg.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' }));
  }

  function globalKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      ui.paletteOpen = !ui.paletteOpen;
    }
  }
</script>

<svelte:window onkeydown={globalKey} />

<dialog bind:this={dlg} onclose={() => (ui.paletteOpen = false)} onclick={(e) => e.target === dlg && (ui.paletteOpen = false)} aria-label="Command palette">
  <div class="box">
    <div class="in">
      <I.Search size={18} />
      <input
        bind:this={input} bind:value={q} onkeydown={key}
        placeholder="Search pages, containers, actions — or ask {mode.assistant.name}"
        role="combobox" aria-expanded="true" aria-controls="cmd-list" aria-activedescendant={filtered[sel] ? 'cmd-' + filtered[sel].id : undefined}
      />
      <kbd>Esc</kbd>
    </div>
    <ul id="cmd-list" role="listbox">
      {#each filtered as c, i (c.id)}
        {@const Icon = c.icon}
        {#if i === 0 || filtered[i - 1].group !== c.group}<li class="grp" role="presentation">{c.group}</li>{/if}
        <li id="cmd-{c.id}" role="option" aria-selected={i === sel} class:sel={i === sel} onpointermove={() => (sel = i)} onclick={() => run(c)} onkeydown={() => {}}>
          <span class="ic"><Icon size={16} /></span>
          <span class="lbl">{c.label}</span>
          {#if c.hint}<code class="faint">{c.hint}</code>{/if}
        </li>
      {:else}
        <li class="empty muted">Type to search, or ask {mode.assistant.name} anything.</li>
      {/each}
    </ul>
    <footer class="faint"><span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>↵</kbd> run</span><span>Actions always ask before running</span></footer>
  </div>
</dialog>

<style>
  dialog {
    width: min(620px, calc(100vw - 24px)); margin: 12vh auto auto; padding: 0;
    border: 1px solid var(--border-strong); border-radius: var(--radius-lg);
    background: var(--surface); color: var(--text); box-shadow: 0 40px 100px -30px rgb(0 0 0 / 0.7);
  }
  dialog[open] { animation: pop var(--dur-2) var(--ease); }
  dialog::backdrop { background: rgb(5 6 10 / 0.5); backdrop-filter: blur(3px); }
  @keyframes pop { from { opacity: 0; transform: translateY(-6px) scale(0.985); } }
  .box { display: flex; flex-direction: column; max-height: min(560px, 76dvh); }
  .in { display: flex; align-items: center; gap: 10px; padding: 0 16px; height: 56px; border-bottom: 1px solid var(--border); color: var(--text-faint); }
  .in input { flex: 1; border: 0; background: transparent; outline: none; font-size: 1.02rem; color: var(--text); }
  ul { list-style: none; margin: 0; padding: 6px; overflow: auto; }
  .grp { padding: 10px 10px 4px; font-size: 0.72rem; font-weight: 650; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-faint); }
  li[role='option'] { display: flex; align-items: center; gap: 12px; height: 40px; padding: 0 10px; border-radius: var(--radius-sm); cursor: pointer; }
  li.sel { background: var(--accent-soft); }
  .ic { color: var(--text-muted); display: inline-flex; }
  .sel .ic { color: var(--accent); }
  .lbl { flex: 1; }
  .empty { padding: 16px; }
  footer { display: flex; gap: 16px; padding: 8px 14px; border-top: 1px solid var(--border); font-size: 0.75rem; }
  footer span:last-child { margin-left: auto; }
  kbd { font-family: var(--font-mono); font-size: 0.7rem; padding: 1px 5px; margin-right: 2px; border: 1px solid var(--border-strong); border-radius: 4px; }
  @media (max-width: 639px) { footer { display: none; } dialog { margin-top: 8vh; } }
</style>
