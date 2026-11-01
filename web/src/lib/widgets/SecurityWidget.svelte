<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import { live } from '../state/live.svelte';
  import { fmtAgo } from '../time.svelte';
  import { Lock, ShieldCheck, Activity } from '../icons';

  const sec = $derived(live.security);
</script>

{#if sec}
  <ul class="s">
    <li>
      <span class="ic"><ShieldCheck size={16} /></span>
      {#if sec.clamav}
        <span class="t">ClamAV <span class="faint">· {fmtAgo(sec.clamav.at)}</span></span>
        <Badge tone={sec.clamav.infected ? 'bad' : 'ok'}>{sec.clamav.infected} infected</Badge>
      {:else}
        <span class="t">ClamAV</span><Badge>no scan found</Badge>
      {/if}
    </li>
    <li>
      <span class="ic"><Lock size={16} /></span>
      <span class="t">Firewall lockdown</span>
      <Badge tone={sec.lockdown && sec.panelDrop ? 'ok' : 'bad'}>{sec.lockdown && sec.panelDrop ? 'intact' : 'missing'}</Badge>
    </li>
    <li>
      <span class="ic"><Activity size={16} /></span>
      <span class="t">fail2ban <span class="faint">· sshd</span></span>
      <Badge tone="neutral">{sec.banned} banned</Badge>
    </li>
  </ul>
{:else}
  <p class="muted">No security data from cloud64 yet.</p>
{/if}

<style>
  .s { list-style: none; margin: 0; padding: 0; height: 100%; display: flex; flex-direction: column; justify-content: space-around; gap: 6px; }
  li { display: flex; align-items: center; gap: var(--space-2); font-size: 0.9rem; }
  .ic { color: var(--text-muted); display: inline-flex; }
  .t { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
