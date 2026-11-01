<script lang="ts">
  let { values, label, tone = 'accent' }: { values: number[]; label: string; tone?: 'accent' | 'warn' | 'ok' } = $props();
  const W = 200, H = 56, P = 4;
  const path = $derived.by(() => {
    const min = Math.min(...values) - 2, max = Math.max(...values) + 2;
    const pts = values.map((v, i) => [P + (i / (values.length - 1)) * (W - 2 * P), H - P - ((v - min) / (max - min)) * (H - 2 * P)]);
    const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
    return { line, area: `${line} L${W - P},${H} L${P},${H} Z`, last: pts[pts.length - 1] };
  });
  const gid = `sg${Math.random().toString(36).slice(2, 8)}`;
</script>

<svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label={label} class={tone}>
  <defs>
    <linearGradient id={gid} x1="0" x2="0" y1="0" y2="1">
      <stop offset="0" stop-color="currentColor" stop-opacity="0.28" />
      <stop offset="1" stop-color="currentColor" stop-opacity="0" />
    </linearGradient>
  </defs>
  <path d={path.area} fill="url(#{gid})" />
  <path d={path.line} fill="none" stroke="currentColor" stroke-width="2" vector-effect="non-scaling-stroke" stroke-linejoin="round" />
  <circle cx={path.last[0]} cy={path.last[1]} r="3" fill="currentColor" />
</svg>

<style>
  svg { width: 100%; height: 100%; display: block; overflow: visible; }
  .accent { color: var(--accent); } .warn { color: var(--warn); } .ok { color: var(--ok); }
</style>
