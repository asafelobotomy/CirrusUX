import { test } from 'node:test';
import assert from 'node:assert/strict';
import { appKey, needsMessage, sourceLabel } from '../src/lib/catalogue.ts';

test('apps are keyed by source and id, so a taken id still lists', () => {
  assert.equal(appKey({ id: 'jellyfin' }), 'built-in/jellyfin');
  assert.equal(appKey({ id: 'jellyfin', source: 'cirrusstore' }), 'cirrusstore/jellyfin');
  assert.notEqual(appKey({ id: 'navidrome', source: 'friends' }), appKey({ id: 'navidrome', source: 'cirrusstore' }));
});

test('sources are named for people', () => {
  assert.equal(sourceLabel(undefined), 'Built in');
  assert.equal(sourceLabel('built-in'), 'Built in');
  assert.equal(sourceLabel('cirrusstore'), 'CirrusStore');
  assert.equal(sourceLabel('friends'), 'friends');
});

test('an app that needs a newer CirrusUX says how to get it', () => {
  assert.equal(needsMessage('0.8.0'), 'This app needs CirrusUX 0.8.0 or newer: run sudo cirrusux-setup upgrade on the server.');
});
