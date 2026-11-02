// Run with `npm test` (Node's own test runner; it strips the types).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { contrast, ensureContrast, onColor } from '../src/lib/theme/contrast.ts';

test('contrast ratios match WCAG', () => {
  assert.equal(Math.round(contrast('#000000', '#ffffff') * 10) / 10, 21);
  assert.equal(contrast('#777777', '#777777'), 1);
  // Symmetric.
  assert.equal(contrast('#123456', '#fedcba'), contrast('#fedcba', '#123456'));
});

test('ensureContrast leaves readable colours alone and fixes others', () => {
  assert.deepEqual(ensureContrast('#000000', '#ffffff'), { color: '#000000', adjusted: false });
  for (const [fg, bg] of [['#777777', '#808080'], ['#7c9cff', '#ffffff'], ['#3b3b3b', '#11131a']]) {
    const r = ensureContrast(fg, bg, 4.5);
    assert.equal(r.adjusted, true, `${fg} on ${bg}`);
    assert.ok(contrast(r.color, bg) >= 4.5, `${r.color} on ${bg} is ${contrast(r.color, bg)}`);
  }
});

test('onColor picks readable text for a fill', () => {
  for (const fill of ['#000000', '#ffffff', '#7c9cff', '#3ecf8e', '#ff7a90']) {
    assert.ok(contrast(onColor(fill), fill) >= 4.5, fill);
  }
});
