<script lang="ts">
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import NewRepoDialog from './NewRepoDialog.svelte';
  import { listRepos } from './api';
  import { gitHref } from './diff';
  import { gitData } from './state.svelte';
  import { mode } from '../api.svelte';
  import { fmtBytes } from '../format';
  import { fmtAgo } from '../time.svelte';
  import { Download, FolderGit2, Plus } from '../icons';

  const repos = $derived.by(() => {
    void gitData.v;
    return listRepos();
  });
  const host = $derived(mode.setup === 'split' ? mode.gateway : mode.server);
  let dialog = $state<'create' | 'import' | null>(null);
</script>

<Card title="Repositories" subtitle="Kept on {host}. Clone, push and pull them like any Git remote.">
  {#snippet actions()}
    <Button size="sm" variant="primary" onclick={() => (dialog = 'create')}>{#snippet icon()}<Plus />{/snippet}New repository</Button>
    <Button size="sm" onclick={() => (dialog = 'import')}>{#snippet icon()}<Download />{/snippet}Import</Button>
  {/snippet}
  {#await repos}
    <p class="muted">Loading…</p>
  {:then list}
    {#if list.length}
      <ul class="gx-list">
        {#each list as r (r.name)}
          <li>
            <span class="gx-ic"><FolderGit2 size={18} /></span>
            <a class="gx-link gx-grow" href={gitHref({ repo: r.name, tab: 'files' })}>
              <strong>{r.name}</strong>
              {#if r.description}<span class="muted gx-small">{r.description}</span>{/if}
            </a>
            {#if r.empty}<Badge tone="neutral">empty</Badge>{/if}
            <span class="faint gx-small">{r.lastCommit ? `Updated ${fmtAgo(new Date(r.lastCommit))}` : 'No commits yet'}</span>
            <span class="num muted gx-small">{fmtBytes(r.size)}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">No repositories yet. Make a new one, or import one from GitHub or anywhere else.</p>
    {/if}
  {:catch e}
    <p class="gx-warn">{e.message}</p>
  {/await}
</Card>

<NewRepoDialog bind:kind={dialog} />
