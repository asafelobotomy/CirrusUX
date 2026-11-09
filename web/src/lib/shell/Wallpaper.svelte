<script lang="ts">
  import { GRADIENTS, settings } from '../state/settings.svelte';

  const wp = $derived(settings.wallpaper);
  const bg = $derived(
    wp.kind === 'gradient' ? (GRADIENTS.find((g) => g.id === wp.value)?.css ?? '') : wp.kind === 'image' ? `center / cover no-repeat url("${wp.value}")` : '',
  );

  // Cards turn slightly glassy over a wallpaper so it shows through.
  $effect(() => {
    const root = document.documentElement.style;
    if (wp.kind === 'none') {
      root.removeProperty('--card-bg');
      root.removeProperty('--card-blur');
    } else {
      root.setProperty('--card-bg', 'color-mix(in oklab, var(--surface) 80%, transparent)');
      root.setProperty('--card-blur', 'blur(18px) saturate(1.3)');
    }
  });
</script>

{#if wp.kind !== 'none' && bg}
  <div class="wp" aria-hidden="true">
    <div class="img" style="background: {bg}; filter: blur({wp.kind === 'image' ? wp.blur : 0}px)"></div>
    <div class="dim" style="opacity: {wp.dim}"></div>
  </div>
{/if}

<style>
  .wp { position: fixed; inset: 0; z-index: -1; overflow: hidden; pointer-events: none; }
  .img { position: absolute; inset: -60px; transition: filter var(--dur-3) var(--ease); }
  .dim { position: absolute; inset: 0; background: var(--bg); transition: opacity var(--dur-3) var(--ease); }
</style>
