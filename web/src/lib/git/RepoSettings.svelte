<script lang="ts">
  // Description, default branch, rename and delete. Each is confirmed,
  // logged and can be undone from the Changelog.
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { type Repo } from './api';
  import { confirmGit } from './actions';
  import { validRepoName } from './diff';
  import { reload } from './state.svelte';
  import { go } from '../state/ui.svelte';
  import { Trash2 } from '../icons';

  let { repo }: { repo: Repo } = $props();

  // Start from the repository's values; the fields are the person's to edit.
  let description = $state('');
  let branch = $state('');
  let newName = $state('');
  $effect.pre(() => {
    description = repo.description;
    branch = repo.defaultBranch;
    newName = repo.name;
  });
</script>

<div class="stack">
  <Card title="Description">
    <form class="gx-row" onsubmit={(e) => { e.preventDefault(); confirmGit({ action: 'git_describe', args: { name: repo.name, description } }, reload); }}>
      <input type="text" bind:value={description} maxlength="200" aria-label="Description" />
      <Button size="sm" type="submit" disabled={description === repo.description}>Save…</Button>
    </form>
  </Card>

  <Card title="Default branch" subtitle="What new clones check out, and what the panel shows first.">
    {#if repo.branches.length}
      <form class="gx-row" onsubmit={(e) => { e.preventDefault(); confirmGit({ action: 'git_default_branch', args: { name: repo.name, branch } }, reload); }}>
        <select bind:value={branch} aria-label="Default branch">
          {#each repo.branches as b (b.name)}<option value={b.name}>{b.name}</option>{/each}
        </select>
        <Button size="sm" type="submit" disabled={branch === repo.defaultBranch}>Make default…</Button>
      </form>
    {:else}
      <p class="muted">Push a branch first.</p>
    {/if}
  </Card>

  <Card title="Rename" subtitle="The clone address changes with it.">
    <form class="gx-row" onsubmit={(e) => {
      e.preventDefault();
      const to = newName;
      confirmGit({ action: 'git_rename', args: { name: repo.name, to } }, () => { go('git', encodeURIComponent(to) + '/settings'); reload(); });
    }}>
      <input type="text" bind:value={newName} aria-label="New name" spellcheck="false" />
      <Button size="sm" type="submit" disabled={newName === repo.name || !validRepoName(newName)}>Rename…</Button>
    </form>
    {#if newName && !validRepoName(newName)}<p class="gx-warn gx-small">Lower-case letters, digits, dots, dashes and underscores; up to 64.</p>{/if}
  </Card>

  <Card title="Delete" subtitle="It moves to Deleted repositories, where it can be restored.">
    <Button variant="danger" onclick={() => confirmGit({ action: 'git_delete', args: { name: repo.name } }, () => { go('git'); reload(); })}>{#snippet icon()}<Trash2 />{/snippet}Delete {repo.name}…</Button>
  </Card>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  form input { flex: 1; min-width: 160px; max-width: 420px; }
</style>
