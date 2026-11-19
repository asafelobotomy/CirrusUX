import { test } from 'node:test';
import assert from 'node:assert/strict';
import { keyProblem, loginMethods, people, sshAllowed, type Account, type SSHInfo } from '../src/lib/access.ts';

const ssh: SSHInfo = { installed: true, running: true, ports: [22], passwordAuth: false, pubkeyAuth: true, kbdInteractive: true, rootLogin: 'prohibit-password', sftp: true, totp: true };
const user = (o: Partial<Account>): Account => ({ name: 'alice', uid: 1000, home: '/home/alice', shell: '/bin/bash', canLogin: true, admin: false, password: true, locked: false, keys: [], totp: false, ...o });
const key = (sk = false) => ({ type: sk ? 'sk-ssh-ed25519@openssh.com' : 'ssh-ed25519', fingerprint: 'SHA256:x', public: 'x', securityKey: sk });

test('a pasted key is checked before it is sent', () => {
  assert.equal(keyProblem('ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx alice@laptop'), null);
  assert.match(keyProblem('-----BEGIN OPENSSH PRIVATE KEY-----') ?? '', /private key/);
  assert.match(keyProblem('ssh-dss AAAA') ?? '', /starts with/);
  assert.match(keyProblem('ssh-ed25519 AAAA x"; rm -rf /') ?? '', /comment/);
  assert.match(keyProblem('') ?? '', /Paste/);
});

test('AllowUsers and locks decide who may use SSH', () => {
  assert.equal(sshAllowed(ssh, user({})), true);
  assert.equal(sshAllowed({ ...ssh, allowUsers: ['bob@192.0.2.0/24'] }, user({})), false);
  assert.equal(sshAllowed({ ...ssh, allowUsers: ['alice@192.0.2.0/24'] }, user({})), true);
  assert.equal(sshAllowed(ssh, user({ locked: true })), false);
  assert.equal(sshAllowed({ ...ssh, rootLogin: 'no' }, user({ uid: 0, name: 'root' })), false);
});

test('login methods follow what sshd allows', () => {
  assert.deepEqual(loginMethods(ssh, user({ keys: [key(), key(true)], totp: true })), ['2 keys (1 security key)', 'one-time code']);
  assert.deepEqual(loginMethods({ ...ssh, passwordAuth: true }, user({})), ['password']);
  assert.deepEqual(loginMethods(ssh, user({})), []);
});

test('people are login accounts from UID 1000', () => {
  const a = { ssh, users: [user({ name: 'root', uid: 0 }), user({}), user({ name: 'media', uid: 1001, canLogin: false })] };
  assert.deepEqual(people(a).map((u) => u.name), ['alice']);
  assert.deepEqual(people(null), []);
});
