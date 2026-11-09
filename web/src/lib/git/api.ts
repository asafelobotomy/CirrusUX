// The Git page's view of the gateway (/api/git/...). In demo mode every
// read answers from sample data.
import { api, mode } from '../api.svelte';
import * as demo from '../demo/git';

export interface RepoSummary {
  name: string;
  description: string;
  defaultBranch: string;
  size: number;
  lastCommit: string | null;
  empty: boolean;
}
export interface Branch { name: string; commit: string; subject: string; time: string }
export interface Tag { name: string; commit: string; time: string }
export interface Repo extends RepoSummary { branches: Branch[]; tags: Tag[]; cloneUrl: string }
export interface TreeEntry { name: string; type: 'file' | 'folder' | 'link' | 'submodule'; size: number; mode: string }
export interface Tree { commit: string; empty: boolean; entries: TreeEntry[] }
export interface Blob { path: string; size: number; binary: boolean; tooLarge: boolean; text: string }
export interface Commit { sha: string; parents: string[]; author: string; email: string; time: string; subject: string }
export interface FileDiff {
  path: string;
  oldPath?: string;
  change: 'added' | 'deleted' | 'modified' | 'renamed';
  added: number;
  removed: number;
  binary: boolean;
  patch: string;
}
export interface CommitDetail extends Commit { message: string; files: FileDiff[]; truncated: boolean }
export interface Trashed { id: string; name: string; deletedAt: string; size: number }
export interface GitToken { id: string; name: string; owner: string; scope: 'read' | 'read-write'; created: string; lastUsed?: string }

const repoPath = (name: string) => `/api/git/repos/${encodeURIComponent(name)}`;
const qs = (o: Record<string, string | number | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== '') p.set(k, String(v));
  const s = p.toString();
  return s ? '?' + s : '';
};

export const listRepos = (): Promise<RepoSummary[]> => (mode.live ? api('/api/git/repos') : demo.listRepos());
export const getRepo = (name: string): Promise<Repo> => (mode.live ? api(repoPath(name)) : demo.getRepo(name));
export const getTree = (name: string, ref = '', path = ''): Promise<Tree> =>
  mode.live ? api(repoPath(name) + '/tree' + qs({ ref, path })) : demo.getTree(name, path);
export const getBlob = (name: string, ref = '', path = ''): Promise<Blob> =>
  mode.live ? api(repoPath(name) + '/blob' + qs({ ref, path })) : demo.getBlob(name, path);
export const getCommits = (name: string, ref = '', path = '', page = 1): Promise<Commit[]> =>
  mode.live ? api(repoPath(name) + '/commits' + qs({ ref, path, page })) : demo.getCommits(name, page);
export const getCommit = (name: string, sha: string): Promise<CommitDetail> =>
  mode.live ? api(repoPath(name) + '/commit/' + encodeURIComponent(sha)) : demo.getCommit(name, sha);
export const listTrash = (): Promise<Trashed[]> => (mode.live ? api('/api/git/trash') : demo.listTrash());
export const listTokens = (): Promise<GitToken[]> => (mode.live ? api('/api/git/tokens') : demo.listTokens());

export function createToken(name: string, scope: GitToken['scope']): Promise<{ token: GitToken; secret: string }> {
  if (!mode.live) return demo.createToken(name, scope);
  return api('/api/git/tokens', { method: 'POST', body: JSON.stringify({ name, scope }) });
}

export function revokeToken(id: string): Promise<void> {
  if (!mode.live) return demo.revokeToken(id);
  return api(`/api/git/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

/** Links the browser downloads; demo mode has none. */
export const rawUrl = (name: string, ref: string, path: string) => repoPath(name) + '/blob' + qs({ ref, path, raw: 1 });
export const archiveUrl = (name: string, ref: string, format: 'tar.gz' | 'zip') => repoPath(name) + '/archive' + qs({ ref, format });
