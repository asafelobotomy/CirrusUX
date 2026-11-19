// Sample accounts and network for the demo (no gateway).
import type { AccessInfo, NetworkInfo } from '../access';

const key = (type: string, fp: string, comment: string, sk = false) => ({
  type, fingerprint: 'SHA256:' + fp.padEnd(43, 'A'), comment, securityKey: sk, public: `${type} AAAAC3NzaC1lZDI1NTE5AAAAI${fp}`,
});

export const access: AccessInfo = {
  ssh: {
    installed: true, running: true, ports: [22], passwordAuth: false, pubkeyAuth: true, kbdInteractive: true,
    rootLogin: 'prohibit-password', authMethods: 'publickey', sftp: true, sftpInternal: false,
    allowUsers: ['alice@192.0.2.0/24', 'alice@100.64.0.0/10'], totp: true,
  },
  users: [
    { name: 'root', uid: 0, home: '/root', shell: '/bin/bash', canLogin: true, admin: true, password: false, locked: false, keys: [], totp: false },
    {
      name: 'alice', uid: 1000, home: '/home/alice', shell: '/bin/bash', canLogin: true, admin: true, password: true, locked: false, totp: true,
      keys: [key('ssh-ed25519', 'laptop7Qx2', 'alice@laptop'), key('sk-ssh-ed25519@openssh.com', 'yubiKey9Pz', 'alice@yubikey', true)],
    },
    { name: 'media', uid: 1001, home: '/srv/media', shell: '/usr/sbin/nologin', canLogin: false, admin: false, password: false, locked: false, keys: [], totp: false },
    { name: 'guest', uid: 1002, home: '/home/guest', shell: '/bin/bash', canLogin: true, admin: false, password: true, locked: true, lockedByPanel: true, keys: [], totp: false },
  ],
  ftps: { installed: true, running: true, tls: 3, user: 'media', userSet: true, home: '/mnt/media', passivePorts: '39000 40000', anonymous: false, certSha256: 'AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89' },
};

export const network: NetworkInfo = {
  hostname: 'homeserver',
  interfaces: [
    { name: 'lo', kind: 'loopback', state: 'up', mtu: 65536, addrs: ['127.0.0.1/8', '::1/128'] },
    { name: 'enp1s0', kind: 'ethernet', state: 'up', mac: 'aa:bb:cc:dd:ee:ff', mtu: 1500, mbps: 1000, addrs: ['192.0.2.10/24', 'fe80::a8bb:ccff:fedd:eeff/64'] },
    { name: 'tailscale0', kind: 'tailscale', state: 'up', mtu: 1280, addrs: ['100.64.0.10/32', 'fd7a:115c:a1e0::a/128'] },
    { name: 'docker0', kind: 'bridge', state: 'up', mac: 'aa:bb:cc:dd:ee:01', mtu: 1500, addrs: ['172.17.0.1/16'] },
  ],
  gateway: '192.0.2.1', gatewayIface: 'enp1s0', dns: ['192.0.2.1'], search: ['lan'], tailscale: ['100.64.0.10'], hidden: 6,
};
