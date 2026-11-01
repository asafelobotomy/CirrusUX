<script lang="ts">
  // A miniature of the panel drawn with one theme's tokens, scoped by
  // inline custom properties so several themes can sit side by side.
  import { themeStyle } from '../state/settings.svelte';
  import type { Theme } from '../theme/themes';

  let { theme, accent }: { theme: Theme; accent?: string } = $props();
</script>

<div class="pv themed" style={themeStyle(theme, accent ?? theme.tokens.accent)} aria-hidden="true">
  <div class="side"><i class="logo"></i><i></i><i class="on"></i><i></i></div>
  <div class="main">
    <div class="card hero"><i class="orb"></i><div><b></b><s></s></div></div>
    <div class="row">
      <div class="card"><b class="ok"></b><s></s></div>
      <div class="card"><b class="sleep"></b><s></s></div>
    </div>
  </div>
</div>

<style>
  .pv { display: flex; gap: 6px; padding: 8px; height: 100%; background: var(--bg); border-radius: calc(var(--radius) * 0.7); }
  .side { width: 18%; display: flex; flex-direction: column; gap: 5px; }
  .side i { height: 6px; border-radius: 3px; background: var(--surface-3); }
  .side i.logo { width: 12px; height: 12px; border-radius: 4px; background: var(--accent); margin-bottom: 3px; }
  .side i.on { background: var(--accent-soft); box-shadow: inset 2px 0 0 var(--accent); }
  .main { flex: 1; display: flex; flex-direction: column; gap: 6px; }
  .card { flex: 1; background: var(--surface); border: 1px solid var(--border); border-radius: calc(var(--radius) * 0.45); padding: 6px; display: flex; flex-direction: column; gap: 4px; }
  .hero { flex-direction: row; align-items: center; gap: 6px; }
  .hero div { flex: 1; display: flex; flex-direction: column; gap: 4px; }
  .orb { width: 16px; height: 16px; border-radius: 50%; background: var(--ok); box-shadow: 0 0 10px var(--ok); }
  .row { flex: 1; display: flex; gap: 6px; }
  b { display: block; height: 6px; width: 60%; border-radius: 3px; background: var(--text); }
  s { display: block; height: 4px; width: 40%; border-radius: 2px; background: var(--text-faint); }
  b.ok { background: var(--ok); width: 35%; }
  b.sleep { background: var(--sleep); width: 35%; }
</style>
