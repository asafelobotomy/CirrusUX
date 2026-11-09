<script lang="ts">
  import Card from '../lib/components/Card.svelte';
  import ChangelogWidget from '../lib/widgets/ChangelogWidget.svelte';
</script>

<Card title="Every change, with its undo" subtitle="Kept in the panel's audit log, and in a CHANGELOG file too if the gateway is set up with one.">
  <div class="cq"><ChangelogWidget /></div>
</Card>

<style>.cq { container-type: inline-size; }</style>
