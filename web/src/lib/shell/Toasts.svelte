<script lang="ts">
  import { flip } from 'svelte/animate';
  import { fly } from 'svelte/transition';
  import { toasts } from '../state/ui.svelte';
  import { CircleCheck, Info, TriangleAlert } from '../icons';
</script>

<div class="toasts" role="status" aria-live="polite">
  {#each toasts as t (t.id)}
    <div class="toast {t.tone}" animate:flip={{ duration: 200 }} in:fly={{ y: 16, duration: 220 }} out:fly={{ y: 8, duration: 160 }}>
      {#if t.tone === 'ok'}<CircleCheck size={18} />{:else if t.tone === 'warn'}<TriangleAlert size={18} />{:else}<Info size={18} />{/if}
      <span>{t.text}</span>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed; z-index: 60; right: 16px; bottom: calc(16px + env(safe-area-inset-bottom));
    display: flex; flex-direction: column; gap: 8px; align-items: flex-end; pointer-events: none;
  }
  .toast {
    display: flex; align-items: center; gap: 10px; max-width: min(420px, calc(100vw - 32px));
    padding: 10px 14px; border-radius: var(--radius); font-size: 0.92rem;
    background: var(--surface-3); border: 1px solid var(--border-strong);
    box-shadow: 0 12px 30px -12px rgb(0 0 0 / 0.5);
  }
  .ok :global(svg) { color: var(--ok); } .warn :global(svg) { color: var(--warn); } .info :global(svg) { color: var(--info); }
  .toast :global(svg) { flex: none; }
  @media (max-width: 639px) { .toasts { bottom: calc(84px + env(safe-area-inset-bottom)); right: 16px; left: 16px; align-items: stretch; } }
</style>
