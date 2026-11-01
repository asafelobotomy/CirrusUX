<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';

  interface Props extends HTMLButtonAttributes {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    size?: 'sm' | 'md' | 'lg';
    icon?: Snippet;
    children?: Snippet;
  }
  let { variant = 'secondary', size = 'md', icon, children, class: cls = '', ...rest }: Props = $props();
</script>

<button class="btn {variant} {size} {cls}" class:icon-only={!children} {...rest}>
  {#if icon}<span class="ico" aria-hidden="true">{@render icon()}</span>{/if}
  {#if children}<span>{@render children()}</span>{/if}
</button>

<style>
  .btn {
    display: inline-flex; align-items: center; justify-content: center; gap: var(--space-2);
    border: 1px solid transparent; border-radius: var(--radius-sm);
    font-weight: 550; white-space: nowrap; line-height: 1;
    transition: background var(--dur-1) var(--ease), border-color var(--dur-1) var(--ease),
      transform var(--dur-1) var(--ease), box-shadow var(--dur-2) var(--ease);
  }
  .btn:active:not(:disabled) { transform: scale(0.97); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .sm { height: 30px; padding: 0 10px; font-size: 0.86rem; }
  .md { height: 36px; padding: 0 14px; font-size: 0.93rem; }
  .lg { height: 48px; padding: 0 22px; font-size: 1.02rem; border-radius: var(--radius); }
  .icon-only.sm { width: 30px; padding: 0; }
  .icon-only.md { width: 36px; padding: 0; }
  .icon-only.lg { width: 48px; padding: 0; }
  .ico { display: inline-flex; }
  .ico :global(svg) { width: 1.1em; height: 1.1em; }
  .lg .ico :global(svg) { width: 1.25em; height: 1.25em; }

  .primary {
    background: var(--accent); color: var(--on-accent);
    box-shadow: 0 1px 0 color-mix(in oklab, white 25%, transparent) inset, 0 6px 18px -8px var(--accent);
  }
  .primary:hover:not(:disabled) { background: color-mix(in oklab, var(--accent) 88%, white); }
  .secondary { background: var(--surface-2); border-color: var(--border); color: var(--text); }
  .secondary:hover:not(:disabled) { background: var(--surface-3); border-color: var(--border-strong); }
  .ghost { background: transparent; color: var(--text-muted); }
  .ghost:hover:not(:disabled) { background: var(--hover); color: var(--text); }
  /* Destructive actions never look like the main action (PLAN §4.1.6). */
  .danger { background: transparent; border-color: color-mix(in oklab, var(--bad) 55%, transparent); color: var(--bad); }
  .danger:hover:not(:disabled) { background: var(--bad-soft); }
</style>
