<script lang="ts" generics="T extends string | number">
  let { value = $bindable(), options, label }: { value: T; options: { value: T; label: string }[]; label: string } = $props();
</script>

<div class="seg" role="radiogroup" aria-label={label}>
  {#each options as o (o.value)}
    <button
      type="button" role="radio" aria-checked={value === o.value}
      class:on={value === o.value} onclick={() => (value = o.value)}
    >{o.label}</button>
  {/each}
</div>

<style>
  .seg {
    display: inline-flex; padding: 3px; gap: 2px; border-radius: var(--radius-sm);
    background: var(--surface-2); border: 1px solid var(--border);
  }
  button {
    border: 0; background: transparent; color: var(--text-muted);
    height: 28px; padding: 0 12px; border-radius: calc(var(--radius-sm) - 3px);
    font-size: 0.86rem; font-weight: 550;
    transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  }
  button:hover { color: var(--text); }
  .on { background: var(--surface); color: var(--text); box-shadow: 0 1px 3px rgb(0 0 0 / 0.18); }
</style>
