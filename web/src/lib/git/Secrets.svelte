<script lang="ts">
  // A repository's build secrets: names and dates only. A value is typed
  // here once, goes straight to the gateway and is never shown again.
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { ask, toast } from '../state/ui.svelte';
  import { mode } from '../api.svelte';
  import { deleteSecret, listSecrets, putSecret, type Repo } from './api';
  import type { SecretInfo } from './builds';

  let { repo }: { repo: Repo } = $props();
  let list = $state<SecretInfo[]>([]);
  let key = $state('');
  let value = $state('');
  const valid = $derived(/^[A-Z_][A-Z0-9_]{0,63}$/.test(key) && !/^(HOME|CI|PATH|CIRRUS_.*)$/.test(key) && value.length >= 8);

  $effect(() => {
    listSecrets(repo.name).then((l) => (list = l), () => {});
  });

  function save() {
    const [k, v] = [key, value];
    const replacing = list.some((s) => s.name === k);
    ask({
      title: `${replacing ? 'Replace' : 'Set'} secret ${k}?`,
      body: `Builds of ${repo.name} that name ${k} in a step's secrets get this value, for steps you start by hand. It's kept on the gateway and never shown again.`,
      command: `set_build_secret repo=${repo.name} name=${k} value=(hidden)`,
      confirmLabel: replacing ? 'Replace' : 'Save',
      changelog: `${replacing ? 'Replaced' : 'Set'} secret ${k} for builds of ${repo.name}`,
      undo: 'Delete it, or set the old value again',
      run: () => {
        if (!mode.live) return toast(`Secret ${k} saved (demo: nothing changed)`, 'info');
        putSecret(repo.name, k, v).then((l) => { list = l; key = value = ''; toast(`Secret ${k} saved`, 'ok'); }, (e) => toast(String(e?.message ?? e), 'warn'));
      },
    });
  }

  function remove(k: string) {
    ask({
      title: `Delete secret ${k}?`,
      body: `Builds of ${repo.name} that name ${k} will stop until it's set again.`,
      command: `delete_build_secret repo=${repo.name} name=${k}`,
      confirmLabel: 'Delete',
      danger: true,
      changelog: `Deleted secret ${k} for builds of ${repo.name}`,
      run: () => {
        if (!mode.live) return toast(`Secret ${k} deleted (demo: nothing changed)`, 'info');
        deleteSecret(repo.name, k).then((l) => { list = l; }, (e) => toast(String(e?.message ?? e), 'warn'));
      },
    });
  }
</script>

<Card title="Build secrets" subtitle="Values a pipeline's steps can use, such as a deploy token. A step names the secrets it wants. Builds from pushes never get them, and a value can't be read back.">
  {#if list.length}
    <ul class="stack">
      {#each list as s (s.name)}
        <li class="gx-row"><code>{s.name}</code> <span class="muted">set {new Date(s.updated).toLocaleDateString()}</span>
          <Button size="sm" onclick={() => remove(s.name)}>Delete…</Button></li>
      {/each}
    </ul>
  {:else}
    <p class="muted">No secrets yet.</p>
  {/if}
  <form class="gx-row" onsubmit={(e) => { e.preventDefault(); if (valid) save(); }}>
    <input type="text" bind:value={key} placeholder="NAME" aria-label="Secret name" spellcheck="false" autocomplete="off" />
    <input type="password" bind:value={value} placeholder="Value (8 characters or more)" aria-label="Secret value" autocomplete="off" />
    <Button size="sm" type="submit" disabled={!valid}>Save…</Button>
  </form>
</Card>
