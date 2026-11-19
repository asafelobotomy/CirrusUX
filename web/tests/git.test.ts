import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gitHref, parseGitRoute, parsePatch, pathTrail, validRepoName } from '../src/lib/git/diff.ts';
import { fmtBytes } from '../src/lib/format.ts';
import { fmtBuildTime, isOver, stateText, stateTone, took } from '../src/lib/git/builds.ts';

test('a patch splits into numbered lines', () => {
  const patch = [
    'diff --git a/a.txt b/a.txt',
    'index 1..2 100644',
    '--- a/a.txt',
    '+++ b/a.txt',
    '@@ -3,2 +3,3 @@ heading',
    ' same',
    '-old',
    '+new',
    '+added',
    '@@ -10 +11 @@',
    ' tail',
    '\\ No newline at end of file',
  ].join('\n');
  const lines = parsePatch(patch);
  assert.deepEqual(lines, [
    { kind: 'hunk', text: '@@ -3,2 +3,3 @@ heading' },
    { kind: 'ctx', text: 'same', old: 3, new: 3 },
    { kind: 'del', text: 'old', old: 4 },
    { kind: 'add', text: 'new', new: 4 },
    { kind: 'add', text: 'added', new: 5 },
    { kind: 'hunk', text: '@@ -10 +11 @@' },
    { kind: 'ctx', text: 'tail', old: 10, new: 11 },
    { kind: 'ctx', text: '\\ No newline at end of file' },
  ]);
});

test('a binary or empty patch has no lines', () => {
  assert.deepEqual(parsePatch('diff --git a/p.png b/p.png\nBinary files /dev/null and b/p.png differ\n'), []);
  assert.deepEqual(parsePatch(''), []);
});

test('a path becomes a trail of folders', () => {
  assert.deepEqual(pathTrail(''), []);
  assert.deepEqual(pathTrail('a/b'), [
    { name: 'a', path: 'a' },
    { name: 'b', path: 'a/b' },
  ]);
});

test('git routes round-trip', () => {
  const cases: [string, ReturnType<typeof parseGitRoute>][] = [
    ['', { tab: 'files' }],
    ['notes', { repo: 'notes', tab: 'files' }],
    ['notes/files/main/src/a%20b.go', { repo: 'notes', tab: 'files', ref: 'main', path: 'src/a b.go' }],
    ['notes/files/feature%2Fx', { repo: 'notes', tab: 'files', ref: 'feature/x' }],
    ['notes/commits/dev', { repo: 'notes', tab: 'commits', ref: 'dev' }],
    ['notes/commit/abc123', { repo: 'notes', tab: 'commit', sha: 'abc123' }],
    ['notes/branches', { repo: 'notes', tab: 'branches' }],
    ['notes/tags', { repo: 'notes', tab: 'tags' }],
    ['notes/settings', { repo: 'notes', tab: 'settings' }],
  ];
  for (const [sub, route] of cases) {
    assert.deepEqual(parseGitRoute(sub), route, sub);
    assert.equal(gitHref(route), '#/git' + (sub ? '/' + sub : ''), sub);
  }
  assert.deepEqual(parseGitRoute('notes/nonsense'), { repo: 'notes', tab: 'files' });
});

test('repository names follow the gateway rules', () => {
  for (const ok of ['notes', 'my-app', 'a.b_c', 'x', 'a'.repeat(64)]) assert.ok(validRepoName(ok), ok);
  for (const bad of ['', '-x', '.x', 'X', 'a/b', 'x.git', '.trash', 'a'.repeat(65), 'a b']) assert.ok(!validRepoName(bad), bad);
});

test('file sizes keep small files readable', () => {
  assert.equal(fmtBytes(96, false, true), '96 B');
  assert.equal(fmtBytes(2048, false, true), '2.0 kB');
  assert.equal(fmtBytes(52_000, false, true), '52 kB');
  assert.equal(fmtBytes(1_048_577, false, true), '1.0 MB');
});

test('build routes go to and from addresses', () => {
  assert.deepEqual(parseGitRoute('notes/builds'), { repo: 'notes', tab: 'builds' });
  assert.deepEqual(parseGitRoute('notes/build/0123456789abcdef'), { repo: 'notes', tab: 'build', build: '0123456789abcdef' });
  assert.deepEqual(parseGitRoute('notes/build'), { repo: 'notes', tab: 'builds' });
  assert.equal(gitHref({ repo: 'notes', tab: 'build', build: 'ab12' }), '#/git/notes/build/ab12');
  assert.equal(gitHref({ repo: 'notes', tab: 'builds' }), '#/git/notes/builds');
});

test('build times and states read plainly', () => {
  assert.equal(fmtBuildTime(45_000), '45 s');
  assert.equal(fmtBuildTime(192_000), '3 min 12 s');
  assert.equal(fmtBuildTime(3_840_000), '1 h 4 min');
  const t0 = '2026-11-18T10:00:00Z';
  assert.equal(took(t0, '2026-11-18T10:02:00Z', new Date()), '2 min');
  assert.equal(took(t0, undefined, new Date('2026-11-18T10:00:30Z')), '30 s');
  assert.equal(took(undefined, undefined, new Date()), '');
  assert.equal(stateText('queued'), 'Waiting to start');
  assert.equal(stateTone('failed'), 'bad');
  assert.ok(isOver('skipped') && !isOver('running'));
});
