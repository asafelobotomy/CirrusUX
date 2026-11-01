<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import StatusDot from '../lib/components/StatusDot.svelte';
  import Toggle from '../lib/components/Toggle.svelte';
  import Segmented from '../lib/components/Segmented.svelte';
  import Meter from '../lib/components/Meter.svelte';
  import Sparkline from '../lib/components/Sparkline.svelte';
  import { BUILT_IN_THEMES } from '../lib/theme/themes';
  import { themeStyle } from '../lib/state/settings.svelte';
  import { ask, toast } from '../lib/state/ui.svelte';
  import { temps } from '../lib/fake/data';
  import { CircleCheck, Moon, Plus, Power, RotateCcw, Trash2 } from '../lib/icons';

  let on = $state(true);
  let seg = $state<'a' | 'b' | 'c'>('a');

  function demoConfirm(danger: boolean) {
    ask({
      title: danger ? 'Delete the archive from 30 Oct?' : 'Restart Navidrome?',
      body: danger ? 'Permanently deletes 130 files (1.6 GB) listed in MANIFEST.tsv. This cannot be undone.' : 'Navidrome will be unavailable for a few seconds.',
      command: danger ? 'cirrusux-helper archive-approve-delete 2026-10-30' : 'docker restart 1Panel-navidrome-c3D4',
      danger, typed: danger ? 'delete' : undefined,
      confirmLabel: danger ? 'Delete permanently' : 'Restart',
      changelog: danger ? 'Deleted archive 2026-10-30 (gallery demo)' : 'Restarted Navidrome (gallery demo)',
      run: () => {},
    });
  }
</script>

<div class="stack">
  <p class="muted intro">Every component, drawn only from theme tokens. The matrix at the bottom renders the same set in each built-in theme; the build screenshots this page for visual regression tests.</p>

  <Card title="Buttons">
    <div class="row">
      <Button variant="primary">{#snippet icon()}<Power />{/snippet}Primary</Button>
      <Button>Secondary</Button>
      <Button variant="ghost">Ghost</Button>
      <Button variant="danger">{#snippet icon()}<Trash2 />{/snippet}Danger</Button>
      <Button disabled>Disabled</Button>
      <Button size="sm">Small</Button>
      <Button size="lg" variant="primary">Large</Button>
      <Button variant="ghost" aria-label="Add">{#snippet icon()}<Plus />{/snippet}</Button>
    </div>
  </Card>

  <Card title="Status" subtitle="Colour is never the only signal: every state also has a word or icon.">
    <div class="row">
      <Badge tone="ok">{#snippet icon()}<CircleCheck />{/snippet}healthy</Badge>
      <Badge tone="warn">starting</Badge>
      <Badge tone="bad">exited</Badge>
      <Badge tone="info">info</Badge>
      <Badge tone="sleep">{#snippet icon()}<Moon />{/snippet}asleep</Badge>
      <Badge tone="accent">accent</Badge>
      <Badge>neutral</Badge>
    </div>
    <div class="row dots">
      <span><StatusDot tone="ok" /> up</span>
      <span><StatusDot tone="warn" pulse /> starting</span>
      <span><StatusDot tone="bad" /> stopped</span>
      <span><StatusDot tone="sleep" /> asleep</span>
      <span><StatusDot tone="info" pulse /> waking</span>
    </div>
  </Card>

  <Card title="Inputs">
    <div class="inputs">
      <Toggle bind:checked={on} label="A toggle" hint="With a hint underneath" />
      <Segmented label="Demo" bind:value={seg} options={[{ value: 'a', label: 'One' }, { value: 'b', label: 'Two' }, { value: 'c', label: 'Three' }]} />
    </div>
  </Card>

  <Card title="Data">
    <div class="data">
      <div><span class="faint small">Meter · 68%</span><Meter value={68} max={100} label="Demo meter" /></div>
      <div><span class="faint small">Meter · warn</span><Meter value={91} max={100} tone="warn" label="Demo meter warn" /></div>
      <div class="spark"><Sparkline values={temps} label="Demo sparkline" /></div>
    </div>
  </Card>

  <Card title="Confirmations and toasts">
    <div class="row">
      <Button onclick={() => demoConfirm(false)}>{#snippet icon()}<RotateCcw />{/snippet}Normal confirm</Button>
      <Button variant="danger" onclick={() => demoConfirm(true)}>{#snippet icon()}<Trash2 />{/snippet}Irreversible (typed)</Button>
      <Button variant="ghost" onclick={() => toast('Something finished', 'ok')}>Toast</Button>
    </div>
  </Card>

  <h2 class="mh">Theme matrix</h2>
  <div class="matrix">
    {#each BUILT_IN_THEMES as t}
      <div class="cell themed" style={themeStyle(t)}>
        <div class="th"><strong>{t.name}</strong><span class="faint">{t.scheme}</span></div>
        <div class="row"><Button variant="primary" size="sm">Primary</Button><Button size="sm">Secondary</Button><Button size="sm" variant="danger">Danger</Button></div>
        <div class="row"><Badge tone="ok">healthy</Badge><Badge tone="warn">starting</Badge><Badge tone="bad">exited</Badge><Badge tone="sleep">asleep</Badge></div>
        <Meter value={70} max={100} label="{t.name} meter" />
        <p class="muted small">Muted text <span class="faint">and faint text</span> on a surface.</p>
      </div>
    {/each}
  </div>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .intro { max-width: 70ch; }
  .row { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
  .dots { margin-top: var(--space-4); gap: var(--space-5); font-size: 0.9rem; }
  .dots span { display: inline-flex; align-items: center; gap: 8px; }
  .inputs { display: flex; flex-direction: column; gap: var(--space-4); max-width: 420px; align-items: flex-start; }
  .inputs :global(label) { width: 100%; }
  .data { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: var(--space-5); align-items: end; }
  .data > div { display: flex; flex-direction: column; gap: 8px; }
  .spark { height: 60px; }
  .small { font-size: 0.8rem; }
  .mh { font-size: 1.1rem; margin-top: var(--space-3); }
  .matrix { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: var(--space-4); }
  .cell {
    display: flex; flex-direction: column; gap: var(--space-3); padding: var(--space-5);
    background: var(--surface); color: var(--text); border: 1px solid var(--border); border-radius: var(--radius-lg);
    box-shadow: 0 0 0 8px var(--bg);
  }
  .th { display: flex; justify-content: space-between; align-items: baseline; }
</style>
