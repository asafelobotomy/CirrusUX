<script lang="ts">
  import { mode } from '../api.svelte';
  import { askAssistant, suggestions } from '../state/assistant.svelte';
  import { Send, Sparkles } from '../icons';

  let q = $state('');
  const starters = $derived(suggestions());

  function submit(text: string) {
    if (!text.trim()) return;
    askAssistant(text.trim());
    q = '';
  }
</script>

<div class="g">
  <form onsubmit={(e) => { e.preventDefault(); submit(q); }}>
    <Sparkles size={16} />
    <input bind:value={q} placeholder="Ask {mode.assistant.name} about {mode.server}…" aria-label="Ask {mode.assistant.name}" />
    <button type="submit" aria-label="Send"><Send size={16} /></button>
  </form>
  <div class="chips">
    {#each starters as s}<button type="button" onclick={() => submit(s)}>{s}</button>{/each}
  </div>
</div>

<style>
  .g { height: 100%; display: flex; flex-direction: column; gap: var(--space-3); justify-content: center; }
  form {
    display: flex; align-items: center; gap: 8px; padding: 0 6px 0 12px; height: 44px;
    border-radius: var(--radius); border: 1px solid var(--accent-line);
    background: linear-gradient(90deg, var(--accent-soft), transparent 70%), var(--surface-2);
    color: var(--accent);
  }
  input { flex: 1; min-width: 0; border: 0; background: transparent; outline: none; color: var(--text); }
  input::placeholder { color: var(--text-faint); }
  form button { border: 0; background: var(--accent); color: var(--on-accent); width: 32px; height: 32px; border-radius: var(--radius-sm); display: grid; place-items: center; }
  form:focus-within { outline: 2px solid var(--accent); outline-offset: 2px; }
  .chips { display: flex; gap: 6px; flex-wrap: wrap; overflow: hidden; max-height: 64px; }
  .chips button {
    border: 1px solid var(--border); background: var(--surface-2); color: var(--text-muted);
    border-radius: 999px; padding: 4px 10px; font-size: 0.8rem;
  }
  .chips button:hover { color: var(--text); border-color: var(--border-strong); }
</style>
