<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import { live } from '../lib/state/live.svelte';
  import { mode } from '../lib/api.svelte';
  import { fmtDayTime } from '../lib/time.svelte';
  import { power } from '../lib/state/power.svelte';

  const net = $derived(live.network);
  const kinds: Record<string, string> = { ethernet: 'Wired', wifi: 'Wi-Fi', tailscale: 'Tailscale', bridge: 'Bridge', vpn: 'VPN', loopback: 'This machine only', virtual: 'Virtual' };
  const order = ['ethernet', 'wifi', 'tailscale', 'vpn', 'bridge', 'virtual', 'loopback'];
  const ifaces = $derived(net ? [...net.interfaces].sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind)) : []);
  const speed = (m?: number) => (!m ? '' : m >= 1000 ? `${m / 1000} Gb/s` : `${m} Mb/s`);
  const stub = (d: string) => d === '127.0.0.53' || d === '127.0.0.1';
</script>

<div class="stack">
  {#if !net}
    <Card><p class="muted">No network details from {mode.server} yet. Update CirrusUX on it to see them.</p></Card>
  {:else}
    <div class="grid">
      <Card title="Name">
        <p class="big">{net.hostname}</p>
        {#if net.tailscale?.length}<p class="muted">On the tailnet as <code>{net.tailscale.join(', ')}</code></p>{/if}
      </Card>
      <Card title="Gateway">
        {#if net.gateway}<p class="big"><code>{net.gateway}</code></p><p class="muted">through {net.gatewayIface}</p>
        {:else}<p class="muted">No IPv4 default route.</p>{/if}
      </Card>
      <Card title="DNS">
        {#if net.dns.length}
          <p class="big"><code>{net.dns.join(', ')}</code></p>
          {#if net.dns.some(stub)}<p class="muted">A local resolver (systemd-resolved or similar) answers and forwards.</p>{/if}
          {#if net.search?.length}<p class="muted">Search: {net.search.join(', ')}</p>{/if}
        {:else}<p class="muted">No name servers in /etc/resolv.conf.</p>{/if}
      </Card>
    </div>

    <Card title="Interfaces" subtitle={net.hidden ? `${net.hidden} container links left out` : undefined}>
      <div class="table">
        <table>
          <thead><tr><th>Interface</th><th>State</th><th>Addresses</th><th>Hardware</th></tr></thead>
          <tbody>
            {#each ifaces as f (f.name)}
              <tr>
                <td><strong>{f.name}</strong><br /><span class="faint">{kinds[f.kind] ?? f.kind}</span></td>
                <td><Badge tone={f.state === 'up' ? 'ok' : f.state === 'down' ? 'bad' : 'neutral'}>{f.state || 'unknown'}</Badge>{#if f.mbps}<br /><span class="faint">{speed(f.mbps)}</span>{/if}</td>
                <td>{#each f.addrs as a}<code>{a}</code><br />{:else}<span class="faint">none</span>{/each}</td>
                <td>{#if f.mac}<code>{f.mac}</code><br />{/if}<span class="faint">MTU {f.mtu}</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
  <p class="faint note">
    Reads only. A wrong network setting can cut the panel off from {mode.server}, so changes stay on the server:
    Wake-on-LAN and Tailscale through <code>NET_*</code> in host.env (<code>sudo cirrusux-setup apply network</code>),
    addresses through the system's own network tools. Last data {fmtDayTime(power.lastSnapshot)}.
  </p>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr)); gap: var(--space-4); }
  .big { font-size: 1.05rem; font-weight: 600; overflow-wrap: anywhere; }
  .grid p.muted { font-size: 0.85rem; margin-top: 4px; }
  .table { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.88rem; }
  th, td { text-align: left; padding: 10px 12px 10px 0; border-top: 1px solid var(--border); vertical-align: top; }
  th { color: var(--text-muted); font-weight: 550; font-size: 0.8rem; border-top: none; }
  td code { font-size: 0.8rem; }
  .note { font-size: 0.8rem; }
</style>
