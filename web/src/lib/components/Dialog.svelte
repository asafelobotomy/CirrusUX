<script lang="ts">
  import type { Snippet } from 'svelte';

  // Native <dialog>: focus trapping, Escape and the backdrop come for free.
  let {
    open = $bindable(false),
    label,
    wide = false,
    children,
  }: { open?: boolean; label: string; wide?: boolean; children: Snippet } = $props();

  let el: HTMLDialogElement;

  $effect(() => {
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  });
</script>

<dialog
  bind:this={el}
  class:wide
  aria-label={label}
  onclose={() => (open = false)}
  onclick={(e) => e.target === el && (open = false)}
>
  {#if open}<div class="inner">{@render children()}</div>{/if}
</dialog>

<style>
  dialog {
    width: min(460px, calc(100vw - 32px)); max-height: calc(100dvh - 48px);
    padding: 0; border: 1px solid var(--border-strong); border-radius: var(--radius-lg);
    background: var(--surface); color: var(--text);
    box-shadow: 0 30px 80px -20px rgb(0 0 0 / 0.55);
  }
  dialog.wide { width: min(720px, calc(100vw - 32px)); }
  dialog[open] { animation: pop var(--dur-2) var(--ease); }
  dialog::backdrop { background: rgb(5 6 10 / 0.55); backdrop-filter: blur(4px); }
  .inner { padding: var(--space-5); }
  @keyframes pop { from { opacity: 0; transform: translateY(8px) scale(0.98); } }
</style>
