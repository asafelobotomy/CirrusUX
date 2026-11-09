<script lang="ts">
  // A folder at a ref (or a file, when the path is one), with its README.
  import { fmtBytes } from '../format';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import FileView from './FileView.svelte';
  import { archiveUrl, getBlob, getTree, type Blob, type Repo, type Tree } from './api';
  import { gitHref, pathTrail } from './diff';
  import { ApiError, mode } from '../api.svelte';
  import { Download, File, Folder, FolderGit2 } from '../icons';

  let { repo, ref, path }: { repo: Repo; ref: string; path: string } = $props();

  type View = { tree: Tree; readme?: Blob } | { file: Blob };

  async function load(name: string, ref: string, path: string): Promise<View> {
    try {
      const tree = await getTree(name, ref, path);
      const r = tree.entries.find((e) => e.type === 'file' && /^readme(\.md|\.txt)?$/i.test(e.name));
      const readme = r ? await getBlob(name, ref, path ? `${path}/${r.name}` : r.name).catch(() => undefined) : undefined;
      return { tree, readme };
    } catch (e) {
      // A file's path: show the file.
      if (e instanceof ApiError && e.status === 400 && path) return { file: await getBlob(name, ref, path) };
      throw e;
    }
  }

  const view = $derived(load(repo.name, ref, path));
  const href = (p: string) => gitHref({ repo: repo.name, tab: 'files', ref, path: p || undefined });
  const icons = { folder: Folder, file: File, link: File, submodule: FolderGit2 };
</script>

<nav class="trail gx-row" aria-label="Path">
  <a href={href('')}>{repo.name}</a>
  {#each pathTrail(path) as p (p.path)}<span class="faint">/</span><a href={href(p.path)}>{p.name}</a>{/each}
  <span class="spacer"></span>
  {#if mode.live}
    <Button size="sm" variant="ghost" onclick={() => (location.href = archiveUrl(repo.name, ref, 'zip'))}>{#snippet icon()}<Download />{/snippet}.zip</Button>
    <Button size="sm" variant="ghost" onclick={() => (location.href = archiveUrl(repo.name, ref, 'tar.gz'))}>{#snippet icon()}<Download />{/snippet}.tar.gz</Button>
  {/if}
</nav>

{#await view}
  <p class="muted">Loading…</p>
{:then v}
  {#if 'file' in v}
    <FileView repo={repo.name} {ref} blob={v.file} />
  {:else}
    <Card pad={false}>
      <ul class="gx-list entries">
        {#each v.tree.entries as e (e.name)}
          {@const Icon = icons[e.type]}
          <li>
            <span class="gx-ic"><Icon size={16} /></span>
            {#if e.type === 'submodule'}
              <span class="gx-grow">{e.name} <span class="faint gx-small">(another repository)</span></span>
            {:else}
              <a class="gx-link gx-grow" href={href(path ? `${path}/${e.name}` : e.name)}><span class="gx-name">{e.name}</span></a>
            {/if}
            {#if e.type === 'file'}<span class="num faint gx-small">{fmtBytes(e.size, false, true)}</span>{/if}
          </li>
        {:else}
          <li class="muted">This folder is empty.</li>
        {/each}
      </ul>
    </Card>
    {#if v.readme && !v.readme.binary && !v.readme.tooLarge}
      <Card title={v.readme.path.split('/').pop()}>
        <pre class="readme">{v.readme.text}</pre>
      </Card>
    {/if}
  {/if}
{:catch e}
  <p class="gx-warn">{e.message}</p>
{/await}

<style>
  .trail { font-size: 0.95rem; }
  .trail a { text-decoration: none; }
  .trail a:hover { text-decoration: underline; }
  .spacer { flex: 1; }
  .entries > li { padding: 8px var(--space-4); }
  .readme { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; font-family: inherit; line-height: 1.55; }
</style>
