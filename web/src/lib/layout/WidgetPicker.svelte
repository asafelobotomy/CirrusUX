<script lang="ts">
  import Dialog from '../components/Dialog.svelte';
  import Button from '../components/Button.svelte';
  import { WIDGETS, usable } from '../widgets/registry';
  import { addWidget } from '../state/layout.svelte';
  import { toast, ui } from '../state/ui.svelte';
  import { Plus } from '../icons';

  let { open = $bindable(false) }: { open?: boolean } = $props();

  function add(type: string) {
    const m = WIDGETS[type];
    const w = ui.boardDevice === 'phone' ? 4 : ui.boardDevice === 'tablet' ? Math.min(8, m.size[0] + 1) : m.size[0];
    addWidget(ui.boardDevice, type, w, m.size[1], m.config && { ...m.config });
    open = false;
    toast(`Added ${m.title} to the ${ui.boardDevice} layout`, 'ok');
  }
</script>

<Dialog bind:open label="Add a widget" wide>
  <h2>Add a widget</h2>
  <p class="muted sub">Adds to the {ui.boardDevice} layout of this board. Drag it into place afterwards.</p>
  <ul>
    {#each Object.entries(WIDGETS).filter(([type]) => usable(type)) as [type, m]}
      {@const Icon = m.icon}
      <li>
        <button onclick={() => add(type)}>
          <span class="ic"><Icon size={20} /></span>
          <span class="txt"><strong>{m.title}</strong><span class="muted">{m.description}</span></span>
          <Plus size={16} />
        </button>
      </li>
    {/each}
  </ul>
  <div class="row"><Button variant="ghost" onclick={() => (open = false)}>Close</Button></div>
</Dialog>

<style>
  h2 { font-size: 1.2rem; }
  .sub { margin: 4px 0 var(--space-4); font-size: 0.9rem; }
  ul { list-style: none; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: var(--space-2); }
  button {
    width: 100%; display: flex; align-items: center; gap: var(--space-3); text-align: left;
    padding: var(--space-3); border-radius: var(--radius); border: 1px solid var(--border); background: var(--surface-2);
    transition: border-color var(--dur-1) var(--ease), background var(--dur-1) var(--ease);
  }
  button:hover { border-color: var(--accent-line); background: var(--surface-3); }
  .ic { display: grid; place-items: center; width: 40px; height: 40px; flex: none; border-radius: 11px; background: var(--accent-soft); color: var(--accent); }
  .txt { flex: 1; display: flex; flex-direction: column; gap: 2px; font-size: 0.86rem; min-width: 0; }
  .txt strong { font-size: 0.95rem; }
  .row { display: flex; justify-content: flex-end; margin-top: var(--space-4); }
</style>
