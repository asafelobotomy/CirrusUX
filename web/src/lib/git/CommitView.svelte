<script lang="ts">
  // One commit: its message and, per file, a unified diff.
  import Card from '../components/Card.svelte';
  import Badge from '../components/Badge.svelte';
  import { getCommit } from './api';
  import { gitHref, parsePatch } from './diff';
  import { fmtDayTime } from '../time.svelte';

  let { repo, sha }: { repo: string; sha: string } = $props();
  const detail = $derived(getCommit(repo, sha));
  const tones = { added: 'ok', deleted: 'bad', modified: 'info', renamed: 'neutral' } as const;
</script>

{#await detail}
  <p class="muted">Loading…</p>
{:then d}
  {@const [subject, ...rest] = d.message.split('\n')}
  <Card>
    <h2>{subject}</h2>
    {#if rest.join('\n').trim()}<pre class="msg">{rest.join('\n').trim()}</pre>{/if}
    <p class="faint gx-small meta">
      {d.author} &lt;{d.email}&gt; · {fmtDayTime(new Date(d.time))} · <code class="gx-mono">{d.sha.slice(0, 12)}</code>
      {#each d.parents as p (p)} · parent <a href={gitHref({ repo, tab: 'commit', sha: p })} class="gx-mono">{p.slice(0, 8)}</a>{/each}
    </p>
  </Card>
  {#if d.truncated}
    <p class="gx-warn gx-small">This commit is large, so only part of it is shown. Clone it to see all of it.</p>
  {/if}
  {#each d.files as f (f.path)}
    <Card pad={false}>
      <header class="fh gx-row">
        <Badge tone={tones[f.change]}>{f.change}</Badge>
        <strong class="gx-mono path">{f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}</strong>
        <span class="spacer"></span>
        <span class="num gx-small"><span class="plus">+{f.added}</span> <span class="minus">−{f.removed}</span></span>
      </header>
      {#if f.binary}
        <p class="muted note">Not a text file.</p>
      {:else}
        {@const lines = parsePatch(f.patch)}
        {#if lines.length}
          <div class="diff" role="table" aria-label="Changes to {f.path}">
            {#each lines as l, i (i)}
              <div class="l {l.kind}" role="row">
                <span class="o" role="cell">{l.old ?? ''}</span><span class="nn" role="cell">{l.new ?? ''}</span><span class="tx" role="cell"><span class="sign" aria-hidden="true">{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}</span>{l.text}</span>
              </div>
            {/each}
          </div>
        {:else}
          <p class="muted note">No line changes (a mode change or an empty file).</p>
        {/if}
      {/if}
    </Card>
  {/each}
{:catch e}
  <p class="gx-warn">{e.message}</p>
{/await}

<style>
  h2 { font-size: 1.15rem; overflow-wrap: anywhere; }
  .msg { white-space: pre-wrap; font-family: inherit; margin: var(--space-3) 0 0; }
  .meta { margin-top: var(--space-3); overflow-wrap: anywhere; }
  .fh { padding: 10px var(--space-4); border-bottom: 1px solid var(--border); }
  .path { overflow-wrap: anywhere; }
  .spacer { flex: 1; }
  .plus { color: var(--ok); }
  .minus { color: var(--bad); }
  .note { padding: var(--space-4); }
  .diff { font-family: var(--font-mono); font-size: 0.8rem; line-height: 1.5; overflow-x: auto; }
  .l { display: grid; grid-template-columns: 3.5em 3.5em 1fr; min-width: max-content; }
  .o, .nn { color: var(--text-faint); text-align: right; padding: 0 8px; user-select: none; }
  .tx { white-space: pre; padding-right: 12px; }
  .sign { display: inline-block; width: 1.2em; color: var(--text-faint); user-select: none; }
  .add { background: color-mix(in srgb, var(--ok) 14%, transparent); }
  .del { background: color-mix(in srgb, var(--bad) 14%, transparent); }
  .hunk { background: var(--surface-2); color: var(--text-muted); }
  .hunk .tx { padding-left: 8px; }
</style>
