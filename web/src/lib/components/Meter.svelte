<script lang="ts">
  let { value, max = 1, tone = 'accent', label }: { value: number; max?: number; tone?: 'accent' | 'ok' | 'warn' | 'bad'; label: string } = $props();
  const pct = $derived(Math.max(0, Math.min(100, (value / max) * 100)));
</script>

<div class="meter" role="meter" aria-label={label} aria-valuenow={value} aria-valuemin={0} aria-valuemax={max}>
  <div class="fill {tone}" style="width: {pct}%"></div>
</div>

<style>
  .meter { height: 8px; border-radius: 999px; background: var(--surface-3); overflow: hidden; }
  .fill { height: 100%; border-radius: inherit; background: var(--c); transition: width var(--dur-3) var(--ease); }
  .accent { --c: var(--accent); } .ok { --c: var(--ok); } .warn { --c: var(--warn); } .bad { --c: var(--bad); }
</style>
