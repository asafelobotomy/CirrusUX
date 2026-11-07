<script lang="ts">
  // One repository: its clone address, a branch picker and the tabs.
  import Card from '../components/Card.svelte';
  import CloneBox from './CloneBox.svelte';
  import Files from './Files.svelte';
  import Commits from './Commits.svelte';
  import CommitView from './CommitView.svelte';
  import Refs from './Refs.svelte';
  import RepoSettings from './RepoSettings.svelte';
  import { getRepo } from './api';
  import { gitHref, type GitRoute, type GitTab } from './diff';
  import { gitData } from './state.svelte';
  import { mode } from '../api.svelte';
  import { FolderGit2 } from '../icons';

  let { route }: { route: GitRoute } = $props();
  const repo = $derived.by(() => {
    void gitData.v;
    return getRepo(route.repo!);
  });

  const TABS: { tab: GitTab; label: string }[] = [
    { tab: 'files', label: 'Files' },
    { tab: 'commits', label: 'Commits' },
    { tab: 'branches', label: 'Branches' },
    { tab: 'tags', label: 'Tags' },
    { tab: 'settings', label: 'Settings' },
  ];
  const active = $derived(route.tab === 'commit' ? 'commits' : route.tab);

  function pickRef(ref: string) {
    location.hash = gitHref({ repo: route.repo, tab: route.tab === 'commits' ? 'commits' : 'files', ref });
  }
</script>

{#await repo}
  <p class="muted">Loading…</p>
{:then r}
  {@const ref = route.ref ?? r.defaultBranch}
  <div class="stack">
    <header class="top">
      <a class="faint gx-small back" href="#/git">Repositories</a>
      <h1><span class="gx-ic"><FolderGit2 size={22} /></span>{r.name}</h1>
      {#if r.description}<p class="muted">{r.description}</p>{/if}
      <CloneBox url={r.cloneUrl} />
    </header>

    <nav class="tabs" aria-label="{r.name} sections">
      {#each TABS as t (t.tab)}
        <a href={gitHref({ repo: r.name, tab: t.tab, ...(t.tab === 'files' || t.tab === 'commits' ? { ref: route.ref } : {}) })}
          class:on={active === t.tab} aria-current={active === t.tab ? 'page' : undefined}>{t.label}</a>
      {/each}
      {#if (active === 'files' || active === 'commits') && r.branches.length}
        <select class="pick" value={ref} onchange={(e) => pickRef(e.currentTarget.value)} aria-label="Branch or tag">
          <optgroup label="Branches">
            {#each r.branches as b (b.name)}<option value={b.name}>{b.name}</option>{/each}
          </optgroup>
          {#if r.tags.length}
            <optgroup label="Tags">
              {#each r.tags as t (t.name)}<option value={t.name}>{t.name}</option>{/each}
            </optgroup>
          {/if}
          {#if !r.branches.some((b) => b.name === ref) && !r.tags.some((t) => t.name === ref)}<option value={ref}>{ref}</option>{/if}
        </select>
      {/if}
    </nav>

    {#if r.empty && (active === 'files' || active === 'commits')}
      <Card title="Nothing here yet" subtitle="Push an existing project to it from your computer:">
        <pre class="gx-code">cd my-project
git remote add {mode.server} {r.cloneUrl}
git push {mode.server} main</pre>
        <p class="faint gx-small hint">Or start fresh with <code>git clone {r.cloneUrl}</code>.</p>
      </Card>
    {:else if route.tab === 'files'}
      <Files repo={r} {ref} path={route.path ?? ''} />
    {:else if route.tab === 'commits'}
      <Commits repo={r.name} {ref} />
    {:else if route.tab === 'commit' && route.sha}
      <CommitView repo={r.name} sha={route.sha} />
    {:else if route.tab === 'branches' || route.tab === 'tags'}
      <Refs repo={r} kind={route.tab} />
    {:else if route.tab === 'settings'}
      <RepoSettings repo={r} />
    {/if}
  </div>
{:catch e}
  <p class="gx-warn">{e.message}</p>
  <p><a href="#/git">Back to the repositories</a></p>
{/await}

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  .top { display: flex; flex-direction: column; gap: 6px; }
  .back { text-decoration: none; }
  h1 { font-size: 1.5rem; display: flex; gap: 8px; align-items: center; overflow-wrap: anywhere; }
  .tabs { display: flex; gap: 4px; flex-wrap: wrap; align-items: center; border-bottom: 1px solid var(--border); }
  .tabs a { padding: 8px 12px; text-decoration: none; color: var(--text-muted); border-bottom: 2px solid transparent; margin-bottom: -1px; }
  .tabs a.on { color: var(--text); border-bottom-color: var(--accent); font-weight: 600; }
  .pick { margin-left: auto; margin-bottom: 4px; max-width: 220px; }
  .hint { margin-top: var(--space-3); }
</style>
