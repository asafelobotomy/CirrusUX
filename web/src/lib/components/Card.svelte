<script lang="ts">
  import type { Snippet } from 'svelte';
  let { title, subtitle, actions, children, pad = true }: { title?: string; subtitle?: string; actions?: Snippet; children: Snippet; pad?: boolean } = $props();
</script>

<section class="card" class:pad>
  {#if title || actions}
    <header>
      <div>
        {#if title}<h2>{title}</h2>{/if}
        {#if subtitle}<p class="muted">{subtitle}</p>{/if}
      </div>
      {#if actions}<div class="act">{@render actions()}</div>{/if}
    </header>
  {/if}
  {@render children()}
</section>

<style>
  .card {
    background: var(--card-bg, var(--surface)); border: 1px solid var(--border);
    border-radius: var(--radius-lg); backdrop-filter: var(--card-blur, none);
  }
  .pad { padding: var(--space-5); }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--space-3); margin-bottom: var(--space-4); }
  h2 { font-size: 1.05rem; }
  header p { font-size: 0.88rem; margin-top: 2px; }
  .act { display: flex; gap: var(--space-2); flex-wrap: wrap; }
</style>
