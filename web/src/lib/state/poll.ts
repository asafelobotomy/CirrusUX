// Waiting for a queued action to finish. A few failed polls in a row are not
// a failed action: the gateway or the connection can blink while the server
// carries on, so only a longer silence counts as lost contact.

export interface Command { id: string; state: 'pending' | 'sent' | 'done' | 'failed'; error?: string; output?: string[] }

export type PollResult =
  | { kind: 'command'; command: Command }
  | { kind: 'lost' };

export interface PollOptions {
  waitMs: number;
  /** Consecutive failed polls allowed before giving up. */
  maxErrors?: number;
  intervalMs?: number;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
}

export async function pollCommand(get: () => Promise<Command>, o: PollOptions): Promise<PollResult> {
  const { maxErrors = 3, intervalMs = 1000, sleep = (ms) => new Promise<void>((r) => setTimeout(r, ms)), now = Date.now } = o;
  const deadline = now() + o.waitMs;
  let errors = 0;
  let last: Command | null = null;
  do {
    await sleep(intervalMs);
    try {
      last = await get();
      errors = 0;
    } catch {
      if (++errors > maxErrors) return { kind: 'lost' };
    }
  } while ((!last || last.state === 'pending' || last.state === 'sent') && now() < deadline);
  // Out of time with nothing but errors: the same as lost contact.
  if (!last) return { kind: 'lost' };
  return { kind: 'command', command: last };
}
