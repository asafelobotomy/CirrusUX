<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import AccountCard from '../lib/access/AccountCard.svelte';
  import { live } from '../lib/state/live.svelte';
  import { api, mode } from '../lib/api.svelte';
  import { people } from '../lib/access';

  const acc = $derived(live.access);
  const ssh = $derived(acc?.ssh);
  const shown = $derived(acc ? [...acc.users.filter((u) => u.uid === 0), ...people(acc)] : []);
  const others = $derived(acc ? acc.users.filter((u) => u.uid >= 1000 && !u.canLogin) : []);

  // The gateway's own key, which an account can accept for the Terminal.
  let panelKey = $state('');
  $effect(() => {
    if (!mode.terminal) return;
    if (!mode.live) {
      panelKey = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIdemoPanelKey cirrusux-terminal@gateway';
      return;
    }
    api<{ publicKey?: string }>('/api/terminal', { headers: { 'X-Cirrus-Request': '1' } })
      .then((r) => (panelKey = r.publicKey ?? ''))
      .catch(() => (panelKey = ''));
  });
</script>

<div class="stack">
  <Card title="Signing in to the panel">
    <p>You're signed in as <strong>{mode.user || 'you'}</strong>. The panel has no passwords of its own: whoever lets you in to it (Tailscale, or a sign-in service such as Authelia or Authentik in front of it) also handles two-step verification and passkeys. Turn those on there.</p>
  </Card>

  {#if !acc || !ssh}
    <Card><p class="muted">{mode.server} hasn't reported its accounts. Set <code>ACCESS=yes</code> in <code>/etc/cirrusux/helper.env</code> on it.</p></Card>
  {:else}
    <Card title="SSH logins" subtitle={`How people log in to ${mode.server}, from sshd's own settings`}>
      {#if !ssh.installed}
        <p class="muted">OpenSSH's server isn't installed.</p>
      {:else}
        <ul class="facts">
          <li><span>SSH server</span><Badge tone={ssh.running ? 'ok' : 'bad'}>{ssh.running ? 'running' : 'stopped'}</Badge>{#if ssh.ports.length}<span class="faint">port {ssh.ports.join(', ')}</span>{/if}</li>
          <li><span>Keys</span><Badge tone={ssh.pubkeyAuth ? 'ok' : 'neutral'}>{ssh.pubkeyAuth ? 'on' : 'off'}</Badge></li>
          <li><span>Passwords</span><Badge tone={ssh.passwordAuth ? 'warn' : 'ok'}>{ssh.passwordAuth ? 'on' : 'off'}</Badge></li>
          <li><span>One-time codes (2FA)</span><Badge tone={ssh.totp ? 'ok' : 'neutral'}>{ssh.totp ? 'asked for' : 'not set up'}</Badge></li>
          <li><span>Root</span><Badge tone={ssh.rootLogin === 'yes' ? 'warn' : 'ok'}>{ssh.rootLogin === 'yes' ? 'can log in' : ssh.rootLogin === 'no' ? 'no logins' : 'keys only'}</Badge></li>
          {#if ssh.authMethods}<li><span>Needs</span><code>{ssh.authMethods}</code></li>{/if}
          {#if ssh.allowUsers?.length}<li><span>Only</span><code class="wrap">{ssh.allowUsers.join(' ')}</code></li>{/if}
        </ul>
        <p class="faint note">
          Security keys (<code>ed25519-sk</code>) are the SSH form of passkeys: the private key never leaves the device and each login needs a touch.
          {#if !ssh.totp}One-time codes need <code>libpam-google-authenticator</code> in sshd's PAM settings; each person then runs <code>google-authenticator</code> themselves, so the secret never passes through the panel.{/if}
          Who may log in, and from where, is <code>SSH_ALLOW</code> in host.env (<code>sudo cirrusux-setup apply ssh</code>).
        </p>
      {/if}
    </Card>

    <div class="grid">
      {#each shown as u (u.name)}
        <AccountCard user={u} {ssh} {panelKey} />
      {/each}
    </div>
    {#if others.length}
      <p class="faint note">Accounts without a login shell (they can't use SSH): {others.slice(0, 12).map((u) => u.name).join(', ')}{others.length > 12 ? ` and ${others.length - 12} more` : ''}.</p>
    {/if}
    <p class="faint note">Creating and deleting accounts stays on the server (<code>adduser</code>, <code>deluser</code>): the panel adds and removes keys and locks accounts, each confirmed, logged and undoable.</p>
    {#if acc.errors?.length}<p class="faint note">Couldn't read: {acc.errors.join('; ')}</p>{/if}
  {/if}
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr)); gap: var(--space-4); align-items: start; }
  .facts { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr)); gap: 8px 16px; }
  .facts li { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 0.9rem; }
  .facts li > span:first-child { color: var(--text-muted); }
  .wrap { overflow-wrap: anywhere; }
  .note { font-size: 0.82rem; margin-top: var(--space-3); }
  p { font-size: 0.92rem; }
</style>
