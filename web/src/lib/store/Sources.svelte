<script lang="ts">
  // CirrusStore's sources: what the server trusts, and how the last check
  // went. Adding one is done on the server, never from the panel.
  import Card from '../components/Card.svelte';
  import Badge from '../components/Badge.svelte';
  import { mode } from '../api.svelte';
  import { sourceLabel, type StoreSource } from '../catalogue';
  import { store } from '../state/store.svelte';
  import { fmtAgo } from '../time.svelte';

  const tone = (s: StoreSource) => (s.status === 'ok' ? 'ok' : s.status === 'not-checked' ? 'neutral' : s.status === 'expired' ? 'warn' : 'bad');
  const label: Record<StoreSource['status'], string> = {
    ok: 'ok',
    'not-checked': 'not checked',
    expired: 'expired',
    unreachable: 'unreachable',
    refused: 'refused',
    unusable: 'unusable',
  };
</script>

<Card title="Sources" subtitle="Catalogues {mode.server} trusts, besides the apps built into CirrusUX">
  {#if !store.sources.list.length}
    <p class="muted">Only the built-in apps: {mode.server} hasn't reported any sources{store.host ? '' : ' yet'}.</p>
  {:else}
    <ul class="sources">
      {#each store.sources.list as s (s.name)}
        <li>
          <div class="top">
            <strong>{sourceLabel(s.name)}</strong>
            {#if s.official}<Badge tone="accent">official</Badge>{/if}
            <Badge tone={tone(s)}>{label[s.status]}</Badge>
            {#if s.status === 'ok'}<span class="faint small">{s.apps} app{s.apps === 1 ? '' : 's'}</span>{/if}
            {#if s.checked}<span class="faint small">checked {fmtAgo(new Date(s.checked))}</span>{/if}
          </div>
          {#if s.url}<code class="faint small">{s.url}</code>{/if}
          {#each s.fingerprints as f}<code class="small fp">key {f}</code>{/each}
          {#if s.detail || s.error}<p class="small err">{s.detail || s.error}</p>{/if}
        </li>
      {/each}
    </ul>
  {/if}
  <details>
    <summary>Add a source</summary>
    <p class="small">
      Sources are added on {mode.server}, so the panel can't add one. Get the source's URL and the last field of its public key
      (<code>NAME.pub</code>) from its publisher, compare the fingerprint shown here with theirs afterwards, and on {mode.server} run:
    </p>
    <pre>sudo nano /etc/cirrusux/host.env
#   STORE_SOURCES="name|https://example.org/catalogue/|KEY"
sudo cirrusux-setup apply store</pre>
    <p class="small faint">Every app from a source is checked against its key on {mode.server} again before it's installed. <code>STORE_OFFICIAL=no</code> leaves CirrusStore's own catalogue out.</p>
  </details>
</Card>

<style>
  .sources { list-style: none; margin: 0 0 var(--space-3); padding: 0; display: flex; flex-direction: column; }
  .sources li { display: flex; flex-direction: column; gap: 3px; padding: 10px 0; border-bottom: 1px solid var(--border); }
  .sources li:last-child { border-bottom: 0; }
  .top { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .small { font-size: 0.8rem; }
  .fp { word-break: break-all; }
  .err { color: var(--bad); }
  summary { cursor: pointer; color: var(--text-muted); font-size: 0.88rem; }
  details p { margin: var(--space-2) 0; }
  pre { margin: 0; padding: 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-size: 0.78rem; overflow: auto; }
</style>
