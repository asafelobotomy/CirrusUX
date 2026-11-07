<script lang="ts">
  // Branches or tags, newest first. The default branch can't be deleted.
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import { type Repo } from './api';
  import { confirmGit } from './actions';
  import type { ActionReq } from '../state/actions';
  import { gitHref } from './diff';
  import { reload } from './state.svelte';
  import { fmtAgo } from '../time.svelte';
  import { GitBranch, Tag, Trash2 } from '../icons';

  let { repo, kind }: { repo: Repo; kind: 'branches' | 'tags' } = $props();

  const rows = $derived(
    kind === 'branches'
      ? repo.branches.map((b) => ({ name: b.name, commit: b.commit, time: b.time, note: b.subject }))
      : repo.tags.map((t) => ({ name: t.name, commit: t.commit, time: t.time, note: '' })),
  );

  function del(name: string) {
    const req: ActionReq = kind === 'branches'
      ? { action: 'git_branch_delete', args: { name: repo.name, branch: name } }
      : { action: 'git_tag_delete', args: { name: repo.name, tag: name } };
    confirmGit(req, reload);
  }
</script>

<Card pad={false}>
  <ul class="gx-list items">
    {#each rows as r (r.name)}
      <li>
        <span class="gx-ic">{#if kind === 'branches'}<GitBranch size={16} />{:else}<Tag size={16} />{/if}</span>
        <a class="gx-link gx-grow" href={gitHref({ repo: repo.name, tab: 'files', ref: r.name })}>
          <span class="gx-name gx-mono">{r.name}</span>
          <span class="faint gx-small">{r.note ? `${r.note} · ` : ''}{fmtAgo(new Date(r.time))}</span>
        </a>
        {#if kind === 'branches' && r.name === repo.defaultBranch}
          <Badge tone="accent">default</Badge>
        {:else}
          <Button size="sm" variant="ghost" aria-label="Delete {r.name}" onclick={() => del(r.name)}>{#snippet icon()}<Trash2 />{/snippet}Delete…</Button>
        {/if}
      </li>
    {:else}
      <li class="muted">{kind === 'branches' ? 'No branches yet.' : 'No tags. Make one with git tag, then push it.'}</li>
    {/each}
  </ul>
</Card>

<style>
  .items > li { padding: 10px var(--space-4); }
</style>
