<script lang="ts">
  // Recent logs of a container or one of CirrusUX's own units, fetched on
  // request (never a stream that keeps the server busy). "Follow" refreshes
  // every 5 s and stops by itself after 5 minutes. "Explain this" sends the
  // chosen lines to the assistant, which masks addresses and secrets first.
  import { onDestroy } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Segmented from '../lib/components/Segmented.svelte';
  import { mode, has } from '../lib/api.svelte';
  import { live } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { store } from '../lib/state/store.svelte';
  import { sendAction } from '../lib/state/actions';
  import { askAssistant } from '../lib/state/assistant.svelte';
  import { toast } from '../lib/state/ui.svelte';
  import { fmtTime } from '../lib/time.svelte';
  import { Play, RefreshCw, Sparkles, Square } from '../lib/icons';

  const units = $derived([
    'cirrusux-agent.service',
    'cirrusux-dockerfilter.service',
    ...(has('backup') ? ['cirrusux-backup.service'] : []),
    ...(has('av') ? ['cirrusux-av-scan.service'] : []),
    ...(has('sleep') ? ['cirrusux-idle.service'] : []),
    ...store.installed.filter((a) => a.runtime === 'podman').map((a) => `cirrusux-${a.id}.service`),
  ]);
  const sources = $derived([
    ...live.containers.map((c) => ({ value: `container:${c.name}`, label: `${c.app} (container)` })),
    ...units.map((u) => ({ value: `unit:${u}`, label: u })),
  ]);

  let source = $state('');
  let count = $state(200);
  let lines = $state<string[]>([]);
  let fetched = $state<Date | null>(null);
  let loading = $state(false);
  let picked = $state<Set<number>>(new Set());
  let followUntil = $state(0);
  let timer: ReturnType<typeof setInterval> | undefined;

  $effect(() => {
    if (!source && sources.length) source = sources[0].value;
  });

  const awake = $derived(!mode.live || power.state === 'awake');

  async function load() {
    if (!source) return;
    loading = true;
    if (!mode.live) {
      lines = demoLines(source);
    } else {
      const c = await sendAction({ action: 'logs', args: { source, lines: String(count) } }, 'Logs', true);
      if (c?.state === 'done') lines = c.output ?? [];
      else if (c) toast(`Couldn't read the logs: ${c.error ?? 'no answer'}`, 'warn');
    }
    fetched = new Date();
    picked = new Set();
    loading = false;
  }

  function follow() {
    if (followUntil) return stop();
    followUntil = Date.now() + 5 * 60_000;
    void load();
    timer = setInterval(() => {
      if (Date.now() > followUntil) return stop();
      if (!loading) void load();
    }, 5000);
  }
  function stop() {
    clearInterval(timer);
    followUntil = 0;
  }
  onDestroy(stop);
  $effect(() => {
    void source;
    stop();
    lines = [];
    fetched = null;
  });

  function toggle(i: number) {
    const s = new Set(picked);
    if (s.has(i)) s.delete(i);
    else s.add(i);
    picked = s;
  }

  function explain() {
    const chosen = picked.size ? lines.filter((_, i) => picked.has(i)) : lines.slice(-40);
    const label = sources.find((s) => s.value === source)?.label ?? source;
    askAssistant(`Explain these log lines from ${label} on ${mode.server}: what they mean, whether anything is wrong, and what to do about it.\n\n${chosen.slice(-80).join('\n')}`);
  }

  function demoLines(src: string): string[] {
    if (src.startsWith('unit:cirrusux-backup')) {
      return ['2026-11-01T03:30:53+0000 homeserver systemd[1]: Starting cirrusux-backup.service - CirrusUX system backup...', '2026-11-01T03:31:33+0000 homeserver cirrusux-backup[812]: OK 2.0G homeserver-system-2026-11-01_033053.tar.zst.age', '2026-11-01T03:31:33+0000 homeserver systemd[1]: Finished cirrusux-backup.service - CirrusUX system backup.'];
    }
    return [
      '2026-11-01T20:20:04Z [INF] Jellyfin version: 12.4.0',
      '2026-11-01T20:20:09Z [INF] Startup complete 0:00:05.2',
      '2026-11-01T20:31:12Z [WRN] Slow HTTP response from 192.0.2.23 to /Videos/stream in 0:00:01.4',
      '2026-11-01T20:35:40Z [ERR] Error checking for updates from plugin Trakt: HTTP 420 Enhance Your Calm',
      '2026-11-01T20:38:02Z [INF] Playback stopped reported by app "Android TV" playing "Film". Stopped at 4512000 ms',
    ];
  }
</script>

<div class="stack">
  <Card title="Logs" subtitle="The most recent lines, fetched when you ask. Pick lines and ask the assistant what they mean.">
    <div class="bar">
      <select bind:value={source} aria-label="Log source">
        {#each sources as s}<option value={s.value}>{s.label}</option>{/each}
      </select>
      <Segmented label="Lines" bind:value={count} options={[{ value: 100, label: '100' }, { value: 200, label: '200' }, { value: 500, label: '500' }]} />
      <Button size="sm" disabled={!awake || loading || !source} onclick={load}>{#snippet icon()}<RefreshCw />{/snippet}{lines.length ? 'Refresh' : 'Show'}</Button>
      <Button size="sm" variant={followUntil ? 'primary' : 'ghost'} disabled={!awake || !source} onclick={follow}>
        {#snippet icon()}{#if followUntil}<Square />{:else}<Play />{/if}{/snippet}{followUntil ? 'Stop' : 'Follow'}
      </Button>
      <span class="spacer"></span>
      <Button size="sm" variant="ghost" disabled={!lines.length || !mode.assistant.enabled && mode.live} onclick={explain}>
        {#snippet icon()}<Sparkles />{/snippet}Explain {picked.size ? `${picked.size} line${picked.size === 1 ? '' : 's'}` : 'the last lines'}
      </Button>
    </div>
    {#if mode.live && !awake}
      <p class="muted">{mode.server} is asleep. Wake it to read its logs.</p>
    {:else if !lines.length}
      <p class="muted">{loading ? 'Reading…' : fetched ? 'No lines.' : 'Choose a source and press Show.'}</p>
    {:else}
      <ol class="log" aria-label="Log lines">
        {#each lines as l, i}
          <li class:on={picked.has(i)}><button onclick={() => toggle(i)} aria-pressed={picked.has(i)}>{l}</button></li>
        {/each}
      </ol>
      <p class="faint small">
        {lines.length} lines{fetched ? ` · fetched ${fmtTime(fetched)}` : ''}{followUntil ? ` · following until ${fmtTime(new Date(followUntil))}` : ''}. Click lines to pick them.
      </p>
    {/if}
  </Card>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .bar { display: flex; gap: var(--space-2); align-items: center; flex-wrap: wrap; margin-bottom: var(--space-3); }
  .spacer { flex: 1; }
  select {
    height: 32px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong);
    background: var(--surface-2); color: var(--text); font: inherit; font-size: 0.88rem; max-width: 100%;
  }
  .log {
    list-style: none; margin: 0; padding: 6px 0; max-height: 60vh; overflow: auto; border-radius: var(--radius-sm);
    background: var(--surface-2); border: 1px solid var(--border); font-family: var(--font-mono, monospace); font-size: 0.78rem;
  }
  .log button {
    display: block; width: 100%; text-align: left; padding: 2px 10px; border: 0; background: none; color: var(--text);
    font: inherit; white-space: pre-wrap; overflow-wrap: anywhere; border-radius: 0;
  }
  .log button:hover { background: var(--hover); }
  .log li.on button { background: var(--accent-soft); }
  .small { font-size: 0.8rem; margin-top: var(--space-2); }
</style>
