<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import Badge from '../lib/components/Badge.svelte';
  import { ALL_NAV } from '../lib/shell/nav';
  import { ui } from '../lib/state/ui.svelte';

  const PLANNED: Record<string, string[]> = {
    storage: ['Usage per folder, refreshed rarely (du over USB 2 counts as activity)', 'USB link-speed warning', 'SMART and btrfs health once smartmontools and btrfs-progs are installed'],
    updates: ['Image tags vs pinned versions', 'apt upgrades after an approved apt update', 'Backup → set tag → pull → health check → changelog, with restore on failure'],
    media: ['Library counts and scans', 'Archive review with two-step delete', 'AVI re-encode queue (later)'],
    logs: ['Idle log, backup journal, virus-scan log', 'Per-container logs, live tails time out after 5 min'],
  };
  const nav = $derived(ALL_NAV.find((n) => n.route === ui.route));
</script>

<Card title="{nav?.label} is planned" subtitle="Not part of this prototype. From PLAN.md:">
  {#snippet actions()}<Badge tone="accent">{nav?.phase}</Badge>{/snippet}
  <ul>{#each PLANNED[ui.route] ?? [] as p}<li>{p}</li>{/each}</ul>
</Card>

<style>
  ul { margin: 0; padding-left: 1.2em; display: flex; flex-direction: column; gap: 6px; color: var(--text-muted); }
</style>
