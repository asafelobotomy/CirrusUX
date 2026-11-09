// One save in flight at a time. While one is running, only the newest
// document waits behind it, so overlapping saves never use the same version
// number (a 409 against ourselves) and the latest edit is never lost.

/** 'next' lets the waiting document go; 'drop' discards it (it is out of date, or retrying will pick up the latest). */
export type SaveOutcome = 'next' | 'drop';

export function createSaveQueue(send: (doc: string) => Promise<SaveOutcome>) {
  let running = false;
  let waiting: string | null = null;

  async function save(doc: string): Promise<void> {
    if (running) {
      waiting = doc;
      return;
    }
    running = true;
    try {
      let outcome: SaveOutcome = await send(doc);
      while (waiting !== null) {
        const next: string = waiting;
        waiting = null;
        if (outcome === 'drop') break;
        outcome = await send(next);
      }
    } finally {
      waiting = null;
      running = false;
    }
  }

  return { save, busy: () => running };
}
