<script lang="ts">
  // A repository's builds, newest first, and the button that starts one.
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import BuildIcon from './BuildIcon.svelte';
  import { listBuilds, startBuild, type Repo } from './api';
  import { gitHref } from './diff';
  import { isOver, PIPELINE_FILE, stateText, stateTone, took, type BuildRun } from './builds';
  import { gitData } from './state.svelte';
  import { ask, toast } from '../state/ui.svelte';
  import { mode } from '../api.svelte';
  import { fmtAgo, fmtDayTime, now } from '../time.svelte';
  import { Play } from '../icons';

  let { repo }: { repo: Repo } = $props();
  let ref = $state('');
  $effect(() => {
    ref = repo.defaultBranch;
  });

  let list = $state<BuildRun[] | null>(null);
  let error = $state('');
  async function load() {
    try {
      list = await listBuilds(repo.name);
      error = '';
    } catch (e) {
      error = (e as Error).message;
    }
  }
  $effect(() => {
    void gitData.v;
    void load();
  });
  // While a build is waiting or running, follow it.
  $effect(() => {
    if (!list?.some((b) => !isOver(b.state))) return;
    const t = setTimeout(load, 3000);
    return () => clearTimeout(t);
  });

  function run() {
    const at = ref;
    ask({
      title: `Build ${repo.name} at ${at}?`,
      body: `${mode.server} runs the steps in ${PIPELINE_FILE} from the latest commit on ${at}, each in a fresh container that can reach the internet but not your network. It downloads the images the steps name.`,
      command: `build_run repo=${repo.name} ref=${at}`,
      confirmLabel: 'Run build',
      changelog: `Started a build of ${repo.name} (${at})`,
      run: async () => {
        if (!mode.live) {
          toast('Demo: nothing was built', 'info');
          return;
        }
        try {
          const b = await startBuild(repo.name, at);
          location.hash = gitHref({ repo: repo.name, tab: 'build', build: b.id });
        } catch (e) {
          toast((e as Error).message, 'warn');
        }
      },
    });
  }
</script>

<Card pad={false}>
  <div class="bar gx-row">
    <label class="gx-row">
      <span class="muted gx-small">Branch or tag</span>
      <select bind:value={ref} aria-label="Branch or tag to build">
        {#each repo.branches as b (b.name)}<option value={b.name}>{b.name}</option>{/each}
        {#each repo.tags as t (t.name)}<option value={t.name}>{t.name}</option>{/each}
      </select>
    </label>
    <span class="spacer"></span>
    <Button variant="primary" size="sm" onclick={run} disabled={repo.empty}>{#snippet icon()}<Play />{/snippet}Run build</Button>
  </div>

  {#if error}
    <p class="gx-warn pad">{error}</p>
  {:else if !list}
    <p class="muted pad">Loading…</p>
  {:else}
    <ul class="gx-list items">
      {#each list as b (b.id)}
        <li>
          <BuildIcon state={b.state} />
          <a class="gx-link gx-grow" href={gitHref({ repo: repo.name, tab: 'build', build: b.id })}>
            <span class="gx-name">{b.subject || b.commit.slice(0, 8)}</span>
            <span class="faint gx-small" title={fmtDayTime(new Date(b.created))}>
              {b.ref} · <code class="gx-mono">{b.commit.slice(0, 8)}</code> · {b.who} · {fmtAgo(new Date(b.created))}
            </span>
          </a>
          {#if b.started}<span class="faint gx-small">{took(b.started, b.finished, now())}</span>{/if}
          <Badge tone={stateTone(b.state)}>{stateText(b.state)}</Badge>
        </li>
      {:else}
        <li class="empty">
          <p class="muted">No builds yet. Add <code>{PIPELINE_FILE}</code> to the repository to say what to run, then press <strong>Run build</strong>. For example:</p>
          <pre class="gx-code">steps:
  - name: test
    image: golang:1.25
    commands:
      - go vet ./...
      - go test ./...</pre>
        </li>
      {/each}
    </ul>
  {/if}
</Card>

<style>
  .bar { padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border); }
  .bar select { max-width: 220px; }
  .spacer { flex: 1; }
  .items > li { padding: 10px var(--space-4); }
  .empty { flex-direction: column; align-items: stretch !important; }
  .pad { padding: var(--space-4); }
</style>
