# Reinstalling cloud64, and the gateway on cloud-box

This is ROADMAP Phase 8 for the cloud64 profile: a fresh Debian 13 on
cloud64, set up by `cirrusux-setup`, with apps from CirrusStore under
Podman and no 1Panel; and the gateway on cloud-box, which has no systemd,
under runit. The general guide is docs/INSTALL.md, and every setting is in
docs/CONFIG.md.

The settings to fill in are in this folder as examples with names and
comments only: `host.env.example` (cloud64), `gateway.env.example`
(cloud-box), `agent.env.example` and `helper.env.example`. Real values,
addresses, interface names and PCI addresses go only into the files on the
machines.

cloud64 is out of service from step 3 until the apps are back in step 7.
Every change setup makes is in `/var/lib/cirrusux/setup.log` with its undo.

## 1. Capture (before anything else)

On cloud64, from this folder: `sudo bash capture.sh`. It's read-only, leaves
keys and env files out, and names (without printing) any file that looks
like it holds a secret. Copy the result off cloud64. It holds what the new
settings come from: `/etc/cloud64-idle.conf` (the idle times, the TV's
address and its 5 MB/min threshold), the media disk's `fstab` line, the
Wake-on-LAN setting, `tailscale debug prefs`, sshd's and fail2ban's
settings, the egress report script and the old compose files.

Archive each app's config folder to the media disk; the new apps start
clean.

## 2. Prove a backup restores

On the admin machine, where the private key lives, take the newest backup
from `/mnt/media/BackUp/` and check it decrypts and lists:

```bash
age -d -i key.txt <newest>.tar.zst.age | zstd -dc | tar -tvf - | head
```

Don't go on until this works.

## 3. Reinstall Debian 13 on cloud64

Install Debian 13 without a desktop, with SSH. **Don't format the media
disk.** Put its `fstab` line back from the capture (mounted at `/mnt/media`,
with `nofail`, so a missing disk doesn't stop the boot), then
`sudo mount -a` and check the media is there with `ls -ln /mnt/media`. Note
the owner's numbers: they are `APPS_MEDIA_UID` and `APPS_MEDIA_GID`.

Install Tailscale (https://tailscale.com/download/linux) and `sudo tailscale
up` with the same node name as before.

## 4. CirrusUX on cloud64

Build on the admin machine (`make test && make build`), then copy `bin/` and
`deploy/` to cloud64 and make them root's:

```bash
sudo mkdir -p /opt/cirrusux && sudo cp -r bin deploy /opt/cirrusux/ && sudo chown -R root:root /opt/cirrusux
sudo install -D -m 600 /dev/null /etc/cirrusux/host.env    # then fill it in from host.env.example
sudo /opt/cirrusux/deploy/host/cirrusux-setup check
sudo /opt/cirrusux/deploy/host/cirrusux-setup apply
```

Leave `firewall` and `ssh` out of `HOST_MODULES` for the first run. Then add
them, apply again from an SSH session on the LAN or the tailnet, check a
new session still gets in, and confirm each within two minutes:

```bash
sudo cirrusux-setup firewall-confirm
sudo cirrusux-setup ssh-confirm
```

Then choose the FTPS password (it's asked for, never stored by CirrusUX):
`sudo cirrusux-setup ftps-passwd`.

What this sets up, from the settings:
- the sleep manager with the old idle check's tuning: the TV is ignored
  unless it streams (`SLEEP_IGNORE_CLIENTS_PLAY_MB`), and a wake only the TV
  used ends after a few minutes (`SLEEP_IGNORE_WAKE_GRACE_MIN`); after each
  resume, Wake-on-LAN is set again and the clock synced
  (`SLEEP_RESUME_FIXES`), and the USB controller no longer wakes it
  (`SLEEP_NO_WAKEUP_DEVICES`);
- Wake-on-LAN (`g`) on the wired card, kept across reboots;
- backups to `/mnt/media/BackUp/…`, two weeks kept, refused when the disk
  isn't mounted (`BACKUP_REQUIRE_MOUNT`);
- fail2ban banning through nftables, never the tailnet; SSH limited to the
  admin login from the LAN and the tailnet, password login left on;
- Podman, and the `cirrusux-apps` user with the media's own IDs;
- FTPS (pure-ftpd, TLS required, passive ports 39000–40000), one user in
  `/mnt/media`. The firewall already allows every port from the LAN and the
  tailnet.

Until CirrusUX logs outgoing traffic itself, install the captured egress
report as `/usr/local/sbin/egress-report.sh` (root, mode 755) and name it in
`/etc/cirrusux/helper.env` (`helper.env.example`).

## 5. The token

Make one shared token, `head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'`,
and put it in `CIRRUS_AGENT_TOKEN` in cloud64's `/etc/cirrusux/agent.env` and
cloud-box's `/etc/cirrusux/gateway.env`. Set the rest of `agent.env` from
`agent.env.example`.

## 6. The gateway on cloud-box (runit, no systemd)

cloud-box's PID 1 is tini, and its tailscaled runs in userspace mode with a
SOCKS5 server on localhost:1055. Setup never starts, restarts or duplicates
that tailscaled.

1. Install runit, sudo and curl (`sudo apt install runit sudo curl`), and
   make sure `runsvdir -P /etc/service` runs (started by tini, or by the
   box's own start script).
2. Copy `bin/` and `deploy/` to `/opt/cirrusux` as on cloud64, root-owned.
3. `/etc/cirrusux/host.env` with `HOST_MODULES="base cirrusux"`,
   `CIRRUS_ROLE=gateway`, `HOST_TZ`, `CIRRUS_SERVER_NAME` and
   `CIRRUS_ALLOWED_USERS`; then `cirrusux-setup check` and `apply`. It finds
   there's no systemd and installs the runit service
   (`/etc/sv/cirrusux-gateway`); the gateway starts once `gateway.env` is
   filled in from `gateway.env.example` and `apply` runs again.
4. The router: install `deploy/gateway/cirrusux-router-check` as in
   docs/INSTALL.md ("The router check and wake"), with the
   `cirrusux-router-wake` link and both sudoers rules. In
   `/etc/cirrusux/router.env` (root, 600), add
   `ROUTER_PROXY=socks5h://localhost:1055`. Test both while cloud64 sleeps:
   `sudo /usr/local/sbin/cirrusux-router-check` says `offline`, and
   `sudo /usr/local/sbin/cirrusux-router-wake` says `wake sent` and cloud64
   comes up.
5. Publish it on the tailnet only:
   `sudo tailscale serve --bg --https=443 http://127.0.0.1:8064`. In
   userspace mode the proxy runs inside tailscaled, so it needs no tun
   device.

Open `https://cloud-box.<tailnet>.ts.net/`. The gateway's log is
`/var/log/cirrusux-gateway/current`.

The old wake wrapper (`cirrusux-wake`, which ran the admin user's
`cloud64-wake`) stays in this folder until `cirrusux-router-wake` has woken
cloud64 for real; then remove it and its sudoers rule.

## 7. Apps

From CirrusStore in the panel, with `/mnt/media` (or folders inside it) as
the media: Jellyfin (tag `12.4.20261026-211555`), Navidrome, Audiobookshelf,
Kavita, tinyMediaManager and Picard. They start clean. tinyMediaManager and
Picard write to the media as its owner; switch off their update checks as
their pages say.

Reinstall Jellyfin's Trakt plugin from Jellyfin's Dashboard → Plugins →
Catalog, with the same settings as before. Version 33.0.0.0 was checked to
load on this Jellyfin tag (docs/APPS.md).

## 8. Watch it for 24 hours

The sleep manager is the riskiest part to rebuild. On cloud64, read
`/var/log/cirrusux-idle.log` (or the panel's Power page): no extra wakes, no
delayed suspends, `playback by ignored client …` only while the TV streams,
and `only ignored clients since the wake` sleeping a few minutes after a
TV-only wake. Check a backup ran to the media disk and shows in the panel.
