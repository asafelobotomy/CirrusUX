<script lang="ts">
  // Personal access tokens: git's password where the panel's sign-in
  // can't reach it (a reverse proxy's login page, scripts).
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import Dialog from '../components/Dialog.svelte';
  import { createToken, listTokens, revokeToken, type GitToken } from './api';
  import { ask, toast } from '../state/ui.svelte';
  import { fmtAgo, fmtDay } from '../time.svelte';
  import { Copy, Lock, Trash2 } from '../icons';

  let v = $state(0);
  const tokens = $derived.by(() => {
    void v;
    return listTokens();
  });
  let name = $state('');
  let scope = $state<GitToken['scope']>('read-write');
  let secret = $state('');
  let shown = $state(false);

  async function make(e: SubmitEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      const made = await createToken(name.trim(), scope);
      secret = made.secret;
      shown = true;
      name = '';
      v++;
    } catch (err) {
      toast((err as Error).message, 'warn');
    }
  }

  function revoke(t: GitToken) {
    ask({
      title: `Revoke the token "${t.name}"?`,
      body: 'Anything still using it is refused from now on. This can\'t be undone: make a new token instead.',
      command: `revoke token ${t.name}`,
      confirmLabel: 'Revoke',
      danger: true,
      changelog: `Revoked Git access token "${t.name}"`,
      run: () => void revokeToken(t.id).then(() => v++, (err: Error) => toast(err.message, 'warn')),
    });
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(secret);
      toast('Copied', 'ok');
    } catch {
      toast("Couldn't copy: select the token and copy it yourself", 'warn');
    }
  }
</script>

<Card title="Access tokens" subtitle="Passwords for git, for when it can't sign in through the panel's front end. Each one can be revoked.">
  {#await tokens then list}
    {#if list.length}
      <ul class="gx-list">
        {#each list as t (t.id)}
          <li>
            <span class="gx-ic"><Lock size={16} /></span>
            <div class="gx-grow">
              <strong>{t.name}</strong>
              <span class="faint gx-small">Made {fmtDay(new Date(t.created))} · {t.lastUsed ? `last used ${fmtAgo(new Date(t.lastUsed))}` : 'never used'}</span>
            </div>
            <Badge tone={t.scope === 'read' ? 'neutral' : 'accent'}>{t.scope === 'read' ? 'read only' : 'read and write'}</Badge>
            <Button size="sm" variant="ghost" aria-label="Revoke {t.name}" onclick={() => revoke(t)}>{#snippet icon()}<Trash2 />{/snippet}Revoke…</Button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">No tokens. Over <code>tailscale serve</code>, git signs in without one.</p>
    {/if}
  {:catch e}
    <p class="gx-warn">{e.message}</p>
  {/await}
  <form class="gx-row make" onsubmit={make}>
    <input type="text" bind:value={name} placeholder="What it's for, e.g. Laptop" maxlength="60" aria-label="Token name" />
    <select bind:value={scope} aria-label="What the token may do">
      <option value="read-write">Read and write</option>
      <option value="read">Read only</option>
    </select>
    <Button size="sm" type="submit" disabled={!name.trim()}>Make a token</Button>
  </form>
</Card>

<Dialog bind:open={shown} label="Your new token">
  <div class="gx">
    <h2>Your new token</h2>
    <p class="muted">Use it as the password when git asks. It won't be shown again.</p>
    <pre class="gx-code">{secret}</pre>
    <div class="gx-end">
      <Button onclick={copy}>{#snippet icon()}<Copy />{/snippet}Copy</Button>
      <Button variant="primary" onclick={() => ((shown = false), (secret = ''))}>Done</Button>
    </div>
  </div>
</Dialog>

<style>
  .make { margin-top: var(--space-4); }
  .make input { flex: 1; min-width: 160px; }
  h2 { font-size: 1.2rem; }
  pre { margin-top: var(--space-3); overflow-wrap: anywhere; white-space: pre-wrap; }
</style>
