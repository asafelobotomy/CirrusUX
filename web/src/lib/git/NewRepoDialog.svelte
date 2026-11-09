<script lang="ts">
  // New repository or import: the name (checked as the gateway checks it),
  // then a description or the address to import from.
  import Dialog from '../components/Dialog.svelte';
  import Button from '../components/Button.svelte';
  import { validRepoName } from './diff';
  import { confirmGit } from './actions';
  import type { ActionReq } from '../state/actions';
  import { reload } from './state.svelte';

  let { kind = $bindable() }: { kind: 'create' | 'import' | null } = $props();

  let open = $state(false);
  let name = $state('');
  let description = $state('');
  let url = $state('');
  $effect(() => {
    open = kind !== null;
  });
  $effect(() => {
    if (!open) kind = null;
  });

  // An import's name defaults to the last part of its address.
  $effect(() => {
    if (kind !== 'import' || name) return;
    const m = /\/([^/]+?)(\.git)?\/?$/.exec(url);
    if (m && validRepoName(m[1].toLowerCase())) name = m[1].toLowerCase();
  });

  const nameOk = $derived(validRepoName(name));
  const urlOk = $derived(/^https:\/\/[^/\s]+\/\S+/.test(url));
  const ready = $derived(nameOk && (kind === 'create' || urlOk) && description.length <= 200);

  function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!ready) return;
    const req: ActionReq = kind === 'import'
      ? { action: 'git_import', args: { name, url } }
      : { action: 'git_create', args: { name, description } };
    open = false;
    confirmGit(req, reload);
    name = description = url = '';
  }
</script>

<Dialog bind:open label={kind === 'import' ? 'Import a repository' : 'New repository'}>
  <form onsubmit={submit} class="gx">
    <h2>{kind === 'import' ? 'Import a repository' : 'New repository'}</h2>
    {#if kind === 'import'}
      <label class="gx-field">
        Address to copy from
        <input type="text" bind:value={url} placeholder="https://github.com/you/project.git" autocomplete="off" spellcheck="false" />
        <span class="faint gx-small">Fetched once, now, over https. For a private repository, put an access token in the address (https://user:token@…); it isn't kept.</span>
      </label>
    {/if}
    <label class="gx-field">
      Name
      <input type="text" bind:value={name} placeholder="my-project" autocomplete="off" spellcheck="false" />
      {#if name && !nameOk}
        <span class="gx-warn gx-small">Lower-case letters, digits, dots, dashes and underscores; starts with a letter or digit; up to 64; not ending in .git.</span>
      {/if}
    </label>
    {#if kind === 'create'}
      <label class="gx-field">
        Description (optional)
        <input type="text" bind:value={description} maxlength="200" />
      </label>
    {/if}
    <div class="gx-end">
      <Button variant="ghost" type="button" onclick={() => (open = false)}>Cancel</Button>
      <Button variant="primary" type="submit" disabled={!ready}>{kind === 'import' ? 'Import…' : 'Create…'}</Button>
    </div>
  </form>
</Dialog>

<style>
  h2 { font-size: 1.2rem; }
  form { min-width: min(440px, 80vw); }
</style>
