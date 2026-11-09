<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import { live } from '../state/live.svelte';
  import { mode } from '../api.svelte';
  import { fmtAgo } from '../time.svelte';
  import { Lock, ShieldCheck, Activity } from '../icons';

  const sec = $derived(live.security);
  const failing = $derived(sec?.firewall.filter((f) => !f.ok).map((f) => f.chain) ?? []);
</script>

{#if sec}
  <ul class="s">
    {#if sec.clamav || mode.features.includes('av')}
      <li>
        <span class="ic"><ShieldCheck size={16} /></span>
        {#if sec.clamav}
          <span class="t">Virus scan <span class="faint">· {fmtAgo(sec.clamav.at)}</span></span>
          <Badge tone={sec.clamav.infected ? 'bad' : 'ok'}>{sec.clamav.infected} infected</Badge>
        {:else}
          <span class="t">Virus scan</span><Badge>no scan found</Badge>
        {/if}
      </li>
    {/if}
    {#if sec.firewall.length}
      <li>
        <span class="ic"><Lock size={16} /></span>
        <span class="t" title={failing.length ? `Missing: ${failing.join(', ')}` : sec.firewall.map((f) => f.chain).join(', ')}>Firewall rules</span>
        <Badge tone={failing.length ? 'bad' : 'ok'}>{failing.length ? `${failing.length} missing` : 'intact'}</Badge>
      </li>
    {/if}
    {#if sec.banned !== null}
      <li>
        <span class="ic"><Activity size={16} /></span>
        <span class="t">fail2ban</span>
        <Badge tone="neutral">{sec.banned} banned</Badge>
      </li>
    {/if}
  </ul>
{:else}
  <p class="muted">No security data from {mode.server} yet.</p>
{/if}

<style>
  .s { list-style: none; margin: 0; padding: 0; height: 100%; display: flex; flex-direction: column; justify-content: space-around; gap: 6px; }
  li { display: flex; align-items: center; gap: var(--space-2); font-size: 0.9rem; }
  .ic { color: var(--text-muted); display: inline-flex; }
  .t { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
