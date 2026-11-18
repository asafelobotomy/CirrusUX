import test from 'node:test';
import assert from 'node:assert/strict';
import { isNewer, machineState, parseVersion } from '../src/lib/release.ts';

test('versions are read with or without a v, a commit or a suffix', () => {
  assert.deepEqual(parseVersion('v0.9.0'), [0, 9, 0]);
  assert.deepEqual(parseVersion('0.8.1 (7daf45a)'), [0, 8, 1]);
  assert.deepEqual(parseVersion('1.2.3-rc1'), [1, 2, 3]);
  for (const s of ['', 'dev', 'v1.2', 'latest', '1.2.3.4', 'v1.x.3']) assert.equal(parseVersion(s), null, s);
});

test('a release is newer by number, not by text', () => {
  assert.equal(isNewer('v0.9.0', 'v0.8.1'), true);
  assert.equal(isNewer('v0.10.0', 'v0.9.9'), true);
  assert.equal(isNewer('v0.8.1', '0.8.1 (7daf45a)'), false);
  assert.equal(isNewer('v0.8.0', 'v0.8.1'), false);
  assert.equal(isNewer('v0.9.0', 'dev'), false, 'a development build is never offered a release');
  assert.equal(isNewer('', 'v0.8.1'), false);
});

test('what a machine is offered follows its install and the last check', () => {
  assert.equal(machineState(null, 'v0.9.0'), 'no-report');
  assert.equal(machineState({ managed: false }, 'v0.9.0'), 'by-hand');
  assert.equal(machineState({ managed: true, current: 'v0.8.1' }, 'v0.9.0'), 'offer');
  assert.equal(machineState({ managed: true, current: 'v0.9.0', previous: 'v0.8.1' }, 'v0.9.0'), 'current');
  assert.equal(machineState({ managed: true, current: 'v0.8.1' }, ''), 'current', 'nothing checked yet: nothing offered');
});
