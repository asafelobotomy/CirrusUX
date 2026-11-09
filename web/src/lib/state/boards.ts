// Saved boards brought up to date with renamed widgets.
import type { Board } from './layout.svelte';

/** Widget types renamed between versions: old → new. */
const RENAMED: Record<string, string> = { grok: 'assistant' };

/** Brings saved boards up to date with renamed widgets, in place. */
export function migrateBoards(list: Board[]): Board[] {
  for (const b of list) {
    for (const items of Object.values(b.layouts)) {
      for (const it of items) it.type = RENAMED[it.type] ?? it.type;
    }
  }
  return list;
}
