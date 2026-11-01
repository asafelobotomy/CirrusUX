<script lang="ts">
  import type { Snippet } from 'svelte';
  type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'sleep' | 'accent' | 'neutral';
  let { tone = 'neutral', icon, children }: { tone?: Tone; icon?: Snippet; children: Snippet } = $props();
</script>

<span class="badge {tone}">
  {#if icon}<span class="i" aria-hidden="true">{@render icon()}</span>{/if}
  {@render children()}
</span>

<style>
  .badge {
    display: inline-flex; align-items: center; gap: 5px;
    height: 22px; padding: 0 8px; border-radius: 999px;
    font-size: 0.78rem; font-weight: 600; white-space: nowrap;
    background: var(--hover); color: var(--text-muted);
  }
  .i { display: inline-flex; }
  .i :global(svg) { width: 13px; height: 13px; }
  .ok { background: var(--ok-soft); color: var(--ok); }
  .warn { background: var(--warn-soft); color: var(--warn); }
  .bad { background: var(--bad-soft); color: var(--bad); }
  .info { background: color-mix(in oklab, var(--info) 16%, transparent); color: var(--info); }
  .sleep { background: var(--sleep-soft); color: var(--sleep); }
  .accent { background: var(--accent-soft); color: var(--accent); }
</style>
