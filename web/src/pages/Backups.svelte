<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import BackupsWidget from '../lib/widgets/BackupsWidget.svelte';
</script>

<div class="stack">
  <Card title="System backups" subtitle="Archives the backup unit wrote, newest first">
    <BackupsWidget full />
  </Card>
  <Card title="Restore" subtitle="Restoring happens off the server: the private key never goes on it.">
    <p class="muted">On a machine that holds the age identity:</p>
    <pre><code>age -d -i &lt;identity-file&gt; FILE.tar.zst.age | tar --zstd -xf - -C &lt;target&gt;</code></pre>
  </Card>
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--space-4); }
  pre { margin: var(--space-3) 0 0; padding: var(--space-3); border-radius: var(--radius-sm); background: var(--surface-2); border: 1px solid var(--border); overflow-x: auto; }
</style>
