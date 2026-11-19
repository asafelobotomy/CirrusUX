<script lang="ts">
  // One build: its steps and its log, followed live while it runs.
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import BuildIcon from './BuildIcon.svelte';
  import { cancelBuild, getBuild, getBuildLog } from './api';
  import { gitHref } from './diff';
  import { isOver, stateText, stateTone, took, type BuildRun } from './builds';
  import { ask, toast } from '../state/ui.svelte';
  import { mode } from '../api.svelte';
  import { fmtAgo, fmtDayTime, now } from '../time.svelte';
  import { Square } from '../icons';

  let { repo, id }: { repo: string; id: string } = $props();

  let b = $state<BuildRun | null>(null);
  let log = $state('');
  let error = $state('');
  let next = 0;
  let pre: HTMLPreElement | undefined = $state();
  let follow = true;

  async function load(want: string) {
    try {
      const got = await getBuild(want);
      if (want !== id) return;
      b = got;
      if (got.logSize > next) {
        const l = await getBuildLog(want, next);
        if (want !== id) return;
        log += l.text;
        next = l.next;
      }
      error = '';
    } catch (e) {
      error = (e as Error).message;
    }
  }
  $effect(() => {
    const want = id;
    b = null;
    log = '';
    next = 0;
    void load(want);
  });
  // Until the build is over and its whole log is here, look again.
  $effect(() => {
    if (!b || (isOver(b.state) && next >= b.logSize)) return;
    const t = setTimeout(() => load(id), 2000);
    return () => clearTimeout(t);
  });
  // Keep the end of the log in view, unless you scrolled up to read.
  $effect(() => {
    void log;
    if (pre && follow) pre.scrollTop = pre.scrollHeight;
  });
  const onScroll = () => {
    if (pre) follow = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 40;
  };

  function cancel() {
    if (!b) return;
    const run = b;
    ask({
      title: 'Cancel this build?',
      body: run.state === 'queued' ? "It hasn't started; it won't." : `${mode.server} stops the step that is running and skips the rest.`,
      command: `build_cancel run=${run.id}`,
      confirmLabel: 'Cancel build',
      danger: true,
      changelog: `Cancelled the build of ${run.repo} at ${run.commit.slice(0, 8)}`,
      run: async () => {
        if (!mode.live) return;
        try {
          b = await cancelBuild(run.id);
        } catch (e) {
          toast((e as Error).message, 'warn');
        }
      },
    });
  }
</script>

<a class="faint gx-small back" href={gitHref({ repo, tab: 'builds' })}>All builds</a>
{#if error && !b}
  <p class="gx-warn">{error}</p>
{:else if !b}
  <p class="muted">Loading…</p>
{:else}
  <Card>
    <div class="head gx-row">
      <BuildIcon state={b.state} size={20} />
      <div class="gx-grow">
        <strong>{b.subject || 'Build'}</strong>
        <span class="faint gx-small" title={fmtDayTime(new Date(b.created))}>
          {b.ref} ·
          <a href={gitHref({ repo, tab: 'commit', sha: b.commit })}><code class="gx-mono">{b.commit.slice(0, 8)}</code></a>
          · started by {b.who} {fmtAgo(new Date(b.created))}
          {#if b.started} · {took(b.started, b.finished, now())}{/if}
        </span>
      </div>
      <Badge tone={stateTone(b.state)}>{stateText(b.state)}</Badge>
      {#if !isOver(b.state)}
        <Button size="sm" variant="danger" onclick={cancel}>{#snippet icon()}<Square />{/snippet}Cancel</Button>
      {/if}
    </div>
    {#if b.error}<p class="gx-warn gx-small err">{b.error}</p>{/if}
    <ol class="steps">
      {#each b.steps as s, i (s.name)}
        <li class="gx-row">
          <BuildIcon state={s.state} size={14} />
          <span class="gx-grow">{i + 1}. {s.name} <span class="faint gx-small">{s.image}</span></span>
          <span class="faint gx-small">
            {#if s.exit !== undefined && s.exit !== 0}exit code {s.exit} · {/if}
            {s.started ? took(s.started, s.finished, now()) : stateText(s.state)}
          </span>
        </li>
      {/each}
    </ol>
  </Card>

  <pre class="gx-code log" bind:this={pre} onscroll={onScroll} aria-label="Build log">{log || (b.state === 'queued' ? `Waiting for ${mode.server} to start it…` : 'No output yet.')}</pre>
{/if}

<style>
  .back { text-decoration: none; }
  .head { align-items: flex-start; }
  .err { margin: var(--space-2) 0 0; }
  .steps { list-style: none; margin: var(--space-3) 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .steps li { flex-wrap: nowrap; }
  .log { max-height: 70vh; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; min-height: 120px; }
</style>
