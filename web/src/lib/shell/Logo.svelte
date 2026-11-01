<script lang="ts">
  let { compact = false }: { compact?: boolean } = $props();
</script>

<span class="logo">
  <svg viewBox="0 0 32 32" aria-hidden="true">
    <rect width="32" height="32" rx="9" fill="var(--accent)" />
    <path d="M10 20.5a4.5 4.5 0 0 1 .6-8.96A6 6 0 0 1 22 13a3.75 3.75 0 0 1 .25 7.5H10z" fill="var(--on-accent)" />
  </svg>
  {#if !compact}<span class="word">Cirrus<b>UX</b></span>{/if}
</span>

<style>
  .logo { display: inline-flex; align-items: center; gap: 10px; }
  svg { width: 30px; height: 30px; flex: none; }
  .word { font-weight: 650; font-size: 1.08rem; letter-spacing: -0.02em; }
  b { color: var(--accent); font-weight: 750; }
</style>
