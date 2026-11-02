<script lang="ts">
  import { store } from '../state/store.svelte';
  import { fmtAgo } from '../time.svelte';
  import { CircleCheck, CircleX, Loader } from '../icons';

  let { app }: { app?: string } = $props();
  const jobs = $derived(store.jobs.filter((j) => !app || j.app === app));
  const verb = { install: 'Installing', remove: 'Removing', update: 'Updating', 'os-check': 'Checking', 'os-upgrade': 'Upgrading', 'storage-scan': 'Measuring', 'archive-delete': 'Deleting' };
</script>

<ul class="jobs" aria-live="polite">
  {#each jobs as j (j.id)}
    <li class={j.state}>
      <span class="ic" aria-hidden="true">
        {#if j.state === 'running'}<span class="spin"><Loader size={18} /></span>{:else if j.state === 'done'}<CircleCheck size={18} />{:else}<CircleX size={18} />{/if}
      </span>
      <div class="txt">
        <strong>{j.state === 'running' ? `${verb[j.action]} ${j.name}` : j.step}</strong>
        {#if j.state === 'running'}<span class="muted">{j.step || 'Starting…'}</span>{/if}
        {#if j.detail}<span class="faint mono small">{j.detail}</span>{/if}
        {#if j.error}<span class="err">{j.error}</span>{/if}
      </div>
      <span class="faint small when">{fmtAgo(j.finished ?? j.started)}</span>
    </li>
  {/each}
</ul>

<style>
  .jobs { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--space-2); }
  li { display: flex; gap: var(--space-3); align-items: flex-start; padding: var(--space-2) 0; border-bottom: 1px solid var(--border); }
  li:last-child { border-bottom: 0; }
  .ic { display: inline-flex; margin-top: 2px; color: var(--info); }
  .done .ic { color: var(--ok); }
  .failed .ic { color: var(--bad); }
  .spin { display: inline-flex; animation: spin 1.1s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .txt span { overflow-wrap: anywhere; }
  .err { color: var(--bad); font-size: 0.85rem; }
  .small { font-size: 0.78rem; }
  .when { white-space: nowrap; }
  @media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
</style>
