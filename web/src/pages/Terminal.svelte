<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import Card from '../lib/components/Card.svelte';
  import Button from '../lib/components/Button.svelte';
  import Segmented from '../lib/components/Segmented.svelte';
  import { live } from '../lib/state/live.svelte';
  import { power } from '../lib/state/power.svelte';
  import { ui, go } from '../lib/state/ui.svelte';
  import { mode } from '../lib/api.svelte';
  import { people } from '../lib/access';
  import { openSession, type Session } from '../lib/terminal';
  import { Play, Square } from '../lib/icons';

  // #/terminal/<kind>/<user> picks both, e.g. from the File transfer page.
  const [subKind, subUser] = ui.sub.split('/');
  let kind = $state<'ssh' | 'sftp'>(subKind === 'sftp' ? 'sftp' : 'ssh');
  const accounts = $derived(people(live.access).filter((u) => !u.locked));
  let user = $state(subUser || '');
  $effect(() => {
    if (!user && accounts.length) user = accounts[0].name;
  });

  let host = $state<HTMLDivElement>();
  let session = $state<Session | null>(null);
  let status = $state<'idle' | 'connecting' | 'open' | 'ended'>('idle');
  let error = $state('');
  const asleep = $derived(mode.live && power.state !== 'awake');

  async function connect() {
    error = '';
    status = 'connecting';
    session?.dispose();
    await tick(); // the terminal's box is shown before xterm measures it
    try {
      session = await openSession(host!, { user, kind }, () => (status = 'ended'));
      status = 'open';
      go('terminal', `${kind}/${user}`);
    } catch (e) {
      error = (e as Error).message;
      status = 'idle';
    }
  }

  function disconnect() {
    session?.close();
  }

  onDestroy(() => session?.close(true));
</script>

<div class="stack">
  {#if !mode.live}
    <Card><p class="muted">The terminal needs a gateway: in the demo there's no server to connect to.</p></Card>
  {:else}
    <Card>
      <div class="bar">
        <Segmented label="Session" bind:value={kind} options={[{ value: 'ssh', label: 'SSH' }, { value: 'sftp', label: 'SFTP' }]} />
        <label class="who">
          <span>as</span>
          {#if accounts.length}
            <select bind:value={user} disabled={status === 'open' || status === 'connecting'}>
              {#each accounts as a (a.name)}<option value={a.name}>{a.name}</option>{/each}
            </select>
          {:else}
            <input bind:value={user} placeholder="login name" autocomplete="username" spellcheck="false" disabled={status === 'open'} />
          {/if}
        </label>
        <span class="grow"></span>
        {#if status === 'open'}
          <Button size="sm" variant="ghost" onclick={disconnect}>{#snippet icon()}<Square />{/snippet}Disconnect</Button>
        {:else}
          <Button size="sm" variant="primary" disabled={!user || status === 'connecting' || asleep} onclick={connect}>
            {#snippet icon()}<Play />{/snippet}{status === 'connecting' ? 'Connecting…' : status === 'ended' ? 'Connect again' : 'Connect'}
          </Button>
        {/if}
      </div>
      {#if asleep}<p class="muted">{mode.server} is asleep. Wake it first.</p>{/if}
      {#if error}<p class="err">{error}</p>{/if}
    </Card>
    <div class="term" class:on={status !== 'idle'} bind:this={host}></div>
    <p class="faint note">
      An OpenSSH {kind === 'sftp' ? 'sftp' : 'ssh'} session from {mode.gateway} to {mode.server}. The server signs you in as for any SSH client:
      the panel's key if the account allows it (Accounts page), else its password and one-time code. The first connection asks you to check
      the server's host key. Opening and closing are logged; a session with no typing for 30 minutes closes.
    </p>
  {/if}
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .bar { display: flex; gap: var(--space-3); align-items: center; flex-wrap: wrap; }
  .who { display: flex; gap: 8px; align-items: center; font-size: 0.9rem; color: var(--text-muted); }
  .who select, .who input { height: 32px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); color: var(--text); font: inherit; }
  .grow { flex: 1; }
  .err { color: var(--bad); font-size: 0.88rem; margin-top: var(--space-2); }
  .term { display: none; height: min(70dvh, 640px); padding: 8px; border-radius: var(--radius-lg); border: 1px solid var(--border); background: #0b0d12; overflow: hidden; }
  .term.on { display: block; }
  .note { font-size: 0.8rem; }
</style>
