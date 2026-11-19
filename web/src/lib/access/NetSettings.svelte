<script lang="ts">
  import Card from '../components/Card.svelte';
  import Badge from '../components/Badge.svelte';
  import Button from '../components/Button.svelte';
  import Toggle from '../components/Toggle.svelte';
  import { confirmDNS, confirmTailscale, keepNetChange, undoNetChange } from '../state/actions';
  import { agentCan, live, needsAgent } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { mode } from '../api.svelte';

  const ne = $derived(live.netEdit);
  const asleep = $derived(mode.live && power.state !== 'awake');
  const waiting = $derived(!!ne?.pending);

  let servers = $state('');
  let search = $state('');
  let ts = $state({ dns: true, routes: false });
  // Start the fields from what the server reports, until the person edits them.
  let touched = $state(false);
  $effect(() => {
    if (!ne || touched) return;
    servers = ne.dns.join(', ');
    search = ne.search.join(', ');
    if (ne.tailscale) ts = { dns: ne.tailscale.acceptDns, routes: ne.tailscale.acceptRoutes };
  });
  // Flipping a switch counts as editing too, so a refresh doesn't undo it.
  $effect(() => {
    if (ne?.tailscale && (ts.dns !== ne.tailscale.acceptDns || ts.routes !== ne.tailscale.acceptRoutes)) touched = true;
  });

  const list = (s: string) => s.split(/[\s,]+/).filter(Boolean);
  const dnsArg = $derived(list(servers).join(',') || '-');
  const searchArg = $derived(list(search).join(',') || '-');
  const dnsProblem = $derived.by(() => {
    const l = list(servers);
    if (l.length > 3) return 'At most 3 servers.';
    for (const s of l) {
      if (/^(127\.|0\.0\.0\.0|22[4-9]\.|23\d\.)/.test(s) || /^::1?$/.test(s) || s.includes('%')) return `${s} can't be a DNS server here.`;
      if (!/^[0-9a-f.:]+$/i.test(s)) return `${s} isn't an IP address.`;
    }
    return null;
  });
  const searchProblem = $derived(list(search).length > 6 ? 'At most 6 search domains.' : list(search).some((d) => !/^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/.test(d)) ? 'A search domain looks wrong.' : null);
  const dnsChanged = $derived(!!ne && (dnsArg !== (ne.dns.join(',') || '-') || searchArg !== (ne.search.join(',') || '-')));
  const tsChanged = $derived(!!ne?.tailscale && (ts.dns !== ne.tailscale.acceptDns || ts.routes !== ne.tailscale.acceptRoutes));

  // A second counter for the waiting change.
  let tick = $state(Date.now());
  $effect(() => {
    if (!waiting) return;
    const t = setInterval(() => (tick = Date.now()), 1000);
    return () => clearInterval(t);
  });
  const left = $derived(ne?.pending ? Math.max(0, Math.round((Date.parse(ne.pending.deadline) - tick) / 1000)) : 0);
</script>

{#if ne}
  <Card title="Network settings" subtitle="Changes undo themselves after two minutes unless you keep them">
    {#if ne.pending}
      <div class="pending" role="status">
        <p><strong>{ne.pending.what}</strong> is applied but not kept yet. {left > 0 ? `It's undone in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')} unless you keep it.` : 'It is being undone.'}</p>
        <div class="row">
          <Button variant="primary" size="sm" disabled={left <= 0} onclick={keepNetChange}>Keep it</Button>
          <Button size="sm" onclick={undoNetChange}>Undo now</Button>
        </div>
      </div>
    {/if}

    <h3>DNS servers</h3>
    {#if !ne.resolved}
      <p class="muted">DNS here isn't managed by systemd-resolved, so it can't be changed from the panel.</p>
    {:else}
      <div class="form">
        <label for="dns-servers">Servers (up to 3, IPv4 or IPv6)</label>
        <input id="dns-servers" bind:value={servers} oninput={() => (touched = true)} placeholder="192.0.2.53, 2001:db8::53" spellcheck="false" autocomplete="off" />
        <label for="dns-search">Search domains (optional)</label>
        <input id="dns-search" bind:value={search} oninput={() => (touched = true)} placeholder="lan, example.ts.net" spellcheck="false" autocomplete="off" />
        {#if dnsProblem || searchProblem}<p class="warn">{dnsProblem ?? searchProblem}</p>{/if}
        <div class="row">
          <Button variant="primary" size="sm" title={needsAgent('net_dns_set')}
            disabled={asleep || waiting || !!dnsProblem || !!searchProblem || !dnsChanged || !agentCan('net_dns_set')}
            onclick={() => confirmDNS(dnsArg, searchArg)}>Apply DNS</Button>
          {#if ne.dns.length || ne.search.length}
            <Button size="sm" variant="ghost" disabled={asleep || waiting || !agentCan('net_dns_set')} onclick={() => { touched = false; confirmDNS('-', '-'); }}>Use the system's own</Button>
          {/if}
        </div>
      </div>
    {/if}

    {#if ne.tailscale}
      <h3>Tailscale</h3>
      <div class="form">
        <Toggle bind:checked={ts.dns} label="Use DNS from the tailnet" hint="Names of your other devices work; the tailnet's DNS servers answer." />
        <Toggle bind:checked={ts.routes} label="Accept subnet routes" hint="Reach networks that other devices on the tailnet share. Turn off if it clashes with this network." />
        <div class="row">
          <Button variant="primary" size="sm" title={needsAgent('net_tailscale_set')} disabled={asleep || waiting || !tsChanged || !agentCan('net_tailscale_set')}
            onclick={() => confirmTailscale(ts.dns, ts.routes)}>Apply Tailscale settings</Button>
        </div>
      </div>
    {/if}
    <p class="faint note">
      Only DNS and Tailscale are here, on purpose: a wrong IP address can't undo itself. Addresses stay with the system's own network tools.
      {#if ne.dns.length}<Badge tone="info">DNS set from the panel</Badge>{/if}
    </p>
  </Card>
{/if}

<style>
  h3 { font-size: 0.95rem; margin: var(--space-4) 0 var(--space-2); }
  .form { display: flex; flex-direction: column; gap: 6px; max-width: 460px; }
  .form label { font-size: 0.85rem; font-weight: 550; }
  input { padding: 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); font: inherit; font-family: var(--font-mono); font-size: 0.85rem; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 4px; }
  .pending { border: 1px solid var(--border-strong); background: var(--accent-soft); border-radius: var(--radius-sm); padding: var(--space-3); }
  .pending p { font-size: 0.9rem; margin-bottom: 6px; }
  .warn { color: var(--warn, var(--text)); font-size: 0.85rem; }
  .note { font-size: 0.8rem; margin-top: var(--space-3); }
</style>
