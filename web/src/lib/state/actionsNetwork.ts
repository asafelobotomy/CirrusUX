// Network changes from the Network page: DNS servers and Tailscale's DNS
// and routes. Each is confirmed and logged, and undoes itself after two
// minutes unless it's kept, so a wrong setting can't lock anyone out.
import { live } from './live.svelte';
import { ask, toast } from './ui.svelte';
import { mode } from '../api.svelte';
import { offline, sendAction, type Opts } from './send';

const SECONDS = 120;
const onOff = (b: boolean) => (b ? 'on' : 'off');

/** Demo only: shows the same waiting change a real server reports. */
function demoPending(what: string): void {
  if (live.netEdit) live.netEdit.pending = { kind: 'demo', what, deadline: new Date(Date.now() + SECONDS * 1000).toISOString() };
}

const WAIT = `It's undone by itself after ${SECONDS} seconds unless you press Keep, so if it cuts you off, wait two minutes and the panel comes back.`;

export function confirmDNS(servers: string, search: string, o: Opts = {}): void {
  if (offline('net_dns_set')) return;
  const reset = servers === '-' && search === '-';
  ask({
    ...o,
    title: reset ? 'Go back to the system\'s own DNS?' : `Use DNS server${servers.includes(',') ? 's' : ''} ${servers}?`,
    body: `${reset ? `${mode.server} goes back to the DNS settings it gets from its network.` : `${mode.server} asks ${servers} to look up names${search !== '-' ? `, and tries the search domains ${search}` : ''}. Containers and Tailscale may use their own DNS.`} ${WAIT}`,
    command: reset ? 'rm /etc/systemd/resolved.conf.d/50-cirrusux.conf; systemctl restart systemd-resolved' : `write /etc/systemd/resolved.conf.d/50-cirrusux.conf (DNS=${servers.replaceAll(',', ' ')}); systemctl restart systemd-resolved`,
    undo: 'put the earlier DNS settings back',
    confirmLabel: reset ? 'Use system DNS' : 'Apply DNS',
    changelog: reset ? 'Went back to the system\'s own DNS settings' : `Set DNS servers to ${servers}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'net_dns_set', args: { servers, search } }, reset ? 'Went back to system DNS' : `Set DNS to ${servers}`);
        return;
      }
      if (live.netEdit) Object.assign(live.netEdit, { dns: servers === '-' ? [] : servers.split(','), search: search === '-' ? [] : search.split(',') });
      demoPending(reset ? 'System DNS' : `DNS ${servers}`);
      toast('Demo: DNS changed (nothing on a server changed).', 'ok');
    },
  });
}

export function confirmTailscale(dns: boolean, routes: boolean, o: Opts = {}): void {
  if (offline('net_tailscale_set')) return;
  ask({
    ...o,
    title: 'Change Tailscale\'s settings?',
    body: `DNS from the tailnet: ${onOff(dns)}. Subnet routes from other devices: ${onOff(routes)}. ${WAIT}`,
    command: `tailscale set --accept-dns=${dns} --accept-routes=${routes}`,
    undo: 'put the earlier Tailscale settings back',
    confirmLabel: 'Apply',
    changelog: `Tailscale: DNS from the tailnet ${onOff(dns)}, subnet routes ${onOff(routes)}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'net_tailscale_set', args: { dns: dns ? 'yes' : 'no', routes: routes ? 'yes' : 'no' } }, 'Changed Tailscale\'s settings');
        return;
      }
      if (live.netEdit?.tailscale) live.netEdit.tailscale = { acceptDns: dns, acceptRoutes: routes };
      demoPending('Tailscale settings');
      toast('Demo: Tailscale changed (nothing on a server changed).', 'ok');
    },
  });
}

/** Keeps the waiting change. Pressing a button is the confirmation, so it asks nothing more. */
export function keepNetChange(): void {
  if (offline('net_confirm')) return;
  if (mode.live) {
    void sendAction({ action: 'net_confirm' }, 'Kept the network change');
    return;
  }
  if (live.netEdit) live.netEdit.pending = undefined;
  toast('Demo: kept.', 'ok');
}

/** Undoes the waiting change now. */
export function undoNetChange(): void {
  if (offline('net_revert')) return;
  if (mode.live) {
    void sendAction({ action: 'net_revert' }, 'Undid the network change');
    return;
  }
  toast('Demo: undone (the demo keeps the new values).', 'ok');
  if (live.netEdit) live.netEdit.pending = undefined;
}
