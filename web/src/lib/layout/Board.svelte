<script lang="ts">
  import { flip } from 'svelte/animate';
  import { WIDGETS, usable } from '../widgets/registry';
  import { COLUMNS, moveWidget, removeWidget, type Item } from '../state/layout.svelte';
  import { power } from '../state/power.svelte';
  import { mode } from '../api.svelte';
  import { fmtTime } from '../time.svelte';
  import type { Device } from '../state/ui.svelte';
  import { GripVertical, X } from '../icons';

  let { items, device, editing }: { items: Item[]; device: Device; editing: boolean } = $props();

  const cols = $derived(COLUMNS[device]);
  let grid: HTMLElement;
  let dragId = $state<string | null>(null);
  let resizeId = $state<string | null>(null);

  // ---- reorder: drag the grip, the widget under the pointer gives way ----
  function startDrag(e: PointerEvent, id: string) {
    e.preventDefault();
    dragId = id;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }
  function onDragMove(e: PointerEvent) {
    if (!dragId) return;
    const el = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('[data-wid]');
    const over = el?.dataset.wid;
    if (over && over !== dragId) moveWidget(device, dragId, items.findIndex((x) => x.id === over));
  }
  const endDrag = () => (dragId = null);

  function gripKey(e: KeyboardEvent, id: string) {
    const i = items.findIndex((x) => x.id === id);
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') moveWidget(device, id, i - 1);
    else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') moveWidget(device, id, i + 1);
    else return;
    e.preventDefault();
    queueMicrotask(() => grid.querySelector<HTMLElement>(`[data-wid="${id}"] .grip`)?.focus());
  }

  // ---- resize: corner handle snaps to whole columns and rows ----
  let start = { x: 0, y: 0, w: 0, h: 0, colW: 0, rowH: 0, gap: 0 };
  function startResize(e: PointerEvent, item: Item) {
    e.preventDefault();
    const cs = getComputedStyle(grid);
    const gap = parseFloat(cs.columnGap);
    start = {
      x: e.clientX, y: e.clientY, w: item.w, h: item.h, gap,
      colW: (grid.clientWidth - gap * (cols - 1)) / cols,
      rowH: parseFloat(cs.gridAutoRows),
    };
    resizeId = item.id;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }
  function onResizeMove(e: PointerEvent, item: Item) {
    if (resizeId !== item.id) return;
    const [minW, minH] = WIDGETS[item.type].min;
    item.w = clamp(start.w + Math.round((e.clientX - start.x) / (start.colW + start.gap)), Math.min(minW, cols), cols);
    item.h = clamp(start.h + Math.round((e.clientY - start.y) / (start.rowH + start.gap)), minH, 8);
  }
  function resizeKey(e: KeyboardEvent, item: Item) {
    const [minW, minH] = WIDGETS[item.type].min;
    const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key];
    if (!d) return;
    e.preventDefault();
    item.w = clamp(item.w + d[0], Math.min(minW, cols), cols);
    item.h = clamp(item.h + d[1], minH, 8);
  }
  const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));
</script>

<div
  class="grid" class:editing bind:this={grid} role="presentation"
  style="--cols: {cols}"
  onpointermove={onDragMove} onpointerup={endDrag} onpointercancel={endDrag}
>
  {#each items.filter((i) => usable(i.type)) as item (item.id)}
    {@const meta = WIDGETS[item.type]}
      {@const W = meta.component}
      {@const Icon = meta.icon}
      <section
        class="cell" class:bare={meta.bare} class:dragging={dragId === item.id} class:resizing={resizeId === item.id}
        data-wid={item.id}
        style="grid-column: span {Math.min(item.w, cols)}; grid-row: span {item.h}"
        animate:flip={{ duration: dragId ? 180 : 0 }}
        aria-label={meta.title}
      >
        {#if !meta.bare}
          <header>
            <h3><Icon size={15} />{item.config?.title || item.config?.group || meta.title}</h3>
            {#if meta.live && (power.state !== 'awake' || mode.offline)}
              <span class="asof num">as of {fmtTime(power.lastSnapshot)}</span>
            {/if}
          </header>
        {/if}
        <div class="body" class:live={meta.live}>
          <W config={item.config} />
        </div>

        {#if editing}
          <div class="edit">
            <button
              class="grip" aria-label="Move {meta.title}. Use arrow keys to move earlier or later."
              onpointerdown={(e) => startDrag(e, item.id)} onkeydown={(e) => gripKey(e, item.id)}
            ><GripVertical size={16} /></button>
            <span class="size num">{Math.min(item.w, cols)}×{item.h}</span>
            <button class="rm" aria-label="Remove {meta.title}" onclick={() => removeWidget(device, item.id)}><X size={15} /></button>
          </div>
          <button
            class="resize" aria-label="Resize {meta.title}. Use arrow keys to change width and height."
            onpointerdown={(e) => startResize(e, item)}
            onpointermove={(e) => onResizeMove(e, item)}
            onpointerup={() => (resizeId = null)}
            onkeydown={(e) => resizeKey(e, item)}
          ></button>
        {/if}
      </section>
  {/each}
</div>

<style>
  .grid {
    display: grid; grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    grid-auto-rows: var(--row-h); grid-auto-flow: row dense; gap: var(--space-4);
  }
  .cell {
    position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0;
    container-type: inline-size;
    background: var(--card-bg, var(--surface)); border: 1px solid var(--border);
    border-radius: var(--radius-lg); backdrop-filter: var(--card-blur, none);
    padding: var(--space-4) var(--space-5) var(--space-4);
    transition: box-shadow var(--dur-2) var(--ease), transform var(--dur-2) var(--ease), border-color var(--dur-2) var(--ease);
  }
  .cell.bare { padding: 0; overflow: hidden; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: var(--space-3); }
  h3 {
    display: flex; align-items: center; gap: 7px; font-size: 0.8rem; font-weight: 600;
    text-transform: uppercase; letter-spacing: 0.07em; color: var(--text-muted);
  }
  h3 :global(svg) { color: var(--text-faint); }
  .asof {
    font-size: 0.72rem; padding: 2px 7px; border-radius: 999px;
    color: var(--sleep); background: var(--sleep-soft);
  }
  .body { flex: 1; min-height: 0; }

  .editing { user-select: none; -webkit-user-select: none; }
  .editing .cell { border-style: dashed; border-color: var(--accent-line); }
  .editing .cell .body:not(:has(textarea)) { pointer-events: none; user-select: none; }
  .cell.dragging { transform: scale(1.02); box-shadow: 0 24px 60px -20px rgb(0 0 0 / 0.6); z-index: 5; border-style: solid; }
  .cell.resizing { box-shadow: 0 0 0 2px var(--accent); }
  .edit {
    position: absolute; top: 8px; right: 8px; display: flex; align-items: center; gap: 4px; z-index: 3;
    padding: 3px; border-radius: 10px; background: var(--surface-3); border: 1px solid var(--border-strong);
    box-shadow: 0 6px 16px -8px rgb(0 0 0 / 0.5);
  }
  .edit button { border: 0; background: transparent; color: var(--text-muted); width: 28px; height: 28px; border-radius: 7px; display: grid; place-items: center; }
  .edit button:hover { background: var(--hover); color: var(--text); }
  .grip { cursor: grab; touch-action: none; }
  .dragging .grip { cursor: grabbing; }
  .rm:hover { color: var(--bad) !important; }
  .size { font-size: 0.72rem; color: var(--text-faint); padding: 0 2px; }
  .resize {
    position: absolute; right: 2px; bottom: 2px; width: 22px; height: 22px; z-index: 3;
    border: 0; background: transparent; cursor: nwse-resize; touch-action: none;
  }
  .resize::after {
    content: ''; position: absolute; right: 5px; bottom: 5px; width: 10px; height: 10px;
    border-right: 2px solid var(--accent); border-bottom: 2px solid var(--accent); border-bottom-right-radius: 4px;
  }
</style>
