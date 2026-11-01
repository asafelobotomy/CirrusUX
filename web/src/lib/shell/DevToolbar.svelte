<script lang="ts">
  import { power, setAwake, sleep, wake } from '../state/power.svelte';
  import { clearAll } from '../fake/store';
  import { FlaskConical } from '../icons';

  let open = $state(false);

  function toggleActivity() {
    power.reasons = power.reasons.length ? [] : ['Jellyfin playback from tablet'];
  }
  function reset() {
    clearAll();
    location.reload();
  }
</script>

<div class="dev" class:open>
  <button class="tog" onclick={() => (open = !open)} aria-expanded={open} aria-label="Prototype controls"><FlaskConical size={15} /><span>Prototype</span></button>
  {#if open}
    <div class="panel">
      <p class="faint">Simulate server state</p>
      <div class="row">
        <button class:on={power.state === 'awake'} onclick={setAwake}>Awake</button>
        <button class:on={power.state === 'asleep'} onclick={sleep}>Asleep</button>
        <button onclick={() => { sleep(); wake(); }}>Wake sequence</button>
      </div>
      <div class="row">
        <button class:on={power.reasons.length > 0} onclick={toggleActivity}>Activity: streaming</button>
        <button onclick={reset}>Reset all settings</button>
      </div>
      <p class="faint small">Sample data from the 1 Nov brief. No server is contacted.</p>
    </div>
  {/if}
</div>

<style>
  .dev { position: fixed; left: 50%; transform: translateX(-50%); bottom: 16px; z-index: 50; display: flex; flex-direction: column-reverse; align-items: center; gap: 8px; }
  .tog {
    display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 12px; border-radius: 999px;
    border: 1px dashed var(--border-strong); background: var(--surface-3); color: var(--text-muted); font-size: 0.78rem; font-weight: 600;
  }
  .panel {
    width: 300px; padding: 12px; border-radius: var(--radius); background: var(--surface-3); border: 1px solid var(--border-strong);
    box-shadow: 0 20px 50px -20px rgb(0 0 0 / 0.6); display: flex; flex-direction: column; gap: 8px; font-size: 0.82rem;
  }
  .row { display: flex; flex-wrap: wrap; gap: 6px; }
  .row button { border: 1px solid var(--border-strong); background: var(--surface-2); border-radius: 8px; padding: 4px 9px; font-size: 0.8rem; }
  .row button.on { background: var(--accent); color: var(--on-accent); border-color: transparent; }
  .small { font-size: 0.74rem; }
  @media (max-width: 639px) { .dev { bottom: calc(86px + env(safe-area-inset-bottom)); } }
</style>
