// Sample builds for demo mode, shaped like the gateway's answers.
import { ApiError } from '../api.svelte';
import { now } from '../time.svelte';
import type { BuildLog, BuildRun, BuildSettings } from '../git/builds';

const min = 60_000;
const at = (minsAgo: number) => new Date(now().getTime() - minsAgo * min).toISOString();
const sha = (n: number) => n.toString(16).padStart(8, '0').repeat(5);

function runs(): BuildRun[] {
  return [
    {
      id: 'b000000000000002', repo: 'website', ref: 'redesign', commit: sha(12), subject: 'New colours for the header',
      who: 'you', created: at(42), state: 'failed', started: at(42), finished: at(39), logSize: 0,
      error: 'Step test failed (exit code 1)',
      steps: [
        { name: 'install', image: 'node:24', state: 'passed', started: at(42), finished: at(41), exit: 0 },
        { name: 'test', image: 'node:24', state: 'failed', started: at(41), finished: at(39), exit: 1 },
        { name: 'build', image: 'node:24', state: 'skipped' },
      ],
    },
    {
      id: 'b000000000000001', repo: 'website', ref: 'main', commit: sha(11), subject: 'Update the photo gallery',
      who: 'you', created: at(60 * 26), state: 'passed', started: at(60 * 26), finished: at(60 * 26 - 4), logSize: 0,
      steps: [
        { name: 'install', image: 'node:24', state: 'passed', started: at(60 * 26), finished: at(60 * 26 - 1), exit: 0 },
        { name: 'test', image: 'node:24', state: 'passed', started: at(60 * 26 - 1), finished: at(60 * 26 - 3), exit: 0 },
        { name: 'build', image: 'node:24', state: 'passed', started: at(60 * 26 - 3), finished: at(60 * 26 - 4), exit: 0 },
      ],
    },
  ];
}

const LOGS: Record<string, string> = {
  b000000000000002: `Building website at ${sha(12).slice(0, 12)} (redesign)

── Step 1 of 3: install (node:24) ──
$ npm ci
added 214 packages in 38s

── Step 2 of 3: test (node:24) ──
$ npm test
✔ gallery shows every photo
✖ header colours meet the contrast check
  expected 3.9 to be at least 4.5
1 of 2 tests failed

Step test failed (exit code 1)
`,
  b000000000000001: `Building website at ${sha(11).slice(0, 12)} (main)

── Step 1 of 3: install (node:24) ──
$ npm ci
added 214 packages in 41s

── Step 2 of 3: test (node:24) ──
$ npm test
✔ gallery shows every photo
✔ header colours meet the contrast check

── Step 3 of 3: build (node:24) ──
$ npm run build
built in 52s
`,
};

export const listBuilds = async (repo: string): Promise<BuildRun[]> => runs().filter((r) => r.repo === repo).map(withSize);

export async function getBuild(id: string): Promise<BuildRun> {
  const r = runs().find((x) => x.id === id);
  if (!r) throw new ApiError(404, 'no such build');
  return withSize(r);
}

export async function getBuildLog(id: string, from: number): Promise<BuildLog> {
  const text = LOGS[id] ?? '';
  return { text: text.slice(from), next: text.length };
}

const withSize = (r: BuildRun): BuildRun => ({ ...r, logSize: (LOGS[r.id] ?? '').length });

export const getBuildSettings = async (_repo: string): Promise<BuildSettings> => ({ onPush: false, wake: false });
