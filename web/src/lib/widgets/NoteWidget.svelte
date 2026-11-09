<script lang="ts">
  import { ui } from '../state/ui.svelte';
  let { config }: { config?: Record<string, string> } = $props();
</script>

{#if ui.editLayout && config}
  <textarea bind:value={config.text} aria-label="Note text"></textarea>
{:else}
  <p class="note">{config?.text || 'Empty note. Switch on Edit layout to write here.'}</p>
{/if}

<style>
  .note { white-space: pre-wrap; color: var(--text-muted); font-size: 0.92rem; height: 100%; overflow: auto; }
  textarea {
    width: 100%; height: 100%; resize: none; border-radius: var(--radius-sm); padding: 8px;
    border: 1px solid var(--border-strong); background: var(--surface-2);
  }
</style>
