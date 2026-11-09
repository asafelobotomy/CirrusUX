<script lang="ts">
  // Git hosting: repositories kept by the gateway, cloned and pushed over
  // HTTPS, browsed here. #/git lists them; #/git/<name>/... shows one.
  import '../lib/git/git.css';
  import { ui } from '../lib/state/ui.svelte';
  import { mode } from '../lib/api.svelte';
  import { parseGitRoute } from '../lib/git/diff';
  import RepoList from '../lib/git/RepoList.svelte';
  import Tokens from '../lib/git/Tokens.svelte';
  import Trash from '../lib/git/Trash.svelte';
  import RepoView from '../lib/git/RepoView.svelte';

  const route = $derived(parseGitRoute(ui.sub));
</script>

<div class="gx">
  {#if !mode.git}
    <p class="muted">Git hosting is off. Set <code>CIRRUS_GIT=1</code> in the gateway's settings to turn it on (docs/GIT.md).</p>
  {:else if route.repo}
    {#key route.repo}<RepoView {route} />{/key}
  {:else}
    <div class="stack">
      <RepoList />
      <Tokens />
      <Trash />
    </div>
  {/if}
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
</style>
