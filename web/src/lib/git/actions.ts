// Confirmations for Git hosting's changes. They run on the gateway, so
// they work while the server sleeps, and each says how to undo it.
import { ask, toast, type Confirm } from '../state/ui.svelte';
import { sendAction, type ActionReq } from '../state/actions';
import { mode } from '../api.svelte';

type Args = Record<string, string>;
type Copy = Pick<Confirm, 'title' | 'body' | 'confirmLabel' | 'changelog'> & { undo?: string; danger?: boolean; typed?: string };

const short = (ref: string) => ref.replace(/^refs\/(heads|tags)\//, '');
const trashName = (id: string) => id.replace(/-\d{8}-\d{6}(-\d+)?$/, '');

const COPY: Record<string, (a: Args) => Copy> = {
  git_create: (a) => ({
    title: `Create repository ${a.name}?`, body: 'An empty repository you can push to.', confirmLabel: 'Create',
    changelog: `Created repository ${a.name}`, undo: 'Delete it (it goes to Deleted repositories)',
  }),
  git_import: (a) => ({
    title: `Import ${a.name}?`,
    body: `The gateway fetches ${a.url.replace(/\/\/[^/@]+@/, '//')} once, now, and keeps a copy here. Nothing syncs afterwards.`,
    confirmLabel: 'Import', changelog: `Imported ${a.name}`, undo: 'Delete it (it goes to Deleted repositories)',
  }),
  git_rename: (a) => ({
    title: `Rename ${a.name} to ${a.to}?`, body: `Clones that use the old address need \`git remote set-url\` to the new one.`,
    confirmLabel: 'Rename', changelog: `Renamed repository ${a.name} to ${a.to}`, undo: `Rename it back to ${a.name}`,
  }),
  git_delete: (a) => ({
    title: `Delete repository ${a.name}?`, body: 'It moves to Deleted repositories, where you can restore it or delete it for good.',
    confirmLabel: 'Delete', danger: true, changelog: `Deleted repository ${a.name}`, undo: 'Restore it',
  }),
  git_restore: (a) => ({
    title: `Restore ${trashName(a.trash)}?`, body: 'It comes back under its old name.', confirmLabel: 'Restore',
    changelog: `Restored repository ${trashName(a.trash)}`, undo: 'Delete it again',
  }),
  git_purge: (a) => ({
    title: `Delete ${trashName(a.trash)} for good?`, body: 'Its files and history are removed from the gateway. This can\'t be undone.',
    confirmLabel: 'Delete for good', danger: true, typed: trashName(a.trash), changelog: `Deleted ${trashName(a.trash)} for good`,
  }),
  git_describe: (a) => ({
    title: `Change the description of ${a.name}?`, body: a.description ? `New description: ${a.description}` : 'The description is cleared.',
    confirmLabel: 'Save', changelog: `Changed the description of ${a.name}`, undo: 'Put the old description back',
  }),
  git_default_branch: (a) => ({
    title: `Make ${a.branch} the default branch of ${a.name}?`, body: 'New clones check it out, and the panel shows it first.',
    confirmLabel: 'Make default', changelog: `Made ${a.branch} the default branch of ${a.name}`, undo: 'Make the old one the default again',
  }),
  git_branch_delete: (a) => ({
    title: `Delete branch ${a.branch}?`, body: `Its commits stay in ${a.name} for at least 30 days, so this can be undone from the Changelog.`,
    confirmLabel: 'Delete branch', danger: true, changelog: `Deleted branch ${a.branch} from ${a.name}`, undo: 'Put the branch back',
  }),
  git_tag_delete: (a) => ({
    title: `Delete tag ${a.tag}?`, body: `Its commit stays in ${a.name} for at least 30 days, so this can be undone from the Changelog.`,
    confirmLabel: 'Delete tag', danger: true, changelog: `Deleted tag ${a.tag} from ${a.name}`, undo: 'Put the tag back',
  }),
  git_ref_restore: (a) => ({
    title: `Restore ${short(a.ref)} in ${a.name}?`, body: `Points ${short(a.ref)} at the commit it had before it was deleted.`,
    confirmLabel: 'Restore', changelog: `Restored ${short(a.ref)} in ${a.name}`, undo: 'Delete it again',
  }),
};

/** Asks, then runs a Git change on the gateway. onDone runs after it worked. */
export function confirmGit(req: ActionReq, onDone?: () => void, o: { source?: 'assistant' } = {}): void {
  const copy = COPY[req.action];
  if (!copy) return;
  const args = req.args ?? {};
  const c = copy(args);
  ask({
    ...o,
    ...c,
    command: `${req.action} ${Object.entries(args).filter(([k]) => k !== 'confirm').map(([k, v]) => `${k}=${k === 'url' ? v.replace(/\/\/[^/@]+@/, '//') : v}`).join(' ')}`,
    run: () => {
      if (!mode.live) {
        toast(`${c.changelog} (demo: nothing changed)`, 'info');
        return;
      }
      const full = c.typed ? { ...req, args: { ...args, confirm: c.typed } } : req;
      const wait = req.action === 'git_import' ? 31 * 60_000 : 3 * 60_000;
      void sendAction(full, c.changelog, false, { where: 'the gateway', waitMs: wait }).then((cmd) => {
        if (cmd?.state === 'done') onDone?.();
      });
    },
  });
}
