<script lang="ts">
  import { fly, fade } from 'svelte/transition';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import { chat, askAssistant } from '../state/assistant.svelte';
  import { ui } from '../state/ui.svelte';
  import { has, mode, sleeps } from '../api.svelte';
  import { Database, Lock, Send, Sparkles, X, WandSparkles } from '../icons';

  let q = $state('');
  let scroller = $state<HTMLElement>();
  const suggestions = $derived([
    sleeps() ? `Is ${mode.server} asleep?` : `Is everything on ${mode.server} running?`,
    ...(has('backup') ? ['Did last night’s backup work?'] : []),
    ...(sleeps() ? ['Why did it wake last?'] : []),
    'Which containers need attention?',
  ]);

  function submit(text = q) {
    if (!text.trim() || chat.thinking) return;
    askAssistant(text.trim());
    q = '';
  }

  $effect(() => {
    chat.messages.length; chat.thinking;
    queueMicrotask(() => scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' }));
  });
</script>

{#if ui.assistantOpen}
  <div class="scrim" transition:fade={{ duration: 150 }} onclick={() => (ui.assistantOpen = false)} aria-hidden="true"></div>
  <aside class="panel" transition:fly={{ x: 40, duration: 220 }} aria-label="{mode.assistant.name}, the assistant">
    <header>
      <span class="title"><span class="mark"><Sparkles size={16} /></span>{mode.assistant.name}</span>
      <Button variant="ghost" size="sm" aria-label="Close {mode.assistant.name}" onclick={() => (ui.assistantOpen = false)}>{#snippet icon()}<X />{/snippet}</Button>
    </header>

    <div class="msgs" bind:this={scroller} aria-live="polite">
      {#if !chat.messages.length}
        <div class="intro">
          <span class="big"><Sparkles size={26} /></span>
          <h2>Ask about {mode.server}</h2>
          <p class="muted">{mode.assistant.name} reads the panel's cached snapshot{sleeps() ? ", so asking doesn't wake the server" : ''}. It can suggest actions, but nothing runs until you confirm.</p>
          <div class="sugg">
            {#each suggestions as s}<button onclick={() => submit(s)}>{s}</button>{/each}
          </div>
        </div>
      {/if}
      {#each chat.messages as m}
        <div class="msg {m.role}">
          {#if m.role === 'assistant' && (m.tools?.length || m.cached)}
            <div class="meta">
              {#if m.cached}<Badge tone="sleep">{#snippet icon()}<Database />{/snippet}from cache · server not woken</Badge>{/if}
              {#each m.tools ?? [] as t}<code class="tool">{t}</code>{/each}
            </div>
          {/if}
          <p>{m.text}</p>
          {#if m.proposal}
            <div class="prop">
              <span class="muted"><WandSparkles size={14} /> Suggested action</span>
              <Button size="sm" variant="primary" onclick={() => m.proposal?.run()}>{m.proposal.label}…</Button>
            </div>
          {/if}
        </div>
      {/each}
      {#if chat.thinking}<div class="msg assistant typing" aria-label="{mode.assistant.name} is thinking"><span></span><span></span><span></span></div>{/if}
    </div>

    <form onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <input bind:value={q} placeholder="Ask anything about the server…" aria-label="Message {mode.assistant.name}" />
      <Button variant="primary" aria-label="Send" type="submit" disabled={!q.trim() || chat.thinking}>{#snippet icon()}<Send />{/snippet}</Button>
    </form>
    <p class="policy faint"><Lock size={12} /> Summaries only · IPs, MACs and paths masked · no secrets ever sent</p>
  </aside>
{/if}

<style>
  .scrim { position: fixed; inset: 0; z-index: 39; background: rgb(0 0 0 / 0.25); }
  @media (min-width: 1100px) { .scrim { display: none; } }
  .panel {
    position: fixed; z-index: 40; top: 0; right: 0; bottom: 0; width: min(420px, 100vw);
    display: flex; flex-direction: column;
    background: var(--surface); border-left: 1px solid var(--border-strong);
    box-shadow: -30px 0 80px -40px rgb(0 0 0 / 0.6);
  }
  header { display: flex; align-items: center; justify-content: space-between; padding: 12px 12px 12px 18px; border-bottom: 1px solid var(--border); }
  .title { display: inline-flex; align-items: center; gap: 10px; font-weight: 650; }
  .mark { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 9px; background: var(--accent); color: var(--on-accent); }
  .msgs { flex: 1; overflow: auto; padding: 18px; display: flex; flex-direction: column; gap: 14px; }
  .intro { display: flex; flex-direction: column; align-items: center; text-align: center; gap: 10px; margin: auto 0; padding: 12px; }
  .big { display: grid; place-items: center; width: 58px; height: 58px; border-radius: 18px; background: var(--accent-soft); color: var(--accent); }
  .intro h2 { font-size: 1.15rem; }
  .intro p { font-size: 0.9rem; }
  .sugg { display: flex; flex-direction: column; gap: 6px; width: 100%; margin-top: 8px; }
  .sugg button { text-align: left; border: 1px solid var(--border); background: var(--surface-2); padding: 9px 12px; border-radius: var(--radius-sm); font-size: 0.88rem; color: var(--text-muted); }
  .sugg button:hover { border-color: var(--accent-line); color: var(--text); }
  .msg { max-width: 92%; font-size: 0.93rem; line-height: 1.5; }
  .msg.user { align-self: flex-end; background: var(--accent); color: var(--on-accent); padding: 9px 13px; border-radius: 16px 16px 4px 16px; }
  .msg.assistant { align-self: flex-start; background: var(--surface-2); border: 1px solid var(--border); padding: 11px 13px; border-radius: 16px 16px 16px 4px; }
  .meta { display: flex; flex-wrap: wrap; gap: 5px; margin-bottom: 8px; }
  .tool { font-size: 0.72rem; padding: 1px 6px; border-radius: 5px; background: var(--surface-3); color: var(--text-muted); }
  .prop { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--border); flex-wrap: wrap; }
  .prop span { display: inline-flex; align-items: center; gap: 5px; font-size: 0.82rem; }
  .typing { display: flex; gap: 4px; padding: 14px 16px; }
  .typing span { width: 6px; height: 6px; border-radius: 50%; background: var(--text-faint); animation: b 1s infinite ease-in-out; }
  .typing span:nth-child(2) { animation-delay: 0.15s; } .typing span:nth-child(3) { animation-delay: 0.3s; }
  @keyframes b { 0%, 80%, 100% { opacity: 0.3; transform: translateY(0); } 40% { opacity: 1; transform: translateY(-3px); } }
  form { display: flex; gap: 8px; padding: 12px 14px 6px; border-top: 1px solid var(--border); }
  form input { flex: 1; min-width: 0; height: 40px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--border-strong); background: var(--surface-2); }
  form input:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
  .policy { display: flex; align-items: center; gap: 5px; padding: 0 16px calc(10px + env(safe-area-inset-bottom)); font-size: 0.72rem; }
</style>
