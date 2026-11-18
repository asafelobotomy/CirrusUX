<script lang="ts">
  // Adding a source: the gateway fetches the catalogue once with the key the
  // owner typed and checks it as Check CirrusStore would, keeping nothing.
  // Trusting it is still done on the server, by root, from the line shown.
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import { api, mode } from '../api.svelte';
  import { toast } from '../state/ui.svelte';

  interface PreviewApp { id: string; name: string; group: string; serial: number; image?: string; problem?: string }
  interface Preview { name: string; url: string; fingerprint: string; key: string; expires: string; apps: PreviewApp[]; entry: string }

  let name = $state('');
  let url = $state('');
  let key = $state('');
  let busy = $state(false);
  let error = $state('');
  let preview = $state<Preview | null>(null);

  // A pasted NAME.pub line names the source too.
  $effect(() => {
    const f = key.trim().split(/\s+/);
    if (f.length === 4 && f[0] === 'cirrusstore-key' && !name) name = f[2];
  });

  const usable = $derived(preview ? preview.apps.filter((a) => !a.problem).length : 0);
  const command = $derived(
    preview ? `sudo nano /etc/cirrusux/host.env\n#   STORE_SOURCES="… ${preview.entry}"\nsudo cirrusux-setup apply store` : '',
  );

  async function check(e: Event): Promise<void> {
    e.preventDefault();
    error = '';
    preview = null;
    if (!mode.live) {
      error = 'Checking a source needs the gateway; this is the demo.';
      return;
    }
    busy = true;
    try {
      preview = await api<Preview>('/api/store/preview', { method: 'POST', body: JSON.stringify({ name: name.trim(), url: url.trim(), key: key.trim() }) });
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }

  async function copy(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      toast('Copied');
    } catch {
      toast("Couldn't copy: select it instead", 'warn');
    }
  }
</script>

<form class="add" onsubmit={check}>
  <p class="small">
    Get the catalogue's URL and its public key (<code>NAME.pub</code>) from its publisher. The panel fetches the catalogue once and checks every file
    against that key; nothing is kept, and nothing changes on {mode.server}.
  </p>
  <label>Public key <input bind:value={key} spellcheck="false" autocomplete="off" placeholder="cirrusstore-key v1 friends AAAA… (or just its last field)" required /></label>
  <div class="row">
    <label>Name <input bind:value={name} spellcheck="false" autocomplete="off" placeholder="friends" required /></label>
    <label class="grow">URL <input bind:value={url} spellcheck="false" autocomplete="off" type="url" placeholder="https://apps.example.org/catalogue/" required /></label>
  </div>
  <div><Button type="submit" size="sm" disabled={busy}>{busy ? 'Checking…' : 'Check this source'}</Button></div>
  {#if error}<p class="small err" role="alert">{error}</p>{/if}
</form>

{#if preview}
  <div class="result" aria-live="polite">
    <p class="small">
      <Badge tone="ok">verified</Badge>
      <strong>{preview.name}</strong> signs {preview.apps.length} app{preview.apps.length === 1 ? '' : 's'} with this key, {usable} usable here. Signed
      until {new Date(preview.expires).toLocaleDateString()}.
    </p>
    <p class="small">Compare this fingerprint with the one the publisher gives you, over a different channel than the key came by:</p>
    <code class="small fp">{preview.fingerprint}</code>
    <ul class="apps">
      {#each preview.apps as a (a.id)}
        <li class:off={!!a.problem}>
          <span><strong>{a.name}</strong> <span class="faint">{a.group}</span></span>
          {#if a.image}<code class="faint small">{a.image}</code>{/if}
          {#if a.problem}<span class="small err">{a.problem}</span>{/if}
        </li>
      {/each}
    </ul>
    <p class="small">
      If you trust its publisher, add it on {mode.server}: put this at the end of <code>STORE_SOURCES</code>, after a space if other sources are already
      there, then apply the store module. Apps from it stay limited like any other (pinned images, no privileged mode or host networking), and
      {mode.server} checks each one against this key again before installing it.
    </p>
    <pre>{command}</pre>
    <div><Button size="sm" onclick={() => copy(preview!.entry)}>Copy the STORE_SOURCES entry</Button></div>
  </div>
{/if}

<style>
  .add { display: flex; flex-direction: column; gap: var(--space-2); margin: var(--space-2) 0; }
  .row { display: flex; gap: var(--space-2); flex-wrap: wrap; }
  .grow { flex: 1 1 240px; }
  label { display: flex; flex-direction: column; gap: 3px; font-size: 0.8rem; color: var(--text-muted); }
  input {
    height: 32px; padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong);
    background: var(--surface-2); color: var(--text); font: inherit; font-size: 0.86rem; min-width: 0;
  }
  .result { display: flex; flex-direction: column; gap: var(--space-2); margin-top: var(--space-2); }
  .apps { list-style: none; margin: 0; padding: 0; }
  .apps li { display: flex; flex-direction: column; gap: 2px; padding: 6px 0; border-bottom: 1px solid var(--border); }
  .apps li.off strong { color: var(--text-muted); }
  .small { font-size: 0.8rem; }
  .fp { word-break: break-all; }
  .err { color: var(--bad); }
  p { margin: 0; }
  pre { margin: 0; padding: 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-size: 0.78rem; overflow: auto; }
</style>
