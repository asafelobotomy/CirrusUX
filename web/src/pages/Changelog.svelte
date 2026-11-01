<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import ChangelogWidget from '../lib/widgets/ChangelogWidget.svelte';
</script>

<Card title="Every change, with its undo" subtitle="Appended to /workspace/cloud64/CHANGELOG.md on the admin box and to the panel's audit log.">
  <div class="cq"><ChangelogWidget /></div>
</Card>

<style>.cq { container-type: inline-size; }</style>
