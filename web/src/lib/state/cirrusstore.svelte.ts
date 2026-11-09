// CirrusStore's sources: listed from the server's report, fetched by the
// gateway only when the owner presses Check CirrusStore.
import { api, mode } from '../api.svelte';
import type { StoreSource } from '../catalogue';
import { demoSources } from '../demo/cirrusstore';
import { now } from '../time.svelte';
import { loadCatalogue, loadUpdates, store } from './store.svelte';
import { toast } from './ui.svelte';

interface StoreView {
  fetched?: string;
  sources: StoreSource[];
}

const toSources = (v: StoreView) => ({ fetched: v.fetched ? new Date(v.fetched) : null, list: v.sources ?? [] });

export async function loadSources(): Promise<void> {
  if (!mode.live) {
    if (!store.sources.list.length) {
      const fetched = new Date(now().getTime() - 2 * 86400_000);
      store.sources = { fetched, list: demoSources(fetched) };
    }
    return;
  }
  try {
    store.sources = toSources(await api<StoreView>('/api/store'));
  } catch {
    /* keep what we have */
  }
}

/** Check CirrusStore: the gateway fetches every source's catalogue and
 *  verifies it; the server isn't woken. */
export async function checkStore(): Promise<void> {
  if (!mode.live) {
    await new Promise((r) => setTimeout(r, 900));
    const fetched = now();
    store.sources = { fetched, list: demoSources(fetched) };
    return;
  }
  try {
    store.sources = toSources(await api<StoreView>('/api/store/refresh', { method: 'POST' }));
    await loadCatalogue(true);
    await loadUpdates();
    const bad = store.sources.list.filter((s) => s.status !== 'ok' && s.status !== 'not-checked');
    if (bad.length) toast(`CirrusStore: ${bad.map((s) => `${s.name} ${s.status}`).join(', ')}`, 'warn');
  } catch (e) {
    toast(`Couldn't check CirrusStore: ${(e as Error).message}`, 'warn');
  }
}

/** Opens an app's page, from a given source or whichever offers it. */
export function openApp(id: string, source?: string): void {
  store.openSource = source ?? null;
  store.open = id;
}
