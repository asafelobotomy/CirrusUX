<script lang="ts">
  import { tzLabel } from '../lib/time.svelte';
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Toggle from '../lib/components/Toggle.svelte';
  import Segmented from '../lib/components/Segmented.svelte';
  import ThemePreview from '../lib/components/ThemePreview.svelte';
  import { activeTheme, allThemes, effectiveAccent, GRADIENTS, settings } from '../lib/state/settings.svelte';
  import { boards, resetBoards } from '../lib/state/layout.svelte';
  import { go, toast } from '../lib/state/ui.svelte';
  import { api, mode } from '../lib/api.svelte';
  import { live } from '../lib/state/live.svelte';

  const uiVersion = __CIRRUSUX_VERSION__;
  const mismatch = $derived(mode.live && ((mode.version && !mode.version.startsWith(uiVersion)) || (live.agentVersion && !live.agentVersion.startsWith(uiVersion))));
  import { Check, Download, Image, Palette, RotateCcw, TriangleAlert, Upload } from '../lib/icons';

  const SWATCHES = ['#7c9cff', '#4fe0c4', '#3ecf8e', '#f0a868', '#ff7a90', '#c084fc', '#f5d04a', '#5ec8f2'];
  const acc = $derived(effectiveAccent());
  const light = $derived(allThemes().filter((t) => t.scheme === 'light'));
  const dark = $derived(allThemes().filter((t) => t.scheme === 'dark'));

  function pickTheme(id: string) {
    settings.followSystem = false;
    settings.themeId = id;
  }

  function upload(e: Event) {
    const f = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!f) return;
    if (f.size > 5 * 1024 * 1024) return toast('Images up to 5 MB, please', 'warn');
    if (mode.live) return void uploadLive(f);
    const r = new FileReader();
    r.onload = () => (settings.wallpaper = { ...settings.wallpaper, kind: 'image', value: String(r.result) });
    r.readAsDataURL(f);
    toast("Demo mode: the image isn't kept after a reload", 'info');
  }

  // Live, the image goes to the gateway on its own; settings keep only its URL.
  async function uploadLive(f: File) {
    try {
      const { savedAt } = await api<{ savedAt: number }>('/api/wallpaper', { method: 'PUT', body: f, headers: { 'Content-Type': f.type || 'application/octet-stream' } });
      settings.wallpaper = { ...settings.wallpaper, kind: 'image', value: `/api/wallpaper?v=${savedAt}` };
    } catch (e) {
      toast((e as Error).message, 'warn');
    }
  }

  function exportUi() {
    const data = JSON.stringify({ version: 1, settings: $state.snapshot(settings), boards: $state.snapshot(boards) }, null, 2);
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([data], { type: 'application/json' }));
    a.download = 'cirrusux-ui.json';
    a.click();
    URL.revokeObjectURL(a.href);
  }

  function importUi(e: Event) {
    const f = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!f) return;
    f.text().then((t) => {
      try {
        const d = JSON.parse(t);
        if (d.settings) Object.assign(settings, d.settings);
        if (d.boards?.list) boards.list = d.boards.list;
        toast('Imported settings and layouts', 'ok');
      } catch {
        toast('That file is not a CirrusUX export', 'warn');
      }
    });
  }
</script>

<div class="cols">
  <Card title="Theme" subtitle="Every colour in the panel comes from the theme.">
    {#snippet actions()}<Button size="sm" onclick={() => go('theme-editor')}>{#snippet icon()}<Palette />{/snippet}Theme editor</Button>{/snippet}
    <div class="themes" role="radiogroup" aria-label="Theme">
      {#each allThemes() as t (t.id)}
        {@const on = !settings.followSystem && settings.themeId === t.id}
        <button class="theme" class:on role="radio" aria-checked={on} onclick={() => pickTheme(t.id)}>
          <span class="pvw"><ThemePreview theme={t} /></span>
          <span class="tn">{t.name}{#if on}<Check size={14} />{/if}</span>
          <span class="faint ts">{t.scheme}{t.builtIn ? '' : ' · custom'}</span>
        </button>
      {/each}
    </div>
    <div class="sep"></div>
    <Toggle bind:checked={settings.followSystem} label="Follow the system light/dark setting" hint="Switches between the two themes below." />
    {#if settings.followSystem}
      <div class="pair">
        <label>Light <select bind:value={settings.lightThemeId}>{#each light as t}<option value={t.id}>{t.name}</option>{/each}</select></label>
        <label>Dark <select bind:value={settings.darkThemeId}>{#each dark as t}<option value={t.id}>{t.name}</option>{/each}</select></label>
      </div>
    {/if}
  </Card>

  <Card title="Accent colour" subtitle="Used for buttons, focus and highlights. Kept readable automatically.">
    <div class="sw">
      <button class="swatch def" class:on={!settings.accent} onclick={() => (settings.accent = null)} aria-label="Theme default accent" style="--c: {activeTheme().tokens.accent}">{#if !settings.accent}<Check size={14} />{/if}</button>
      {#each SWATCHES as c}
        <button class="swatch" class:on={settings.accent === c} style="--c: {c}" onclick={() => (settings.accent = c)} aria-label="Accent {c}">{#if settings.accent === c}<Check size={14} />{/if}</button>
      {/each}
      <label class="custom" title="Custom colour">
        <input type="color" value={settings.accent ?? activeTheme().tokens.accent} oninput={(e) => (settings.accent = e.currentTarget.value)} aria-label="Custom accent colour" />
      </label>
    </div>
    {#if acc.adjusted}
      <p class="adj"><TriangleAlert size={15} /> {acc.wanted} is hard to read on this theme, so it's shown as <code>{acc.color}</code> (WCAG AA, 4.5:1).</p>
    {/if}
  </Card>

  <Card title="Density and text">
    <div class="field"><span>Density</span><Segmented label="Density" bind:value={settings.density} options={[{ value: 'comfortable', label: 'Comfortable' }, { value: 'compact', label: 'Compact' }]} /></div>
    <div class="field"><span>Text size</span><Segmented label="Text size" bind:value={settings.textScale} options={[{ value: 0.9, label: 'S' }, { value: 1, label: 'M' }, { value: 1.12, label: 'L' }, { value: 1.25, label: 'XL' }]} /></div>
    <p class="faint small">Times are shown in {mode.tz}, labelled {tzLabel()}. The gateway sets the zone (CIRRUS_TZ).</p>
  </Card>

  <Card title="Wallpaper" subtitle="Cards turn glassy so it shows through, dimmed for readability.">
    <div class="wps">
      <button class="wp none" class:on={settings.wallpaper.kind === 'none'} onclick={() => (settings.wallpaper.kind = 'none')}>None</button>
      {#each GRADIENTS as g}
        <button class="wp" class:on={settings.wallpaper.kind === 'gradient' && settings.wallpaper.value === g.id} style="background: {g.css}" aria-label="Gradient {g.id}"
          onclick={() => (settings.wallpaper = { ...settings.wallpaper, kind: 'gradient', value: g.id })}></button>
      {/each}
      <label class="wp up" class:on={settings.wallpaper.kind === 'image'}>
        <Image size={18} /><span>Upload</span>
        <input type="file" accept="image/*" onchange={upload} />
      </label>
    </div>
    {#if settings.wallpaper.kind !== 'none'}
      {#if settings.wallpaper.kind === 'image'}
        <label class="range">Blur <input type="range" min="0" max="60" bind:value={settings.wallpaper.blur} /><span class="num">{settings.wallpaper.blur}px</span></label>
      {/if}
      <label class="range">Dim <input type="range" min="0" max="0.9" step="0.05" bind:value={settings.wallpaper.dim} /><span class="num">{Math.round(settings.wallpaper.dim * 100)}%</span></label>
    {/if}
  </Card>

  <Card title="Layouts and backup of your setup" subtitle={mode.live ? 'Stored on the gateway, so it follows you between devices, with version history.' : 'Demo mode: kept in this browser. The real panel stores it on the gateway, with version history.'}>
    <div class="btns">
      <Button onclick={exportUi}>{#snippet icon()}<Download />{/snippet}Export</Button>
      <label class="filebtn"><Upload size={16} /> Import<input type="file" accept="application/json" onchange={importUi} /></label>
      <Button variant="danger" onclick={() => { resetBoards(); toast('Boards reset to the defaults', 'ok'); }}>{#snippet icon()}<RotateCcw />{/snippet}Reset boards</Button>
    </div>
  </Card>

  <Card title="Custom CSS" subtitle="For anything tokens don't cover. External URLs are blocked by the content security policy.">
    <textarea bind:value={settings.customCss} spellcheck="false" placeholder={'/* e.g. */\n.cell { border-radius: 4px; }'} aria-label="Custom CSS"></textarea>
  </Card>

  <Card title="About CirrusUX">
    <dl class="about">
      <dt>Web UI</dt><dd class="mono">{uiVersion}</dd>
      <dt>Gateway</dt><dd class="mono">{mode.live ? mode.version || 'unknown' : 'none (demo data)'}</dd>
      <dt>Agent on {mode.server}</dt><dd class="mono">{mode.live ? live.agentVersion || 'no report yet' : 'none (demo data)'}</dd>
      <dt>Licence</dt><dd>GNU AGPL 3.0 or later</dd>
    </dl>
    {#if mismatch}
      <p class="adj"><TriangleAlert size={15} /> The parts run different versions. Install the same release everywhere.</p>
    {/if}
  </Card>

  <Card title={mode.assistant.name} subtitle="What may be sent to the assistant's API (decision D2 in the plan).">
    <ul class="pol">
      <li class="on"><Check size={15} /> Summaries only, with IPs, MACs and paths masked</li>
      <li><span class="b"></span> Raw log lines, only when you ask for one</li>
      <li class="never"><TriangleAlert size={15} /> Never: secrets, .env files, file contents</li>
    </ul>
  </Card>
</div>

<style>
  .cols { columns: 2 460px; column-gap: var(--space-4); }
  .cols > :global(*) { break-inside: avoid; margin-bottom: var(--space-4); }
  .themes { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: var(--space-3); }
  .theme { display: flex; flex-direction: column; gap: 6px; padding: 6px; border-radius: var(--radius); border: 1.5px solid var(--border); background: transparent; text-align: left; }
  .theme:hover { border-color: var(--border-strong); }
  .theme.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .pvw { height: 78px; display: block; }
  .tn { display: flex; align-items: center; gap: 6px; font-weight: 600; font-size: 0.9rem; padding: 0 4px; }
  .tn :global(svg) { color: var(--accent); }
  .ts { font-size: 0.75rem; padding: 0 4px 4px; text-transform: capitalize; }
  .sep { height: 1px; background: var(--border); margin: var(--space-4) 0; }
  .pair { display: flex; gap: var(--space-4); margin-top: var(--space-3); flex-wrap: wrap; }
  .pair label { display: flex; align-items: center; gap: 8px; font-size: 0.9rem; color: var(--text-muted); }
  select { height: 34px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); }
  .sw { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
  .swatch { width: 34px; height: 34px; border-radius: 50%; border: 0; background: var(--c); color: #0b0d12; display: grid; place-items: center; box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.15); }
  .swatch.on { box-shadow: 0 0 0 3px var(--bg), 0 0 0 5px var(--c); }
  .swatch.def { background: conic-gradient(var(--c) 0 50%, var(--surface-3) 0); }
  .custom { width: 34px; height: 34px; border-radius: 50%; overflow: hidden; border: 1px dashed var(--border-strong); position: relative; background: conic-gradient(red, yellow, lime, cyan, blue, magenta, red); cursor: pointer; }
  .custom input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
  .adj { display: flex; gap: 8px; align-items: flex-start; margin-top: var(--space-3); font-size: 0.86rem; color: var(--warn); }
  .field { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-3); flex-wrap: wrap; }
  .small { font-size: 0.8rem; }
  .wps { display: grid; grid-template-columns: repeat(auto-fill, minmax(78px, 1fr)); gap: 8px; }
  .wp { height: 58px; border-radius: var(--radius-sm); border: 1.5px solid var(--border); background: var(--surface-2); color: var(--text-muted); font-size: 0.82rem; display: flex; align-items: center; justify-content: center; gap: 6px; cursor: pointer; position: relative; }
  .wp.on { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .wp input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
  .range { display: grid; grid-template-columns: 3.5em 1fr 3.5em; align-items: center; gap: 10px; margin-top: var(--space-3); font-size: 0.88rem; color: var(--text-muted); }
  input[type='range'] { accent-color: var(--accent); }
  .btns { display: flex; flex-wrap: wrap; gap: 8px; }
  .filebtn { position: relative; display: inline-flex; align-items: center; gap: 8px; height: 36px; padding: 0 14px; border-radius: var(--radius-sm); background: var(--surface-2); border: 1px solid var(--border); font-weight: 550; font-size: 0.93rem; cursor: pointer; }
  .filebtn input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
  textarea { width: 100%; min-height: 120px; padding: 10px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); font-family: var(--font-mono); font-size: 0.85rem; resize: vertical; }
  .about { display: grid; grid-template-columns: auto 1fr; gap: 6px var(--space-4); margin: 0; font-size: 0.9rem; }
  .about dt { color: var(--text-muted); }
  .about dd { margin: 0; }
  .pol { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 8px; font-size: 0.9rem; color: var(--text-muted); }
  .pol li { display: flex; align-items: center; gap: 8px; }
  .pol .on { color: var(--text); } .pol .on :global(svg) { color: var(--ok); }
  .pol .never :global(svg) { color: var(--bad); }
  .b { width: 15px; height: 15px; border-radius: 50%; border: 1.5px solid var(--border-strong); display: inline-block; }
</style>
