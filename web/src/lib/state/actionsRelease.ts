// Confirmations for CirrusUX's own upgrade and rollback, on the server (the
// agent runs it) or on a split setup's gateway machine (the gateway does).
import { ask } from './ui.svelte';
import { now } from '../time.svelte';
import { mode } from '../api.svelte';
import { refreshSoon } from '../sync.svelte';
import { live } from './live.svelte';
import { store } from './store.svelte';
import { loadRelease, release } from './release.svelte';
import { d3, offline, sendAction } from './send';

export type Where = 'server' | 'gateway';

const nameOf = (w: Where) => (w === 'server' ? mode.server : mode.gateway);
const installOf = (w: Where) => (w === 'server' ? live.install : release.gateway);

/** What restarts, so the owner knows the panel may go quiet for a moment. */
function restarts(w: Where): string {
  if (w === 'gateway' || mode.setup === 'single') return ' The panel restarts with it and is out of reach for a moment; reload the page afterwards.';
  return '';
}

function send(w: Where, kind: 'upgrade' | 'rollback', label: string, version: string): void {
  if (!mode.live) return demoRun(w, kind, version);
  const action = `${w === 'server' ? 'self' : 'gateway'}_${kind}`;
  const args = kind === 'upgrade' ? { version } : undefined;
  void sendAction({ action, args }, label, false, { where: nameOf(w) }).then(() => {
    refreshSoon();
    void loadRelease();
  });
}

/** Upgrades CirrusUX on one machine to the release the last check found. */
export function confirmSelfUpgrade(w: Where, version: string): void {
  if (offline(w === 'server' ? 'self_upgrade' : undefined)) return;
  const name = nameOf(w);
  const from = installOf(w)?.current ?? 'the release in use';
  const both = w === 'server' && mode.setup === 'single' ? ' (the gateway runs there too)' : '';
  const label = `Started upgrading CirrusUX on ${name} to ${version}`;
  ask({
    title: `Upgrade CirrusUX on ${name} to ${version}?`,
    body:
      `Downloads CirrusUX ${version} from GitHub on ${name}${both}, checks its signature and checksum, and applies it as cirrusux-setup would at a terminal: ` +
      `new programs, service files and sudo rules, and its services restart. Your settings and data stay as they are.${restarts(w)}${w === 'server' ? d3() : ''}`,
    command: `cirrusux-helper self-upgrade ${version} (cirrusux-setup upgrade ${version} -y)`,
    undo: `Go back to ${from} (cirrusux-setup rollback)`,
    confirmLabel: 'Upgrade',
    changelog: label,
    run: () => send(w, 'upgrade', label, version),
  });
}

/** Puts the release before back on one machine. Running it again undoes it. */
export function confirmSelfRollback(w: Where): void {
  if (offline(w === 'server' ? 'self_rollback' : undefined)) return;
  const name = nameOf(w);
  const inst = installOf(w);
  const label = `Started putting CirrusUX on ${name} back to the release before`;
  ask({
    title: `Put CirrusUX on ${name} back to ${inst?.previous}?`,
    body:
      `Switches ${name} from ${inst?.current} back to ${inst?.previous}, which is still unpacked there, and applies it: nothing is downloaded. ` +
      `Your settings and data stay as they are.${restarts(w)}${w === 'server' ? d3() : ''}`,
    command: 'cirrusux-helper self-rollback (cirrusux-setup rollback -y)',
    undo: `Go back again, to ${inst?.current}`,
    confirmLabel: 'Go back',
    changelog: label,
    run: () => send(w, 'rollback', label, inst?.previous ?? ''),
  });
}

/** Demo mode: the job runs for a few seconds and the versions swap. */
function demoRun(w: Where, kind: 'upgrade' | 'rollback', version: string): void {
  const inst = installOf(w);
  const from = inst?.current ?? '';
  const steps = kind === 'upgrade' ? [`Downloading and checking ${version}`, 'Applying it'] : [`Going back to ${version}`];
  const id = Math.random().toString(16).slice(2, 10);
  const action = `self-${kind}` as const;
  if (w === 'server') store.jobs.unshift({ id, action, app: 'cirrusux', name: 'CirrusUX', state: 'running', step: steps[0], started: now(), finished: null });
  else release.gateway.job = { job: id, action, state: 'running', step: steps[0], started: now() };
  const job = () => (w === 'server' ? store.jobs.find((j) => j.id === id) : release.gateway.job);
  steps.forEach((s, i) => setTimeout(() => { const j = job(); if (j) j.step = s; }, i * 1500));
  setTimeout(() => {
    const j = job();
    if (j) Object.assign(j, { state: 'done', step: `Now on ${version}`, ...(w === 'server' ? { finished: now() } : {}) });
    const next = { managed: true, current: version, previous: from };
    if (w === 'server') live.install = next;
    else Object.assign(release.gateway, next);
  }, steps.length * 1500);
}
