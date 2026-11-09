<script lang="ts">
  import Button from '../components/Button.svelte';
  import { toast } from '../state/ui.svelte';
  import { Copy } from '../icons';

  let { url }: { url: string } = $props();

  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      toast('Clone address copied', 'ok');
    } catch {
      toast("Couldn't copy: select the address and copy it yourself", 'warn');
    }
  }
</script>

<div class="clone gx-row">
  <span class="faint gx-small">Clone</span>
  <input type="text" readonly value={url} aria-label="Clone address" onfocus={(e) => e.currentTarget.select()} class="gx-mono" />
  <Button size="sm" onclick={copy} aria-label="Copy the clone address">{#snippet icon()}<Copy />{/snippet}</Button>
</div>

<style>
  .clone { max-width: 640px; }
  input { flex: 1; min-width: 0; }
</style>
