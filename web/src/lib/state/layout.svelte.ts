// Boards and per-device widget layouts (PLAN §4.3). Order-based grid:
// widgets flow in order and the grid packs them densely.
import { load, save } from '../demo/store';
import type { Device } from './ui.svelte';
import { migrateBoards } from './boards';

export interface Item {
  id: string;
  type: string;
  w: number;
  h: number;
  /** Per-widget options, e.g. the note text or the app group. */
  config?: Record<string, string>;
}

export interface Board {
  id: string;
  name: string;
  layouts: Record<Device, Item[]>;
}

export const COLUMNS: Record<Device, number> = { phone: 4, tablet: 8, desktop: 12 };

let seq = 0;
const uid = () => `w${Date.now().toString(36)}${(seq++).toString(36)}`;

type Spec = [type: string, desktop: [number, number], tablet: [number, number], phone: [number, number], config?: Record<string, string>];

function board(id: string, name: string, specs: Spec[]): Board {
  const make = (pick: (s: Spec) => [number, number]) =>
    specs.map((s, i) => ({ id: `${id}-${i}`, type: s[0], w: pick(s)[0], h: pick(s)[1], config: s[4] }));
  return { id, name, layouts: { desktop: make((s) => s[1]), tablet: make((s) => s[2]), phone: make((s) => s[3]) } };
}

export function defaultBoards(): Board[] {
  return [
    board('home', 'Home', [
      ['power', [5, 4], [4, 4], [4, 4]],
      ['apps', [7, 2], [4, 4], [4, 3]],
      ['timeline', [7, 2], [8, 2], [4, 2]],
      ['backups', [4, 2], [4, 2], [4, 2]],
      ['security', [3, 2], [4, 2], [4, 2]],
      ['containers', [5, 4], [8, 4], [4, 5]],
      ['storage', [4, 2], [4, 3], [4, 3]],
      ['temps', [3, 2], [4, 3], [4, 2]],
      ['assistant', [5, 2], [4, 2], [4, 2]],
      ['changelog', [7, 2], [8, 3], [4, 3]],
    ]),
    board('media', 'Media', [
      ['apps', [8, 2], [8, 2], [4, 3], { group: 'Watch & listen' }],
      ['storage', [4, 4], [8, 3], [4, 3]],
      ['apps', [8, 2], [8, 2], [4, 2], { group: 'Manage' }],
      ['note', [6, 2], [8, 2], [4, 2], { text: 'A note for this board. Switch on Edit layout to write here.' }],
    ]),
    board('maintenance', 'Maintenance', [
      ['digest', [8, 3], [8, 3], [4, 4]],
      ['power', [4, 4], [4, 4], [4, 4]],
      ['backups', [4, 2], [4, 2], [4, 2]],
      ['security', [4, 2], [4, 2], [4, 2]],
      ['updates', [4, 2], [4, 2], [4, 2]],
      ['temps', [4, 2], [4, 2], [4, 2]],
      ['changelog', [12, 3], [8, 3], [4, 3]],
    ]),
  ];
}

export { migrateBoards };

const saved = load('boards', { list: defaultBoards(), active: 'home' });
export const boards = $state<{ list: Board[]; active: string }>({ ...saved, list: migrateBoards(saved.list) });

export function activeBoard(): Board {
  return boards.list.find((b) => b.id === boards.active) ?? boards.list[0];
}

export function addWidget(device: Device, type: string, w: number, h: number, config?: Record<string, string>): void {
  activeBoard().layouts[device].push({ id: uid(), type, w: Math.min(w, COLUMNS[device]), h, config });
}

export function removeWidget(device: Device, id: string): void {
  const list = activeBoard().layouts[device];
  const i = list.findIndex((x) => x.id === id);
  if (i >= 0) list.splice(i, 1);
}

export function moveWidget(device: Device, id: string, toIndex: number): void {
  const list = activeBoard().layouts[device];
  const from = list.findIndex((x) => x.id === id);
  if (from < 0 || toIndex < 0 || toIndex >= list.length || from === toIndex) return;
  const [item] = list.splice(from, 1);
  list.splice(toIndex, 0, item);
}

export function addBoard(name: string): void {
  const b: Board = { id: uid(), name, layouts: { desktop: [], tablet: [], phone: [] } };
  boards.list.push(b);
  boards.active = b.id;
}

export function resetBoards(): void {
  boards.list = defaultBoards();
  boards.active = 'home';
}

$effect.root(() => {
  $effect(() => save('boards', $state.snapshot(boards)));
});
