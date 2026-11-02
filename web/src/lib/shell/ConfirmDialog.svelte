<script lang="ts">
  import { mode } from '../api.svelte';
  import Dialog from '../components/Dialog.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import { confirmState, runConfirmed } from '../state/ui.svelte';
  import { Sparkles, Undo2 } from '../icons';

  let open = $state(false);
  let typed = $state('');

  $effect(() => {
    open = confirmState.current !== null;
    typed = '';
  });
  $effect(() => {
    if (!open) confirmState.current = null;
  });

  const c = $derived(confirmState.current);
  const blocked = $derived(!!c?.typed && typed.trim() !== c.typed);
</script>

<Dialog bind:open label={c?.title ?? 'Confirm'}>
  {#if c}
    {#if c.source === 'assistant'}
      <div class="from"><Badge tone="accent">{#snippet icon()}<Sparkles />{/snippet}Proposed by {mode.assistant.name}</Badge></div>
    {/if}
    <h2>{c.title}</h2>
    <p class="muted body">{c.body}</p>

    <div class="cmd">
      <span class="lbl">Runs</span>
      <code>{c.command}</code>
    </div>
    {#if c.undo}
      <div class="cmd undo">
        <span class="lbl"><Undo2 size={13} /> Undo</span>
        <code>{c.undo}</code>
      </div>
    {/if}
    {#if c.preview}
      <details class="preview" open>
        <summary><span class="lbl">Writes</span> <code>{c.preview.file}</code></summary>
        <pre>{c.preview.content}</pre>
      </details>
    {/if}
    <p class="faint note">Logged to CHANGELOG with the time and who{c.undo ? ', and the undo command' : ''}.{c.undo ? '' : ' There is no undo.'}</p>

    {#if c.typed}
      <label class="typed">
        <span>Type <strong>{c.typed}</strong> to confirm</span>
        <input bind:value={typed} autocomplete="off" spellcheck="false" />
      </label>
    {/if}

    <div class="row">
      <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
      <Button variant={c.danger ? 'danger' : 'primary'} disabled={blocked} onclick={runConfirmed}>{c.confirmLabel}</Button>
    </div>
  {/if}
</Dialog>

<style>
  .from { margin-bottom: var(--space-3); }
  h2 { font-size: 1.2rem; }
  .body { margin-top: var(--space-2); }
  .cmd {
    margin-top: var(--space-4); display: flex; flex-direction: column; gap: 4px;
    padding: var(--space-3); border-radius: var(--radius-sm);
    background: var(--surface-2); border: 1px solid var(--border);
  }
  .cmd.undo { margin-top: var(--space-2); }
  .lbl { display: inline-flex; align-items: center; gap: 4px; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); font-weight: 600; }
  code { word-break: break-all; }
  .note { font-size: 0.8rem; margin-top: var(--space-2); }
  .preview { margin-top: var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  .preview summary { padding: var(--space-2) var(--space-3); cursor: pointer; display: flex; gap: 8px; align-items: center; }
  .preview pre { margin: 0; padding: var(--space-3); border-top: 1px solid var(--border); max-height: 240px; overflow: auto; font-size: 0.76rem; line-height: 1.45; white-space: pre; }
  .typed { display: flex; flex-direction: column; gap: 6px; margin-top: var(--space-4); font-size: 0.9rem; }
  .typed input {
    height: 36px; padding: 0 10px; border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong); background: var(--surface-2);
  }
  .row { display: flex; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
</style>
