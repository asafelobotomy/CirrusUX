<script lang="ts">
  import { onMount } from 'svelte';
  import { api, mode } from '../api.svelte';
  import { fmtDayTime } from '../time.svelte';

  interface Digest { day: string; at: string; lines: string[]; summary?: string }

  // Demo: what a morning digest looks like for the sample data.
  const SAMPLE: Digest = {
    day: '2026-11-01', at: '2026-11-01T08:00:00Z',
    summary: "Everything that matters is fine: last night's backup finished and ClamAV found nothing. One thing to look at: the media disk still runs at USB 2 speed.",
    lines: [
      'Backup: OK at 2026-11-01 03:31 GMT, 2.1 GB.',
      'Containers: 7 of 8 running; nginx_server (exited) is the old leftover.',
      'Sleep so far today: 5 h 5 min asleep, 2 wakes.',
      'Security: ClamAV 0 infected; firewall lockdown intact; 0 banned by fail2ban.',
      'Media disk: 554 GB free of 1.9 TB, still on a USB 2 link.',
      'Panel actions in the last 24 h: 1.',
    ],
  };

  let digest = $state<Digest | null>(mode.live ? null : SAMPLE);
  let failed = $state(false);

  async function load() {
    try {
      digest = await api<Digest>('/api/digest');
      failed = false;
    } catch {
      failed = true;
    }
  }

  onMount(() => {
    if (!mode.live) return;
    void load();
    const t = setInterval(load, 30 * 60_000);
    return () => clearInterval(t);
  });
</script>

{#if digest}
  <div class="d">
    {#if digest.summary}<p class="sum">{digest.summary}</p>{/if}
    <ul>{#each digest.lines as l}<li>{l}</li>{/each}</ul>
    <p class="faint small">{digest.summary ? `Written by ${mode.assistant.name} from` : 'Built from'} cached data · {fmtDayTime(new Date(digest.at))}</p>
  </div>
{:else if failed}
  <p class="muted">Couldn't load the digest from the gateway.</p>
{:else}
  <p class="muted">Loading…</p>
{/if}

<style>
  .d { height: 100%; overflow: auto; display: flex; flex-direction: column; gap: 8px; }
  .sum { font-size: 0.95rem; line-height: 1.5; }
  ul { margin: 0; padding-left: 1.1em; display: flex; flex-direction: column; gap: 3px; font-size: 0.85rem; color: var(--text-muted); }
  .small { font-size: 0.76rem; }
</style>
