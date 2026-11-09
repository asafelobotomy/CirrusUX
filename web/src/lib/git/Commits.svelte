<script lang="ts">
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { getCommits } from './api';
  import { gitHref } from './diff';
  import { fmtAgo, fmtDayTime } from '../time.svelte';
  import { GitCommit } from '../icons';

  let { repo, ref }: { repo: string; ref: string } = $props();
  let page = $state(1);
  const commits = $derived(getCommits(repo, ref, '', page));
</script>

<Card pad={false}>
  {#await commits}
    <p class="muted pad">Loading…</p>
  {:then list}
    <ul class="gx-list items">
      {#each list as c (c.sha)}
        <li>
          <span class="gx-ic"><GitCommit size={16} /></span>
          <a class="gx-link gx-grow" href={gitHref({ repo, tab: 'commit', sha: c.sha })}>
            <span class="gx-name">{c.subject}</span>
            <span class="faint gx-small" title={fmtDayTime(new Date(c.time))}>{c.author} · {fmtAgo(new Date(c.time))}</span>
          </a>
          <code class="faint gx-mono">{c.sha.slice(0, 8)}</code>
        </li>
      {:else}
        <li class="muted">No commits here.</li>
      {/each}
    </ul>
    {#if page > 1 || list.length === 50}
      <div class="gx-end pager">
        <Button size="sm" disabled={page === 1} onclick={() => page--}>Newer</Button>
        <Button size="sm" disabled={list.length < 50} onclick={() => page++}>Older</Button>
      </div>
    {/if}
  {:catch e}
    <p class="gx-warn pad">{e.message}</p>
  {/await}
</Card>

<style>
  .items > li { padding: 10px var(--space-4); }
  .pad { padding: var(--space-4); }
  .pager { margin: 0; padding: var(--space-3) var(--space-4); border-top: 1px solid var(--border); }
</style>
