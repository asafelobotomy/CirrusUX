// Account changes from the Accounts page: SSH keys and locking. Each is
// confirmed, logged and undoable; the server checks every one again.
import { live } from './live.svelte';
import { ask, toast } from './ui.svelte';
import { mode } from '../api.svelte';
import { offline, sendAction, type Opts } from './send';
import type { Account, SSHKey } from '../access';

const keyName = (k: { comment?: string; fingerprint: string }) => (k.comment ? `"${k.comment}"` : k.fingerprint);

function user(name: string): Account | undefined {
  return live.access?.users.find((u) => u.name === name);
}

export function confirmAddKey(name: string, key: string, o: Opts = {}): void {
  if (offline('ssh_key_add')) return;
  const f = key.trim().split(/\s+/);
  const comment = f.slice(2).join(' ');
  const sk = f[0]?.startsWith('sk-');
  ask({
    ...o,
    title: `Add this key for ${name}?`,
    body: `Whoever holds the matching private key${sk ? ' (a security key, which also needs a touch)' : ''} can log in to ${mode.server} as ${name} over SSH and SFTP.`,
    command: `append to ~${name}/.ssh/authorized_keys: ${f[0]} …${comment ? ' ' + comment : ''}`,
    undo: 'remove the same key',
    confirmLabel: 'Add key',
    changelog: `Added an SSH key for ${name}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'ssh_key_add', args: { user: name, key: key.trim() } }, `Added an SSH key for ${name}`);
        return;
      }
      user(name)?.keys.push({ type: f[0], fingerprint: 'SHA256:demo' + Date.now(), comment, securityKey: sk, public: f.slice(0, 2).join(' ') });
      toast('Demo: key added (nothing on a server changed).', 'ok');
    },
  });
}

export function confirmRemoveKey(name: string, k: SSHKey, o: Opts = {}): void {
  if (offline('ssh_key_remove')) return;
  ask({
    ...o,
    title: `Remove ${keyName(k)} from ${name}?`,
    body: `That key stops working for new SSH and SFTP logins as ${name}. Sessions already open stay open.`,
    command: `remove ${k.fingerprint} from ~${name}/.ssh/authorized_keys`,
    undo: 'add the same key back',
    danger: true,
    confirmLabel: 'Remove key',
    changelog: `Removed an SSH key from ${name}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'ssh_key_remove', args: { user: name, fingerprint: k.fingerprint } }, `Removed an SSH key from ${name}`);
        return;
      }
      const u = user(name);
      if (u) u.keys = u.keys.filter((x) => x.fingerprint !== k.fingerprint);
    },
  });
}

export function confirmLock(name: string, lock: boolean, o: Opts = {}): void {
  if (offline(lock ? 'account_lock' : 'account_unlock')) return;
  ask({
    ...o,
    title: `${lock ? 'Lock' : 'Unlock'} ${name}?`,
    body: lock
      ? `No new login works as ${name}: password, keys, SFTP, FTPS. Sessions already open stay open, and the account's files are kept.`
      : `${name} can log in again the ways it could before.`,
    command: lock ? `usermod --expiredate 1970-01-02 ${name}` : `usermod --expiredate '' ${name}`,
    undo: lock ? `usermod --expiredate '' ${name}` : `usermod --expiredate 1970-01-02 ${name}`,
    danger: lock,
    confirmLabel: lock ? 'Lock account' : 'Unlock account',
    changelog: `${lock ? 'Locked' : 'Unlocked'} the account ${name}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: lock ? 'account_lock' : 'account_unlock', args: { user: name } }, `${lock ? 'Locked' : 'Unlocked'} the account ${name}`);
        return;
      }
      const u = user(name);
      if (u) Object.assign(u, { locked: lock, lockedByPanel: lock });
    },
  });
}

export function confirmCreateAccount(name: string, role: 'user' | 'admin', o: Opts = {}): void {
  if (offline('account_create')) return;
  const what = role === 'admin' ? 'an administrator account' : 'a standard account';
  ask({
    ...o,
    title: `Create ${name}?`,
    body: `Makes ${what} on ${mode.server} with a home folder. It has no password and no key, so nobody can log in as ${name} until you add a key on this page.${role === 'admin' ? ' Administrators can run commands as root.' : ''}`,
    command: `useradd --create-home --shell /bin/bash${role === 'admin' ? ' --groups sudo' : ''} ${name}`,
    undo: `userdel ${name} (its home folder is kept)`,
    confirmLabel: 'Create account',
    danger: role === 'admin',
    changelog: `Created the ${role === 'admin' ? 'administrator' : 'standard'} account ${name}`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'account_create', args: { user: name, role } }, `Created the account ${name}`);
        return;
      }
      live.access?.users.push({ name, uid: 1100 + (live.access.users.length), home: `/home/${name}`, shell: '/bin/bash', canLogin: true, admin: role === 'admin', password: false, locked: false, createdByPanel: true, keys: [], totp: false });
      toast('Demo: account created (nothing on a server changed).', 'ok');
    },
  });
}

export function confirmRemoveAccount(name: string, o: Opts = {}): void {
  if (offline('account_remove')) return;
  ask({
    ...o,
    title: `Remove ${name}?`,
    body: `${name} can no longer log in. Its home folder and files stay where they are, so nothing is deleted. Only accounts made from this page can be removed here, and never the last admin who can log in.`,
    command: `userdel ${name}`,
    undo: `useradd --create-home ${name} (the kept home folder is reused)`,
    danger: true,
    confirmLabel: 'Remove account',
    changelog: `Removed the account ${name} (home folder kept)`,
    run: () => {
      if (mode.live) {
        void sendAction({ action: 'account_remove', args: { user: name } }, `Removed the account ${name}`);
        return;
      }
      if (live.access) live.access.users = live.access.users.filter((u) => u.name !== name);
    },
  });
}
