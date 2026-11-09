// Sending an action to the server through the gateway and waiting for it.
import { toast } from './ui.svelte';
import { api, mode, sleeps } from '../api.svelte';
import { loadChangelog, refreshSoon } from '../sync.svelte';
import { agentCan, UPDATE_HINT } from './live.svelte';
import { pollCommand, type Command } from './poll';

export type { Command };
export type Opts = { source?: 'assistant' };

export interface ActionReq { action: string; args?: Record<string, string> }

/** Decision D3, for servers that sleep. */
export const d3 = () => (sleeps() ? ` Keeps ${mode.server} awake for at least 10 minutes afterwards.` : '');

/** True (with a toast) when the action can't go ahead: offline, or the server's agent doesn't know it. */
export function offline(action?: string): boolean {
  if (mode.offline) {
    toast("You're offline. Actions need the gateway.", 'warn');
    return true;
  }
  if (mode.live && action && !agentCan(action)) {
    toast(`${UPDATE_HINT}: run sudo cirrusux-setup upgrade there.`, 'warn');
    return true;
  }
  return false;
}

/** Sends an action to the server through the gateway and waits for the result. */
export async function sendAction(req: ActionReq, label: string, quiet = false, opts: { where?: string; waitMs?: number } = {}): Promise<Command | null> {
  if (mode.live && !agentCan(req.action) && !req.action.startsWith('git_')) {
    // The gateway refuses it too; this saves the round trip and says why.
    toast(`${label}: ${UPDATE_HINT}.`, 'warn');
    return null;
  }
  try {
    const { id } = await api<{ id: string }>('/api/actions', { method: 'POST', body: JSON.stringify(req) });
    if (!quiet) toast(`${label}: sent to ${opts.where ?? mode.server}…`, 'info');
    const r = await pollCommand(() => api<Command>(`/api/actions/${id}`), { waitMs: opts.waitMs ?? 5 * 60_000 });
    void loadChangelog();
    refreshSoon();
    if (r.kind === 'lost') {
      // The action may well have run: don't say it failed.
      if (!quiet) toast(`${label}: lost contact with the gateway, so the result isn't known. Check the Changelog to see whether it went through.`, 'warn');
      return null;
    }
    const c = r.command;
    if (!quiet) {
      if (c.state === 'done') toast(`${label}. Logged to CHANGELOG.`, 'ok');
      else toast(`${label} failed: ${c.error ?? `no answer from ${opts.where ?? mode.server}`}`, 'warn');
    }
    return c;
  } catch (e) {
    toast((e as Error).message, 'warn');
    return null;
  }
}
