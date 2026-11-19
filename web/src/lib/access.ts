// Who can reach the server and how (the snapshot's access and network
// sections): login accounts, SSH keys, SFTP, FTPS, and its network.

export interface SSHKey {
  type: string;
  fingerprint: string;
  comment?: string;
  /** A FIDO2 security key (sk-…): the SSH form of a passkey. */
  securityKey?: boolean;
  options?: boolean;
  public: string;
}

export interface Account {
  name: string; uid: number; home: string; shell: string;
  canLogin: boolean; admin: boolean; password: boolean;
  locked: boolean; lockedByPanel?: boolean;
  keys: SSHKey[]; totp: boolean; keysError?: string; groups?: string[];
}

export interface SSHInfo {
  installed: boolean; running: boolean; ports: number[];
  passwordAuth: boolean; pubkeyAuth: boolean; kbdInteractive: boolean;
  rootLogin: string; authMethods?: string;
  sftp: boolean; sftpInternal?: boolean;
  allowUsers?: string[]; allowGroups?: string[];
  totp: boolean; keysFile?: string;
}

export interface FTPSInfo {
  installed: boolean; running: boolean;
  /** pure-ftpd's TLS level: 3 refuses plain FTP. */
  tls: number;
  user?: string; userSet: boolean; home?: string; passivePorts?: string;
  anonymous: boolean; certSha256?: string;
}

export interface AccessInfo { ssh: SSHInfo; users: Account[]; ftps?: FTPSInfo; errors?: string[] }

export interface NetIface { name: string; kind: string; state: string; mac?: string; mtu: number; mbps?: number; addrs: string[] }

export interface NetworkInfo {
  hostname: string; interfaces: NetIface[];
  gateway?: string; gatewayIface?: string;
  dns: string[]; search?: string[]; tailscale?: string[]; hidden?: number;
}

/** The accounts a person logs in with: UID 1000 and up, with a shell. */
export const people = (a: AccessInfo | null): Account[] => (a?.users ?? []).filter((u) => u.uid >= 1000 && u.canLogin);

/** Whether a user may log in over SSH under sshd's AllowUsers and AllowGroups (both must let them in). */
export function sshAllowed(ssh: SSHInfo, u: Account): boolean {
  if (u.locked || !u.canLogin) return false;
  if (u.uid === 0 && ssh.rootLogin === 'no') return false;
  const allow = ssh.allowUsers ?? [];
  const byUser = !allow.length || allow.some((w) => {
    const name = w.split('@')[0];
    return name === '*' || name === u.name;
  });
  const groups = ssh.allowGroups ?? [];
  const byGroup = !groups.length || groups.some((g) => (u.groups ?? []).includes(g));
  return byUser && byGroup;
}

/** The way into each account, in words. */
export function loginMethods(ssh: SSHInfo, u: Account): string[] {
  const out: string[] = [];
  const sk = u.keys.filter((k) => k.securityKey).length;
  if (ssh.pubkeyAuth && u.keys.length) out.push(sk ? `${u.keys.length} key${u.keys.length > 1 ? 's' : ''} (${sk} security key${sk > 1 ? 's' : ''})` : `${u.keys.length} key${u.keys.length > 1 ? 's' : ''}`);
  if (ssh.passwordAuth && u.password) out.push('password');
  if (ssh.totp && u.totp) out.push('one-time code');
  return out;
}

/** Reads a pasted public key just enough to warn early; the server checks it properly. */
export function keyProblem(text: string): string | null {
  const f = text.trim().split(/\s+/);
  const types = ['ssh-ed25519', 'sk-ssh-ed25519@openssh.com', 'ecdsa-sha2-nistp256', 'ecdsa-sha2-nistp384', 'ecdsa-sha2-nistp521', 'sk-ecdsa-sha2-nistp256@openssh.com', 'ssh-rsa'];
  if (!text.trim()) return 'Paste a public key.';
  if (/PRIVATE KEY/.test(text)) return "That's a private key. Never paste it anywhere: paste the .pub file instead.";
  if (!types.includes(f[0])) return 'A public key starts with ssh-ed25519, sk-ssh-ed25519@openssh.com, ecdsa-sha2-… or ssh-rsa.';
  if (f.length < 2 || !/^[A-Za-z0-9+/]+=*$/.test(f[1])) return "The key part after the type doesn't look right.";
  if (f.length > 2 && !/^[A-Za-z0-9@._+:-][A-Za-z0-9@._+: -]{0,99}$/.test(f.slice(2).join(' '))) return 'The comment may use letters, digits, spaces and @._+:- (up to 100).';
  return null;
}
