<script lang="ts">
  import Card from '../components/Card.svelte';
  import Badge from '../components/Badge.svelte';
  import Button from '../components/Button.svelte';
  import { confirmAddKey, confirmLock, confirmRemoveAccount, confirmRemoveKey } from '../state/actions';
  import { agentCan, needsAgent } from '../state/live.svelte';
  import { power } from '../state/power.svelte';
  import { mode } from '../api.svelte';
  import { keyProblem, loginMethods, sshAllowed, type Account, type SSHInfo } from '../access';
  import { KeyRound, Lock, Plus, Trash2, UserLock } from '../icons';

  let { user, ssh, panelKey = '' }: { user: Account; ssh: SSHInfo; panelKey?: string } = $props();

  let adding = $state(false);
  let pasted = $state('');
  const problem = $derived(pasted ? keyProblem(pasted) : null);
  const asleep = $derived(mode.live && power.state !== 'awake');
  const methods = $derived(loginMethods(ssh, user));
  const panelKeyAdded = $derived(!!panelKey && user.keys.some((k) => k.public === panelKey.split(/\s+/).slice(0, 2).join(' ')));
  const editable = $derived(user.uid >= 1000 && user.canLogin);

  function add() {
    if (problem) return;
    confirmAddKey(user.name, pasted);
    pasted = '';
    adding = false;
  }
</script>

<Card>
  <div class="head">
    <span class="ic"><KeyRound size={18} /></span>
    <div class="who">
      <h3>{user.name}</h3>
      <p class="muted">{user.home} · {user.shell}</p>
    </div>
    <div class="tags">
      {#if user.admin}<Badge tone="accent">admin</Badge>{/if}
      {#if user.locked}<Badge tone="bad">locked</Badge>
      {:else if !user.canLogin}<Badge>no login shell</Badge>
      {:else if sshAllowed(ssh, user)}<Badge tone="ok">SSH allowed</Badge>
      {:else}<Badge tone="warn">SSH not allowed</Badge>{/if}
      {#if user.totp && ssh.totp}<Badge tone="ok">one-time code</Badge>{/if}
    </div>
  </div>

  {#if user.canLogin}
    <p class="ways">
      {#if user.locked}No login works while it's locked.
      {:else if methods.length}Logs in with {methods.join(', ')}.
      {:else}No way to log in over SSH yet: add a key.{/if}
      {#if user.uid === 0 && ssh.rootLogin !== 'yes'} Root logins: {ssh.rootLogin === 'no' ? 'off' : 'keys only'}.{/if}
    </p>
  {/if}

  {#if user.keysError}
    <p class="faint">Keys not shown: {user.keysError}</p>
  {:else if user.keys.length}
    <ul class="keys">
      {#each user.keys as k (k.fingerprint)}
        <li>
          <div class="k">
            <span class="kc">{k.comment || '(no comment)'}</span>
            <code class="fp">{k.fingerprint}</code>
            <span class="kt">
              {k.type.replace('@openssh.com', '')}
              {#if k.securityKey}<Badge tone="ok">security key (passkey)</Badge>{/if}
              {#if k.options}<Badge tone="warn">has options</Badge>{/if}
              {#if panelKey && k.public === panelKey.split(/\s+/).slice(0, 2).join(' ')}<Badge tone="info">panel terminal</Badge>{/if}
            </span>
          </div>
          {#if editable}
            <Button size="sm" variant="ghost" aria-label={`Remove ${k.comment || k.fingerprint}`} title={needsAgent('ssh_key_remove')}
              disabled={asleep || !agentCan('ssh_key_remove')} onclick={() => confirmRemoveKey(user.name, k)}>
              {#snippet icon()}<Trash2 />{/snippet}
            </Button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if editable}
    {#if adding}
      <div class="add">
        <label for={`key-${user.name}`}>Public key (the .pub file, one line)</label>
        <textarea id={`key-${user.name}`} bind:value={pasted} rows="3" spellcheck="false" placeholder="ssh-ed25519 AAAA… you@laptop"></textarea>
        {#if problem}<p class="warn">{problem}</p>{/if}
        <p class="faint">For a passkey-style login, make a key on a security key: <code>ssh-keygen -t ed25519-sk</code>.</p>
        <div class="row">
          <Button size="sm" variant="primary" disabled={!pasted || !!problem} onclick={add}>Add key</Button>
          <Button size="sm" variant="ghost" onclick={() => { adding = false; pasted = ''; }}>Cancel</Button>
        </div>
      </div>
    {/if}
    <div class="row">
      {#if !adding}
        <Button size="sm" disabled={asleep || user.locked || !agentCan('ssh_key_add')} title={needsAgent('ssh_key_add')} onclick={() => (adding = true)}>
          {#snippet icon()}<Plus />{/snippet}Add a key
        </Button>
      {/if}
      {#if panelKey && !panelKeyAdded && !user.locked}
        <Button size="sm" disabled={asleep || !agentCan('ssh_key_add')} title="Lets the panel's Terminal log in as this account with the gateway's own key"
          onclick={() => confirmAddKey(user.name, panelKey)}>
          {#snippet icon()}<Plus />{/snippet}Allow the panel's terminal
        </Button>
      {/if}
      {#if user.locked && user.lockedByPanel}
        <Button size="sm" disabled={asleep || !agentCan('account_unlock')} onclick={() => confirmLock(user.name, false)}>
          {#snippet icon()}<UserLock />{/snippet}Unlock
        </Button>
      {:else if !user.locked}
        <Button size="sm" variant="ghost" disabled={asleep || !agentCan('account_lock')} onclick={() => confirmLock(user.name, true)}>
          {#snippet icon()}<Lock />{/snippet}Lock
        </Button>
      {:else}
        <span class="faint">Expired by a date set on the server (chage -E); the panel leaves it as it is.</span>
      {/if}
      {#if user.createdByPanel}
        <Button size="sm" variant="ghost" title="Keeps the home folder and its files" disabled={asleep || !agentCan('account_remove')} onclick={() => confirmRemoveAccount(user.name)}>
          {#snippet icon()}<Trash2 />{/snippet}Remove account
        </Button>
      {/if}
    </div>
  {/if}
</Card>

<style>
  .head { display: flex; gap: var(--space-3); align-items: flex-start; flex-wrap: wrap; }
  .ic { display: grid; place-items: center; width: 36px; height: 36px; border-radius: 10px; background: var(--accent-soft); color: var(--accent); flex: none; }
  .who { flex: 1; min-width: 140px; }
  .who h3 { font-size: 1rem; }
  .who p { font-size: 0.82rem; overflow-wrap: anywhere; }
  .tags { display: flex; gap: 6px; flex-wrap: wrap; }
  .ways { font-size: 0.88rem; margin: var(--space-3) 0 0; }
  .keys { list-style: none; margin: var(--space-3) 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .keys li { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  .k { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .kc { font-weight: 550; font-size: 0.9rem; }
  .fp { font-size: 0.76rem; color: var(--text-muted); overflow-wrap: anywhere; }
  .kt { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; font-size: 0.78rem; color: var(--text-faint); }
  .add { display: flex; flex-direction: column; gap: 6px; margin-top: var(--space-3); }
  .add label { font-size: 0.85rem; font-weight: 550; }
  textarea { width: 100%; padding: 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); font-family: var(--font-mono); font-size: 0.8rem; resize: vertical; }
  .warn { color: var(--warn, var(--text)); font-size: 0.85rem; }
  .faint { font-size: 0.8rem; }
  .row { display: flex; gap: var(--space-2); flex-wrap: wrap; align-items: center; margin-top: var(--space-3); }
</style>
