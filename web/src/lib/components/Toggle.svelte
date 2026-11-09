<script lang="ts">
  let { checked = $bindable(false), label, hint }: { checked?: boolean; label: string; hint?: string } = $props();
</script>

<label class="row">
  <span class="txt">
    <span>{label}</span>
    {#if hint}<span class="hint">{hint}</span>{/if}
  </span>
  <input type="checkbox" role="switch" bind:checked />
  <span class="track" aria-hidden="true"><span class="thumb"></span></span>
</label>

<style>
  .row { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); cursor: pointer; }
  .txt { display: flex; flex-direction: column; gap: 2px; }
  .hint { font-size: 0.84rem; color: var(--text-muted); }
  input { position: absolute; opacity: 0; width: 1px; height: 1px; }
  .track {
    flex: none; width: 40px; height: 24px; border-radius: 999px; padding: 3px;
    background: var(--surface-3); border: 1px solid var(--border-strong);
    transition: background var(--dur-2) var(--ease);
  }
  .thumb {
    display: block; width: 16px; height: 16px; border-radius: 50%; background: var(--text-muted);
    transition: transform var(--dur-2) var(--ease), background var(--dur-2) var(--ease);
  }
  input:checked + .track { background: var(--accent); border-color: transparent; }
  input:checked + .track .thumb { transform: translateX(16px); background: var(--on-accent); }
  input:focus-visible + .track { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>
