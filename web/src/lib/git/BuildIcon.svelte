<script lang="ts">
  // A build's or step's state as an icon, coloured like its badge.
  import { stateText, stateTone, type BuildState, type StepState } from './builds';
  import { CalendarClock, CircleCheck, CircleX, Loader, Square } from '../icons';

  let { state, size = 16 }: { state: BuildState | StepState; size?: number } = $props();
  const Icon = $derived(
    { passed: CircleCheck, failed: CircleX, running: Loader, queued: CalendarClock, cancelled: Square, waiting: CalendarClock, skipped: Square }[state] ?? Square,
  );
</script>

<span class="bi {stateTone(state)}" class:spin={state === 'running'} title={stateText(state)} aria-label={stateText(state)} role="img">
  <Icon {size} />
</span>

<style>
  .bi { display: inline-flex; flex: none; color: var(--text-muted); }
  .ok { color: var(--ok); }
  .bad { color: var(--bad); }
  .info { color: var(--info); }
  .warn { color: var(--warn); }
  .spin :global(svg) { animation: spin 1.6s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spin :global(svg) { animation: none; } }
</style>
