import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pollCommand, type Command } from '../src/lib/state/poll.ts';
import { createSaveQueue } from '../src/lib/saveQueue.ts';
import { migrateBoards } from '../src/lib/state/boards.ts';
import { agoText, fmtDuration } from '../src/lib/timefmt.ts';
import { fmtBytes, fmtGb, fmtNum } from '../src/lib/format.ts';
import { agentRuns } from '../src/lib/state/agentActions.ts';
import { offlineFromResponse, parseBody, requestHeaders } from '../src/lib/apiCore.ts';

const cmd = (state: Command['state']): Command => ({ id: 'a1', state });
const noWait = { sleep: async () => {}, waitMs: 60_000 };

test('a finished action is returned as it answered', async () => {
  const answers = [cmd('pending'), cmd('sent'), cmd('done')];
  const r = await pollCommand(async () => answers.shift()!, noWait);
  assert.deepEqual(r, { kind: 'command', command: cmd('done') });
});

test('a few failed polls in a row are tolerated', async () => {
  const steps: (Command | Error)[] = [new Error('x'), new Error('x'), new Error('x'), cmd('done')];
  const r = await pollCommand(async () => {
    const s = steps.shift()!;
    if (s instanceof Error) throw s;
    return s;
  }, noWait);
  assert.equal(r.kind, 'command');
});

test('the error count resets after a good poll', async () => {
  const steps: (Command | Error)[] = [new Error('x'), new Error('x'), cmd('pending'), new Error('x'), new Error('x'), cmd('failed')];
  const r = await pollCommand(async () => {
    const s = steps.shift()!;
    if (s instanceof Error) throw s;
    return s;
  }, noWait);
  assert.equal(r.kind === 'command' && r.command.state, 'failed');
});

test('too many failed polls mean contact was lost, not that the action failed', async () => {
  let calls = 0;
  const r = await pollCommand(async () => {
    calls++;
    throw new Error('down');
  }, noWait);
  assert.deepEqual(r, { kind: 'lost' });
  assert.equal(calls, 4);
});

test('polling stops at the deadline with the last answer', async () => {
  let t = 0;
  const r = await pollCommand(async () => cmd('sent'), { waitMs: 3000, sleep: async () => { t += 1000; }, now: () => t });
  assert.deepEqual(r, { kind: 'command', command: cmd('sent') });
});

test('saves run one at a time and only the newest waits', async () => {
  const sent: string[] = [];
  const gates: (() => void)[] = [];
  const q = createSaveQueue((doc) => {
    sent.push(doc);
    return new Promise((res) => gates.push(() => res('next')));
  });
  const first = q.save('v1');
  void q.save('v2');
  void q.save('v3');
  assert.deepEqual(sent, ['v1']);
  assert.equal(q.busy(), true);
  gates[0]();
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(sent, ['v1', 'v3']);
  gates[1]();
  await first;
  assert.equal(q.busy(), false);
  assert.deepEqual(sent, ['v1', 'v3']);
});

test('a waiting save is dropped when the first one says to', async () => {
  const sent: string[] = [];
  const q = createSaveQueue(async (doc) => {
    sent.push(doc);
    if (doc === 'v1') await new Promise((r) => setImmediate(r));
    return 'drop';
  });
  const p = q.save('v1');
  void q.save('v2');
  await p;
  assert.deepEqual(sent, ['v1']);
  await q.save('v4');
  assert.deepEqual(sent, ['v1', 'v4']);
});

test('old widget names are renamed in place', () => {
  const item = (type: string) => ({ id: type, type, w: 1, h: 1 });
  const boards = [{ id: 'b', name: 'B', layouts: { desktop: [item('grok'), item('power')], tablet: [item('grok')], phone: [] } }];
  const out = migrateBoards(boards as never);
  assert.equal(out, boards as never);
  assert.deepEqual(out[0].layouts.desktop.map((i) => i.type), ['assistant', 'power']);
  assert.equal(out[0].layouts.tablet[0].type, 'assistant');
});

test('sizes read the way the panel shows them', () => {
  assert.equal(fmtBytes(554e9), '554 GB');
  assert.equal(fmtBytes(1.92e12), '1.9 TB');
  assert.equal(fmtBytes(1.92e12, true), '1.92 TB');
  assert.equal(fmtBytes(2e9, true), '2.0 GB');
  assert.equal(fmtBytes(25e6), '25 MB');
  assert.equal(fmtBytes(4000), '4 kB');
  assert.equal(fmtGb(1.5, true), '1.5 GB');
  assert.equal(fmtBytes(96, false, true), '96 B');
  assert.equal(fmtBytes(2048, false, true), '2.0 kB');
  assert.equal(fmtBytes(52_000, false, true), '52 kB');
  assert.equal(fmtBytes(1_048_577, false, true), '1.0 MB');
  assert.equal(fmtNum(12345), '12,345');
});

test('elapsed times read in plain words', () => {
  const at = new Date('2026-11-01T12:00:00Z');
  const ago = (ms: number) => agoText(at, new Date(at.getTime() + ms));
  assert.equal(ago(10_000), 'just now');
  assert.equal(ago(5 * 60_000), '5 min ago');
  assert.equal(ago(2 * 3600_000), '2 h ago');
  assert.equal(ago(2 * 3600_000 + 30 * 60_000), '2 h 30 min ago');
  assert.equal(ago(50 * 3600_000), '2 d ago');
  assert.equal(fmtDuration(-5), '0 min');
  assert.equal(fmtDuration(45 * 60_000), '45 min');
  assert.equal(fmtDuration(3600_000), '1 h');
  assert.equal(fmtDuration(90 * 60_000), '1 h 30 min');
});

test('only state-changing calls carry the CSRF header', () => {
  assert.equal(requestHeaders({}).has('X-Cirrus-Request'), false);
  assert.equal(requestHeaders({ method: 'GET' }).has('X-Cirrus-Request'), false);
  const h = requestHeaders({ method: 'POST', body: '{}' });
  assert.equal(h.get('X-Cirrus-Request'), '1');
  assert.equal(h.get('Content-Type'), 'application/json');
  assert.equal(requestHeaders({ method: 'PUT', body: 'x', headers: { 'Content-Type': 'image/png' } }).get('Content-Type'), 'image/png');
});

test('the offline marker is read from reads only', () => {
  const off = { headers: new Headers({ 'X-Cirrus-Offline': '1' }) };
  assert.equal(offlineFromResponse(undefined, off), true);
  assert.equal(offlineFromResponse('GET', { headers: new Headers() }), false);
  assert.equal(offlineFromResponse('POST', off), null);
});

test('an empty answer is undefined, JSON is parsed', () => {
  assert.equal(parseBody(''), undefined);
  assert.deepEqual(parseBody('{"id":"a"}'), { id: 'a' });
});

test('an action is offered only when the agent lists it', () => {
  assert.equal(agentRuns([], false, 'backup_run'), true, 'an older agent lists nothing: allow all');
  assert.equal(agentRuns(['allow_sleep'], false, 'backup_run'), false);
  assert.equal(agentRuns(['backup_run'], false, 'backup_run'), true);
  assert.equal(agentRuns([], true, 'backup_run'), false, 'a protocol mismatch turns actions off');
});
