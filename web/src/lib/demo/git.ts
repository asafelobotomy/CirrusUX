// Sample repositories for demo mode, shaped like the gateway's answers.
import type { Blob, Commit, CommitDetail, GitToken, Repo, RepoSummary, Trashed, Tree } from '../git/api';

const day = 86_400_000;
const ago = (days: number) => new Date(Date.now() - days * day).toISOString();
const sha = (n: number) => n.toString(16).padStart(8, '0').repeat(5);
const base = 'https://homeserver.example.ts.net/git/';

const notesCommits: Commit[] = [
  { sha: sha(3), parents: [sha(2)], author: 'Sam', email: 'sam@example.com', time: ago(1), subject: 'Add the packing list' },
  { sha: sha(2), parents: [sha(1)], author: 'Sam', email: 'sam@example.com', time: ago(6), subject: 'Recipes: fix the bread timings' },
  { sha: sha(1), parents: [], author: 'Sam', email: 'sam@example.com', time: ago(30), subject: 'First notes' },
];

const repos: Repo[] = [
  {
    name: 'empty-project', description: 'Nothing pushed yet', defaultBranch: 'main', size: 24_576, lastCommit: null, empty: true,
    branches: [], tags: [], cloneUrl: base + 'empty-project.git',
  },
  {
    name: 'notes', description: 'Notes, lists and recipes', defaultBranch: 'main', size: 184_320, lastCommit: ago(1), empty: false,
    branches: [{ name: 'main', commit: sha(3), subject: 'Add the packing list', time: ago(1) }],
    tags: [], cloneUrl: base + 'notes.git',
  },
  {
    name: 'website', description: 'The family website', defaultBranch: 'main', size: 2_621_440, lastCommit: ago(3), empty: false,
    branches: [
      { name: 'redesign', commit: sha(12), subject: 'New colours for the header', time: ago(3) },
      { name: 'main', commit: sha(11), subject: 'Update the photo gallery', time: ago(9) },
    ],
    tags: [{ name: 'v1.0', commit: sha(10), time: ago(40) }],
    cloneUrl: base + 'website.git',
  },
];

const trees: Record<string, Tree['entries']> = {
  '': [
    { name: 'recipes', type: 'folder', size: 0, mode: '040000' },
    { name: 'README.md', type: 'file', size: 96, mode: '100644' },
    { name: 'packing-list.md', type: 'file', size: 120, mode: '100644' },
  ],
  recipes: [{ name: 'bread.md', type: 'file', size: 210, mode: '100644' }],
};

const files: Record<string, string> = {
  'README.md': '# Notes\n\nLists, recipes and anything worth keeping.\nPushed to the home server, not to GitHub.\n',
  'packing-list.md': '# Packing list\n\n- Passports\n- Chargers\n- Rain jackets\n- Snacks for the drive\n',
  'recipes/bread.md': '# Bread\n\n1. Mix 500 g flour, 350 g water, 10 g salt, 7 g yeast.\n2. Rise for 2 hours.\n3. Bake at 230 °C for 35 minutes.\n',
};

const tokens: GitToken[] = [
  { id: 'a1b2c3d4e5f60718', name: 'Laptop', owner: 'you', scope: 'read-write', created: ago(20), lastUsed: ago(1) },
];

const find = (name: string): Repo => {
  const r = repos.find((x) => x.name === name);
  if (!r) throw new Error(`There's no repository called ${name}`);
  return r;
};

export const listRepos = async (): Promise<RepoSummary[]> => repos.map(({ branches: _b, tags: _t, cloneUrl: _c, ...s }) => s);
export const getRepo = async (name: string): Promise<Repo> => find(name);

export async function getTree(name: string, path: string): Promise<Tree> {
  const r = find(name);
  if (r.empty) return { commit: '', empty: true, entries: [] };
  if (name !== 'notes') return { commit: r.branches[0].commit, empty: false, entries: [{ name: 'index.html', type: 'file', size: 2048, mode: '100644' }] };
  return { commit: sha(3), empty: false, entries: trees[path] ?? [] };
}

export async function getBlob(name: string, path: string): Promise<Blob> {
  const text = name === 'notes' ? (files[path] ?? '') : '<!doctype html>\n<title>Our website</title>\n';
  return { path, size: text.length, binary: false, tooLarge: false, text };
}

export async function getCommits(name: string, page: number): Promise<Commit[]> {
  const r = find(name);
  if (page > 1 || r.empty) return [];
  if (name === 'notes') return notesCommits;
  return r.branches.map((b) => ({ sha: b.commit, parents: [], author: 'Alex', email: 'alex@example.com', time: b.time, subject: b.subject }));
}

export async function getCommit(name: string, id: string): Promise<CommitDetail> {
  const c = (await getCommits(name, 1)).find((x) => x.sha === id) ?? notesCommits[0];
  return {
    ...c,
    message: c.subject + '\n\nFor the trip in the spring.',
    truncated: false,
    files: [
      {
        path: 'packing-list.md', change: 'added', added: 6, removed: 0, binary: false,
        patch: 'diff --git a/packing-list.md b/packing-list.md\nnew file mode 100644\n--- /dev/null\n+++ b/packing-list.md\n@@ -0,0 +1,6 @@\n+# Packing list\n+\n+- Passports\n+- Chargers\n+- Rain jackets\n+- Snacks for the drive\n',
      },
      {
        path: 'README.md', change: 'modified', added: 1, removed: 1, binary: false,
        patch: 'diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1,3 +1,3 @@\n # Notes\n \n-Lists and recipes.\n+Lists, recipes and anything worth keeping.\n',
      },
    ],
  };
}

export const listTrash = async (): Promise<Trashed[]> => [{ id: 'old-scripts-20261020-091500', name: 'old-scripts', deletedAt: ago(18), size: 51_200 }];
export const listTokens = async (): Promise<GitToken[]> => tokens;

export async function createToken(name: string, scope: GitToken['scope']): Promise<{ token: GitToken; secret: string }> {
  const token: GitToken = { id: Math.random().toString(16).slice(2, 18), name, owner: 'you', scope, created: new Date().toISOString() };
  tokens.push(token);
  return { token, secret: 'cxg_demo' + 'x'.repeat(48) };
}

export async function revokeToken(id: string): Promise<void> {
  const i = tokens.findIndex((t) => t.id === id);
  if (i >= 0) tokens.splice(i, 1);
}
