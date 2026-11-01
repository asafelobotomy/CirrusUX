<script lang="ts">
  // A dot is never the only signal: callers always show a word next to it.
  let { tone = 'ok', pulse = false }: { tone?: 'ok' | 'warn' | 'bad' | 'sleep' | 'neutral' | 'info'; pulse?: boolean } = $props();
</script>

<span class="dot {tone}" class:pulse aria-hidden="true"></span>

<style>
  .dot {
    position: relative; display: inline-block; flex: none;
    width: 8px; height: 8px; border-radius: 50%; background: var(--c);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--c) 18%, transparent);
  }
  .ok { --c: var(--ok); } .warn { --c: var(--warn); } .bad { --c: var(--bad); }
  .sleep { --c: var(--sleep); } .info { --c: var(--info); } .neutral { --c: var(--text-faint); }
  .pulse::after {
    content: ''; position: absolute; inset: 0; border-radius: 50%; background: var(--c);
    animation: ping 1.6s var(--ease) infinite;
  }
  @keyframes ping { 0% { transform: scale(1); opacity: 0.7; } 100% { transform: scale(2.6); opacity: 0; } }
  @media (prefers-reduced-motion: reduce) { .pulse::after { animation: none; } }
</style>
