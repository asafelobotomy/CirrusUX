<script lang="ts">
  // Deleted repositories wait here: restore one, or delete it for good
  // (the only real deletion, with the name typed).
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { listTrash } from './api';
  import { confirmGit } from './actions';
  import { gitData, reload } from './state.svelte';
  import { fmtBytes } from '../format';
  import { fmtDayTime } from '../time.svelte';
  import { RotateCcw, Trash2 } from '../icons';

  const trash = $derived.by(() => {
    void gitData.v;
    return listTrash();
  });
</script>

{#await trash then list}
  {#if list.length}
    <Card title="Deleted repositories" subtitle="Kept until you delete them for good.">
      <ul class="gx-list">
        {#each list as t (t.id)}
          <li>
            <div class="gx-grow">
              <strong>{t.name}</strong>
              <span class="faint gx-small">Deleted {fmtDayTime(new Date(t.deletedAt))} · {fmtBytes(t.size)}</span>
            </div>
            <Button size="sm" onclick={() => confirmGit({ action: 'git_restore', args: { trash: t.id } }, reload)}>{#snippet icon()}<RotateCcw />{/snippet}Restore…</Button>
            <Button size="sm" variant="ghost" onclick={() => confirmGit({ action: 'git_purge', args: { trash: t.id } }, reload)}>{#snippet icon()}<Trash2 />{/snippet}Delete for good…</Button>
          </li>
        {/each}
      </ul>
    </Card>
  {/if}
{/await}
