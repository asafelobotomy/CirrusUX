// Pure helpers for the Git page: diff lines, path trails, page addresses
// and repository names. No imports, so the unit tests run them directly.

export interface DiffLine {
  kind: 'hunk' | 'add' | 'del' | 'ctx';
  text: string;
  old?: number;
  new?: number;
}

const HUNK = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** One file's patch as lines with old and new line numbers. Header lines
 *  (diff --git, index, ---, +++) are dropped. */
export function parsePatch(patch: string): DiffLine[] {
  const out: DiffLine[] = [];
  let oldN = 0;
  let newN = 0;
  let inHunk = false;
  const lines = patch.split('\n');
  if (lines.at(-1) === '') lines.pop();
  for (const line of lines) {
    const m = HUNK.exec(line);
    if (m) {
      inHunk = true;
      oldN = Number(m[1]);
      newN = Number(m[2]);
      out.push({ kind: 'hunk', text: line });
      continue;
    }
    if (line.startsWith('diff --git ')) inHunk = false;
    if (!inHunk) continue;
    if (line.startsWith('+')) out.push({ kind: 'add', text: line.slice(1), new: newN++ });
    else if (line.startsWith('-')) out.push({ kind: 'del', text: line.slice(1), old: oldN++ });
    else if (line.startsWith('\\')) out.push({ kind: 'ctx', text: line });
    else out.push({ kind: 'ctx', text: line.slice(1), old: oldN++, new: newN++ });
  }
  return out;
}

/** "a/b" → the folders on the way: a, then a/b. */
export function pathTrail(path: string): { name: string; path: string }[] {
  if (!path) return [];
  const parts = path.split('/');
  return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
}

export type GitTab = 'files' | 'commits' | 'commit' | 'branches' | 'tags' | 'settings';

export interface GitRoute {
  repo?: string;
  tab: GitTab;
  ref?: string;
  path?: string;
  sha?: string;
}

const dec = (s: string) => {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
};

/** Reads the part of the address after #/git/. */
export function parseGitRoute(sub: string): GitRoute {
  const parts = sub.split('/').filter((p, i) => p !== '' || i === 0);
  const repo = dec(parts[0] ?? '');
  if (!repo) return { tab: 'files' };
  const tab = parts[1];
  const r: GitRoute = { repo, tab: 'files' };
  switch (tab) {
    case 'files': {
      if (parts[2]) r.ref = dec(parts[2]);
      const path = parts.slice(3).map(dec).join('/');
      if (path) r.path = path;
      return r;
    }
    case 'commits':
      r.tab = 'commits';
      if (parts[2]) r.ref = dec(parts[2]);
      return r;
    case 'commit':
      if (!parts[2]) return r;
      return { repo, tab: 'commit', sha: dec(parts[2]) };
    case 'branches':
    case 'tags':
    case 'settings':
      return { repo, tab };
  }
  return r;
}

/** The page address for a Git view; the inverse of parseGitRoute. */
export function gitHref(r: GitRoute): string {
  if (!r.repo) return '#/git';
  const segs = [encodeURIComponent(r.repo)];
  const enc = encodeURIComponent;
  switch (r.tab) {
    case 'files':
      if (r.ref) segs.push('files', enc(r.ref), ...(r.path ? r.path.split('/').map(enc) : []));
      break;
    case 'commits':
      segs.push('commits', ...(r.ref ? [enc(r.ref)] : []));
      break;
    case 'commit':
      segs.push('commit', enc(r.sha ?? ''));
      break;
    default:
      segs.push(r.tab);
  }
  return '#/git/' + segs.join('/');
}

/** The gateway's rule for repository names (githost.ValidName). */
export function validRepoName(name: string): boolean {
  return /^[a-z0-9][a-z0-9._-]{0,63}$/.test(name) && !name.endsWith('.git');
}

/** A file's size: bytes under 1 kB, then as the rest of the panel shows sizes. */
export function fileSize(bytes: number): string {
  if (bytes < 1000) return `${bytes} B`;
  if (bytes < 1e6) return `${(bytes / 1e3).toFixed(bytes < 1e4 ? 1 : 0)} kB`;
  return `${(bytes / 1e6).toFixed(1)} MB`;
}
