<script lang="ts">
  import { live } from '../state/live.svelte';
  import { mode } from '../api.svelte';
  import { power } from '../state/power.svelte';
  import { clock, fmtDuration, fmtTime, ukMidnight } from '../time.svelte';

  const dayStart = $derived(ukMidnight(new Date(clock.now)).getTime());
  const DAY = 86_400_000;

  const segs = $derived.by(() => {
    const out = live.timeline.map((s) => ({ ...s, to: s.to ?? new Date(clock.now) }));
    if (!out.length) return out;
    // Live: the snapshot's open segment stops when the reports stop.
    const tail = out[out.length - 1];
    if (mode.live && power.state !== 'awake' && tail.state === 'awake' && !live.timeline.at(-1)?.to) {
      tail.to = power.lastSnapshot;
      out.push({ from: power.lastSnapshot, to: new Date(clock.now), state: 'asleep', reason: undefined });
    }
    // The live tail follows the simulated power state.
    for (const t of power.transitions) {
      const last = out[out.length - 1];
      if (last.state === t.state) continue;
      last.to = t.at;
      out.push({ from: t.at, to: new Date(clock.now), state: t.state, reason: t.reason });
    }
    return out;
  });

  const pct = (d: Date) => ((d.getTime() - dayStart) / DAY) * 100;
  const asleepMs = $derived(segs.filter((s) => s.state === 'asleep').reduce((n, s) => n + (s.to.getTime() - s.from.getTime()), 0));
  const wakes = $derived(segs.filter((s) => s.state === 'awake').length);
</script>

<div class="tl">
  <p class="sum">
    <span><strong class="num">{fmtDuration(asleepMs)}</strong> <span class="muted">asleep today</span></span>
    <span><strong class="num">{wakes}</strong> <span class="muted">wakes</span></span>
  </p>
  <div class="track" role="list" aria-label="Today's sleep and wake periods">
    {#each segs as s}
      <div
        role="listitem"
        class="seg {s.state}"
        style="left: {pct(s.from)}%; width: {Math.max(0.25, pct(s.to) - pct(s.from))}%"
        title="{s.state === 'awake' ? 'Awake' : 'Asleep'} {fmtTime(s.from)}–{fmtTime(s.to)}{s.reason ? ' · ' + s.reason : ''}"
        aria-label="{s.state} {fmtTime(s.from)} to {fmtTime(s.to)}{s.reason ? ', ' + s.reason : ''}"
      ></div>
    {/each}
    <div class="now" style="left: {pct(new Date(clock.now))}%" aria-hidden="true"></div>
  </div>
  <div class="ticks faint" aria-hidden="true">
    {#each ['00', '06', '12', '18', '24'] as t}<span>{t}</span>{/each}
  </div>
  <div class="legend faint">
    <span><i class="sw awake"></i>Awake</span><span><i class="sw asleep"></i>Asleep</span>
  </div>
</div>

<style>
  .tl { height: 100%; display: flex; flex-direction: column; gap: 8px; justify-content: center; }
  .sum { display: flex; gap: var(--space-4); font-size: 0.9rem; }
  .track { position: relative; height: 22px; border-radius: 6px; background: var(--surface-2); overflow: hidden; }
  .seg { position: absolute; top: 0; bottom: 0; transition: filter var(--dur-1) var(--ease); }
  .seg:hover, .seg:focus-visible { filter: brightness(1.3); outline-offset: -2px; }
  .awake { background: color-mix(in oklab, var(--ok) 70%, var(--surface)); }
  .asleep { background: color-mix(in oklab, var(--sleep) 28%, var(--surface-2)); }
  .now { position: absolute; top: -2px; bottom: -2px; width: 2px; background: var(--text); border-radius: 2px; }
  .ticks { display: flex; justify-content: space-between; font-size: 0.72rem; }
  .legend { display: flex; gap: var(--space-4); font-size: 0.75rem; }
  .legend span { display: inline-flex; align-items: center; gap: 6px; }
  .sw { width: 10px; height: 10px; border-radius: 3px; display: inline-block; }
</style>
