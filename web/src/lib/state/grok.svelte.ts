// Grok assistant. Live mode asks the gateway, which runs Grok with read tools
// over the snapshot cache and proposal-only actions (PLAN §3.6); demo mode
// answers from canned replies.
import { ui } from './ui.svelte';
import { power } from './power.svelte';
import { live } from './live.svelte';
import { confirmBackup, confirmContainer, confirmStayAwake, confirmWake } from './actions';
import { fmtTime } from '../time.svelte';
import { api, mode } from '../api.svelte';

export interface Proposal {
  label: string;
  run: () => void;
}

export interface Msg {
  role: 'user' | 'grok';
  text: string;
  tools?: string[];
  cached?: boolean;
  proposal?: Proposal;
}

export const grok = $state<{ messages: Msg[]; thinking: boolean }>({ messages: [], thinking: false });

function answer(q: string): Msg {
  const s = q.toLowerCase();
  const asleep = power.state !== 'awake';
  const cached = asleep;

  const b = live.lastBackupRun;
  if (/backup/.test(s) && b) {
    return {
      role: 'grok', cached, tools: ['backups'],
      text: `Yes. Last night's backup ran ${fmtTime(b.start)}–${fmtTime(b.end)} and finished OK: ${b.sizeGb.toFixed(1)} GB, encrypted with age. The next one is tomorrow at 03:30, and the RTC alarm will wake the server for it. One thing to know: the backups sit on the same USB disk as the media, so there's no offsite copy yet.`,
      proposal: asleep ? undefined : { label: 'Run a backup now', run: () => confirmBackup({ source: 'grok' }) },
    };
  }
  if (/wake|woke|18:16|why/.test(s)) {
    return {
      role: 'grok', cached, tools: ['wake_history', 'awake_reasons'],
      text: 'At 18:16 the TV streamer sent traffic that woke the NIC. The idle check saw it was an ignored client with only a 2-minute playback blip (under 5 MB), so it gave a short grace and the server went back to sleep at 18:33. Nothing needs fixing.',
    };
  }
  if (/space|storage|disk|biggest/.test(s)) {
    return {
      role: 'grok', cached, tools: ['disk_usage'],
      text: 'TV Shows uses 857 GB and Movies 371 GB: together about 95% of the 1.3 TB used. 554 GB is free. About 126 GB of that is 332 legacy .avi files, which you deferred re-encoding. Also worth knowing: the disk is linked at USB 2 speed (480 Mb/s), which makes scans slow.',
    };
  }
  if (/restart|jellyfin/.test(s)) {
    return {
      role: 'grok', cached, tools: ['containers', 'container_logs_summary'],
      text: 'Jellyfin is up and healthy on 10.11.11. The only repeated log noise is the Trakt plugin hitting HTTP 420 rate limits hourly, which you chose to leave as is. If playback is misbehaving, a restart is safe and takes a few seconds.',
      proposal: asleep ? undefined : { label: 'Restart Jellyfin', run: () => confirmContainer('jf', 'restart', { source: 'grok' }) },
    };
  }
  if (asleep || /asleep|sleep|awake|status/.test(s)) {
    return asleep
      ? {
          role: 'grok', cached: true, tools: ['power_status'],
          text: `cloud64 is asleep since ${fmtTime(power.since)}. I answered from the last snapshot, so I didn't wake it. Want me to wake it?`,
          proposal: { label: 'Wake cloud64', run: () => confirmWake({ source: 'grok' }) },
        }
      : {
          role: 'grok', tools: ['power_status', 'awake_reasons'],
          text: 'cloud64 is awake with nothing holding it awake, so it will sleep after 30 idle minutes. If you’re about to watch something, playback counts as activity and keeps it up on its own.',
          proposal: { label: 'Keep awake for 2 h', run: () => confirmStayAwake(120, { source: 'grok' }) },
        };
  }
  return {
    role: 'grok', cached, tools: ['power_status', 'containers'],
    text: 'All 7 apps are running and healthy, the last backup is fine and ClamAV found nothing. The one leftover is an exited nginx container on :latest, which is waiting for your approval to archive.',
  };
}

interface LiveReply { text: string; tools?: string[]; cached: boolean; proposal?: { action: string; label: string } }

async function askLive(): Promise<Msg> {
  if (!mode.grok) {
    return { role: 'grok', text: "Grok isn't set up on the gateway yet. Add XAI_API_KEY and XAI_MODEL to its environment file and restart it." };
  }
  const history = grok.messages.map((m) => ({ role: m.role === 'grok' ? 'assistant' : 'user', text: m.text }));
  try {
    const r = await api<LiveReply>('/api/grok', { method: 'POST', body: JSON.stringify({ messages: history }) });
    return {
      role: 'grok', text: r.text, tools: r.tools ?? [], cached: r.cached,
      proposal: r.proposal?.action === 'propose_wake'
        ? { label: r.proposal.label || 'Wake cloud64', run: () => confirmWake({ source: 'grok' }) }
        : undefined,
    };
  } catch (e) {
    return { role: 'grok', text: (e as Error).message };
  }
}

export function askGrok(text: string): void {
  ui.grokOpen = true;
  grok.messages.push({ role: 'user', text });
  grok.thinking = true;
  if (mode.live) {
    void askLive().then((m) => {
      grok.messages.push(m);
      grok.thinking = false;
    });
    return;
  }
  setTimeout(() => {
    grok.messages.push(answer(text));
    grok.thinking = false;
  }, 900);
}
