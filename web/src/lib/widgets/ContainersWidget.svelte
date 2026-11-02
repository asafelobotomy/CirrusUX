<script lang="ts">
  import StatusDot from '../components/StatusDot.svelte';
  import Badge from '../components/Badge.svelte';
  import Button from '../components/Button.svelte';
  import { live } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { confirmContainer } from '../state/actions';
  import { clock, fmtDuration } from '../time.svelte';
  import { Play, RotateCcw, Square } from '../icons';
  import type { Health } from '../state/live.svelte';

  let { full = false }: { full?: boolean } = $props();

  const tone = (h: Health) => (h === 'healthy' || h === 'running' ? 'ok' : h === 'starting' ? 'warn' : 'bad') as 'ok' | 'warn' | 'bad';
  const awake = $derived(power.state === 'awake');
</script>

<ul class="list" class:full>
  {#each live.containers as c (c.id)}
    <li class="row">
      <StatusDot tone={awake ? tone(c.health) : 'neutral'} pulse={c.health === 'starting'} />
      <div class="main">
        <div class="name">
          {c.app}
          {#if c.leftover}<Badge tone="warn">leftover</Badge>{/if}
        </div>
        <div class="meta faint">
          <code>{c.tag}</code>
          {#if c.port}<span>:{c.port}</span>{/if}
          {#if full}<span class="mono">{c.name}</span>{/if}
        </div>
      </div>
      <div class="state num">
        <span class="h">{c.health}</span>
        <span class="faint">{c.upSince ? fmtDuration(clock.now - c.upSince.getTime()) : '—'}</span>
      </div>
      <div class="acts">
        {#if c.health === 'exited'}
          <Button size="sm" variant="ghost" aria-label="Start {c.app}" disabled={!awake} onclick={() => confirmContainer(c.id, 'start')}>{#snippet icon()}<Play />{/snippet}</Button>
        {:else}
          <Button size="sm" variant="ghost" aria-label="Restart {c.app}" disabled={!awake} onclick={() => confirmContainer(c.id, 'restart')}>{#snippet icon()}<RotateCcw />{/snippet}</Button>
          {#if full}
            <Button size="sm" variant="ghost" aria-label="Stop {c.app}" disabled={!awake} onclick={() => confirmContainer(c.id, 'stop')}>{#snippet icon()}<Square />{/snippet}</Button>
          {/if}
        {/if}
      </div>
    </li>
  {/each}
</ul>

<style>
  .list { list-style: none; margin: 0; padding: 0; height: 100%; overflow: auto; display: flex; flex-direction: column; }
  .row {
    display: flex; align-items: center; gap: var(--space-3);
    padding: var(--space-2) var(--space-1); border-bottom: 1px solid var(--border);
  }
  .row:last-child { border-bottom: 0; }
  .main { flex: 1; min-width: 0; }
  .name { display: flex; align-items: center; gap: 8px; font-weight: 550; font-size: 0.93rem; }
  .meta { display: flex; gap: 8px; font-size: 0.78rem; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .state { display: flex; flex-direction: column; align-items: flex-end; font-size: 0.78rem; }
  .h { color: var(--text-muted); }
  .acts { display: flex; opacity: 0.55; transition: opacity var(--dur-1) var(--ease); }
  .row:hover .acts, .row:focus-within .acts { opacity: 1; }
  @media (hover: none) { .acts { opacity: 1; } }
  .full .row { padding: var(--space-3) var(--space-2); }
</style>
