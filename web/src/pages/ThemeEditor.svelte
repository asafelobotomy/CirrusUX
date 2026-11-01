<script lang="ts">
  import { onDestroy } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import ThemePreview from '../lib/components/ThemePreview.svelte';
  import { activeTheme, allThemes, editor, settings, themeById } from '../lib/state/settings.svelte';
  import { TOKEN_GROUPS, TOKEN_NAMES, type Theme } from '../lib/theme/themes';
  import { contrast } from '../lib/theme/contrast';
  import { toast } from '../lib/state/ui.svelte';
  import { Download, Pencil, Trash2, Upload } from '../lib/icons';

  let baseId = $state(activeTheme().id);
  let dirty = $state(false);
  const draft = $derived(editor.draft);

  function start() {
    const base = themeById(baseId);
    editor.draft = {
      ...structuredClone($state.snapshot(base)),
      id: base.builtIn ? `custom-${Date.now().toString(36)}` : base.id,
      name: base.builtIn ? `${base.name} (mine)` : base.name,
      builtIn: false,
    };
    dirty = false;
  }

  function setToken(name: string, value: string) {
    if (!editor.draft || !/^#[0-9a-f]{6}$/i.test(value)) return;
    editor.draft.tokens[name as keyof Theme['tokens']] = value;
    dirty = true;
  }

  function saveTheme() {
    const t = $state.snapshot(editor.draft) as Theme;
    const i = settings.customThemes.findIndex((x) => x.id === t.id);
    if (i >= 0) settings.customThemes[i] = t;
    else settings.customThemes.push(t);
    settings.followSystem = false;
    settings.themeId = t.id;
    settings.accent = null;
    editor.draft = null;
    dirty = false;
    toast(`Saved “${t.name}” and switched to it`, 'ok');
  }

  function remove() {
    if (!draft || !confirm(`Delete the theme “${draft.name}”?`)) return;
    settings.customThemes = settings.customThemes.filter((t) => t.id !== draft.id);
    if (settings.themeId === draft.id) settings.themeId = 'graphite';
    editor.draft = null;
  }

  function exportTheme() {
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([JSON.stringify($state.snapshot(editor.draft), null, 2)], { type: 'application/json' }));
    a.download = `${draft?.name.replace(/\W+/g, '-').toLowerCase()}.theme.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  function importTheme(e: Event) {
    (e.currentTarget as HTMLInputElement).files?.[0]?.text().then((text) => {
      try {
        const t = JSON.parse(text) as Theme;
        if (!TOKEN_NAMES.every((n) => /^#[0-9a-f]{6}$/i.test(t.tokens?.[n] ?? ''))) throw new Error();
        editor.draft = { ...t, id: `custom-${Date.now().toString(36)}`, builtIn: false, radius: Number(t.radius) || 14 };
        dirty = true;
      } catch {
        toast('That file is not a valid theme', 'warn');
      }
    });
  }

  onDestroy(() => {
    if (editor.draft && dirty) toast('Unsaved theme changes were discarded', 'warn');
    editor.draft = null;
  });

  const checks = $derived(
    draft
      ? [
          { label: 'Text on surface', a: draft.tokens.text, b: draft.tokens.surface, min: 4.5 },
          { label: 'Muted text on surface', a: draft.tokens['text-muted'], b: draft.tokens.surface, min: 4.5 },
          { label: 'Faint text on surface', a: draft.tokens['text-faint'], b: draft.tokens.surface, min: 3 },
          { label: 'Accent on surface', a: draft.tokens.accent, b: draft.tokens.surface, min: 4.5 },
          { label: 'Button text on accent', a: draft.tokens['on-accent'], b: draft.tokens.accent, min: 4.5 },
          { label: 'Success on surface', a: draft.tokens.ok, b: draft.tokens.surface, min: 3 },
          { label: 'Error on surface', a: draft.tokens.bad, b: draft.tokens.surface, min: 3 },
        ]
      : [],
  );
</script>

{#if !draft}
  <Card title="Make a theme" subtitle="Start from any theme. The whole panel previews your changes live while you edit.">
    <div class="pick">
      {#each allThemes() as t (t.id)}
        <button class="opt" class:on={baseId === t.id} onclick={() => (baseId = t.id)} aria-pressed={baseId === t.id}>
          <span class="pv"><ThemePreview theme={t} /></span>
          <span>{t.name}</span>
        </button>
      {/each}
    </div>
    <div class="row">
      <Button variant="primary" onclick={start}>{#snippet icon()}<Pencil />{/snippet}{themeById(baseId).builtIn ? 'Edit a copy' : 'Edit'}</Button>
      <label class="filebtn"><Upload size={16} /> Import a theme file<input type="file" accept="application/json" onchange={importTheme} /></label>
    </div>
  </Card>
{:else}
  <div class="ed">
    <div class="tokens">
      <Card title="Editing" subtitle="Live preview: the whole panel uses this theme until you save or leave.">
        <label class="name">Name <input bind:value={draft.name} oninput={() => (dirty = true)} /></label>
        <label class="name">Scheme
          <select bind:value={draft.scheme} onchange={() => (dirty = true)}><option value="dark">Dark</option><option value="light">Light</option></select>
        </label>
        <label class="range">Corner radius <input type="range" min="0" max="24" bind:value={draft.radius} oninput={() => (dirty = true)} /><span class="num">{draft.radius}px</span></label>
      </Card>
      {#each Object.entries(TOKEN_GROUPS) as [group, names]}
        <Card title={group}>
          <div class="grid">
            {#each names as n}
              <label class="tok">
                <span class="sw"><input type="color" value={draft.tokens[n]} oninput={(e) => setToken(n, e.currentTarget.value)} aria-label="{n} colour" /></span>
                <span class="tn">{n}</span>
                <input class="hex mono" value={draft.tokens[n]} onchange={(e) => setToken(n, e.currentTarget.value)} aria-label="{n} hex value" />
              </label>
            {/each}
          </div>
        </Card>
      {/each}
    </div>

    <div class="aside">
      <Card title="Readability" subtitle="WCAG 2.2 contrast checks">
        <ul class="checks">
          {#each checks as c}
            {@const r = contrast(c.a, c.b)}
            <li>
              <span class="pair" style="background: {c.b}; color: {c.a}">Aa</span>
              <span class="cl">{c.label}</span>
              <span class="num faint">{r.toFixed(1)}:1</span>
              <Badge tone={r >= c.min ? 'ok' : 'bad'}>{r >= c.min ? 'pass' : `needs ${c.min}`}</Badge>
            </li>
          {/each}
        </ul>
      </Card>
      <Card>
        <div class="row col">
          <Button variant="primary" onclick={saveTheme}>Save and use</Button>
          <Button onclick={exportTheme}>{#snippet icon()}<Download />{/snippet}Export file</Button>
          <Button variant="ghost" onclick={() => { editor.draft = null; dirty = false; }}>Cancel</Button>
          {#if settings.customThemes.some((t) => t.id === draft.id)}
            <Button variant="danger" onclick={remove}>{#snippet icon()}<Trash2 />{/snippet}Delete theme</Button>
          {/if}
        </div>
      </Card>
    </div>
  </div>
{/if}

<style>
  .pick { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: var(--space-3); }
  .opt { display: flex; flex-direction: column; gap: 6px; padding: 6px; border-radius: var(--radius); border: 1.5px solid var(--border); background: transparent; font-weight: 600; font-size: 0.9rem; text-align: left; }
  .opt.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .pv { height: 86px; display: block; }
  .row { display: flex; gap: var(--space-2); flex-wrap: wrap; margin-top: var(--space-4); align-items: center; }
  .row.col { flex-direction: column; align-items: stretch; margin-top: 0; }
  .ed { display: grid; grid-template-columns: 1fr 340px; gap: var(--space-4); align-items: start; }
  .tokens { display: flex; flex-direction: column; gap: var(--space-4); }
  .aside { position: sticky; top: 84px; display: flex; flex-direction: column; gap: var(--space-4); }
  @media (max-width: 1099px) { .ed { grid-template-columns: 1fr; } .aside { position: static; } }
  .name { display: flex; align-items: center; gap: 10px; margin-bottom: var(--space-3); color: var(--text-muted); font-size: 0.9rem; }
  .name input, .name select { flex: 1; max-width: 320px; height: 36px; padding: 0 10px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); }
  .range { display: grid; grid-template-columns: 8em 1fr 3em; align-items: center; gap: 10px; font-size: 0.9rem; color: var(--text-muted); }
  input[type='range'] { accent-color: var(--accent); }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: var(--space-2); }
  .tok { display: grid; grid-template-columns: 34px 1fr 6.2em; align-items: center; gap: 10px; padding: 6px; border-radius: var(--radius-sm); background: var(--surface-2); border: 1px solid var(--border); }
  .sw { position: relative; width: 34px; height: 34px; border-radius: 9px; overflow: hidden; box-shadow: inset 0 0 0 1px var(--border-strong); }
  .sw input { position: absolute; inset: -8px; width: 50px; height: 50px; border: 0; padding: 0; cursor: pointer; }
  .tn { font-size: 0.86rem; font-weight: 550; }
  .hex { width: 100%; height: 30px; padding: 0 6px; border-radius: 7px; border: 1px solid var(--border); background: var(--surface); font-size: 0.8rem; }
  .checks { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 8px; }
  .checks li { display: grid; grid-template-columns: 38px 1fr auto auto; align-items: center; gap: 8px; font-size: 0.84rem; }
  .pair { display: grid; place-items: center; height: 28px; border-radius: 7px; font-weight: 700; border: 1px solid var(--border); }
  .filebtn { position: relative; display: inline-flex; align-items: center; gap: 8px; height: 36px; padding: 0 14px; border-radius: var(--radius-sm); background: var(--surface-2); border: 1px solid var(--border); font-weight: 550; font-size: 0.93rem; cursor: pointer; }
  .filebtn input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
</style>
