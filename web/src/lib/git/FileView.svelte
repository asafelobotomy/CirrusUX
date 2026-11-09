<script lang="ts">
  import { fmtBytes } from '../format';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { rawUrl, type Blob } from './api';
  import { mode } from '../api.svelte';
  import { Download } from '../icons';

  let { repo, ref, blob }: { repo: string; ref: string; blob: Blob } = $props();
  const lines = $derived(blob.text.replace(/\n$/, '').split('\n'));
  const numbers = $derived(lines.map((_, i) => i + 1).join('\n'));
</script>

<Card pad={false}>
  <header class="head gx-row">
    <strong class="gx-mono">{blob.path.split('/').pop()}</strong>
    <span class="faint gx-small">{fmtBytes(blob.size, false, true)}{blob.binary || blob.tooLarge ? '' : ` · ${lines.length} line${lines.length === 1 ? '' : 's'}`}</span>
    <span class="spacer"></span>
    {#if mode.live}
      <Button size="sm" variant="ghost" onclick={() => (location.href = rawUrl(repo, ref, blob.path))}>{#snippet icon()}<Download />{/snippet}Download</Button>
    {/if}
  </header>
  {#if blob.binary}
    <p class="muted note">This file isn't text, so it isn't shown. Download it to open it.</p>
  {:else if blob.tooLarge}
    <p class="muted note">This file is over 1 MB, too large to show here. Download it to open it.</p>
  {:else}
    <div class="code"><pre class="n" aria-hidden="true">{numbers}</pre><pre class="t">{blob.text}</pre></div>
  {/if}
</Card>

<style>
  .head { padding: 10px var(--space-4); border-bottom: 1px solid var(--border); }
  .spacer { flex: 1; }
  .note { padding: var(--space-4); }
  .code { display: flex; overflow-x: auto; font-family: var(--font-mono); font-size: 0.82rem; line-height: 1.5; }
  pre { margin: 0; padding: 10px 12px; }
  .n { color: var(--text-faint); text-align: right; user-select: none; border-right: 1px solid var(--border); flex: none; }
  .t { flex: 1; }
</style>
