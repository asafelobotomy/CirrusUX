<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import Button from '../lib/components/Button.svelte';
  import { live } from '../lib/state/live.svelte';
  import { mode } from '../lib/api.svelte';
  import { go } from '../lib/state/ui.svelte';
  import { people, sshAllowed } from '../lib/access';
  import { SquareTerminal } from '../lib/icons';

  const acc = $derived(live.access);
  const ssh = $derived(acc?.ssh);
  const ftps = $derived(acc?.ftps);
  const sftpUsers = $derived(ssh ? people(acc).filter((u) => sshAllowed(ssh, u)) : []);
  const port = $derived(ssh?.ports[0] ?? 22);
  const sftpURL = (u: string) => `sftp://${u}@${mode.serverHost}${port === 22 ? '' : ':' + port}`;
</script>

<div class="stack">
  {#if !acc || !ssh}
    <Card><p class="muted">{mode.server} hasn't reported its SSH and FTPS settings. Set <code>ACCESS=yes</code> in <code>/etc/cirrusux/helper.env</code> on it.</p></Card>
  {:else}
    <Card title="SFTP" subtitle="File transfer over SSH: the same logins and keys as the Accounts page">
      {#snippet actions()}
        {#if mode.terminal && sftpUsers.length}
          <Button size="sm" onclick={() => go('terminal', 'sftp/' + sftpUsers[0].name)}>
            {#snippet icon()}<SquareTerminal />{/snippet}Open SFTP here
          </Button>
        {/if}
      {/snippet}
      {#if !ssh.installed || !ssh.sftp}
        <p class="muted">sshd has no SFTP subsystem{ssh.installed ? '' : ' (OpenSSH server not installed)'}.</p>
      {:else}
        <p><Badge tone={ssh.running ? 'ok' : 'bad'}>{ssh.running ? 'on' : 'sshd stopped'}</Badge> {ssh.sftpInternal ? "sshd's built-in SFTP server" : 'OpenSSH sftp-server'}, port {port}.</p>
        {#if sftpUsers.length}
          <p class="muted">Connect with any SFTP client (FileZilla, WinSCP, Cyberduck, <code>sftp</code>):</p>
          <ul class="urls">{#each sftpUsers as u (u.name)}<li><code>{sftpURL(u.name)}</code></li>{/each}</ul>
        {:else}
          <p class="muted">No account may log in over SSH yet: add a key on the Accounts page.</p>
        {/if}
      {/if}
    </Card>

    <Card title="FTPS" subtitle="FTP over TLS, for devices that don't speak SFTP">
      {#if !ftps}
        <p class="muted">Not set up. To add it, set <code>FTPS_USER</code> and <code>FTPS_HOME</code> in host.env and run <code>sudo cirrusux-setup apply ftps</code>; it refuses plain FTP.</p>
      {:else}
        <ul class="facts">
          <li><span>Server</span><Badge tone={ftps.running ? 'ok' : 'bad'}>{ftps.running ? 'running' : 'stopped'}</Badge></li>
          <li><span>Encryption</span>
            {#if ftps.tls === 3}<Badge tone="ok">TLS required</Badge>
            {:else}<Badge tone="bad">plain FTP allowed</Badge>{/if}
          </li>
          <li><span>Anonymous</span><Badge tone={ftps.anonymous ? 'bad' : 'ok'}>{ftps.anonymous ? 'allowed' : 'off'}</Badge></li>
          <li><span>Login</span>{#if ftps.userSet}<code>{ftps.user}</code>{:else}<Badge tone="warn">no password set</Badge>{/if}</li>
          {#if ftps.home}<li><span>Folder</span><code>{ftps.home}</code></li>{/if}
          {#if ftps.passivePorts}<li><span>Data ports</span><code>{ftps.passivePorts.replace(' ', '–')}</code></li>{/if}
        </ul>
        {#if ftps.tls !== 3}
          <p class="bad">Passwords and files can cross the network unencrypted. Run <code>sudo cirrusux-setup apply ftps</code> to require TLS again.</p>
        {/if}
        {#if ftps.anonymous}<p class="bad">Anyone can log in without a password. Run <code>sudo cirrusux-setup apply ftps</code> to turn that off.</p>{/if}
        {#if ftps.userSet && ftps.tls === 3}
          <p class="muted">Connect as <code>ftpes://{ftps.user}@{mode.serverHost}</code> (explicit TLS, port 21).</p>
        {:else if !ftps.userSet}
          <p class="muted">Choose its password on the server: <code>sudo cirrusux-setup ftps-passwd</code>. The panel never handles it.</p>
        {/if}
        {#if ftps.certSha256}
          <p class="faint fp">The certificate is self-signed. Your client asks the first time: accept it only if it shows <code>{ftps.certSha256}</code></p>
        {/if}
      {/if}
    </Card>
    <p class="faint note">Plain FTP is never offered: it sends passwords as they are.</p>
  {/if}
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  p { font-size: 0.92rem; margin-top: var(--space-2); }
  .urls { margin: var(--space-2) 0 0; padding-left: 1.2em; }
  .urls code { overflow-wrap: anywhere; }
  .facts { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr)); gap: 8px 16px; }
  .facts li { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 0.9rem; }
  .facts li > span:first-child { color: var(--text-muted); }
  .bad { color: var(--bad, var(--text)); }
  .fp code { overflow-wrap: anywhere; font-size: 0.78rem; }
  .note { font-size: 0.8rem; }
</style>
