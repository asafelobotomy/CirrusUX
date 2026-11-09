# Configuration

This page lists every setting. To install, start with `INSTALL.md`; app
definitions are described in `APPS.md`.

CirrusUX reads three small files, one per part. All of them use plain
`NAME=value` lines, which systemd's `EnvironmentFile=` reads for the gateway
and the agent. Secrets live only in these files (mode 600), never in the
browser, the logs or the repository (decision D12).

| File | On | Read by | Owner, mode |
|---|---|---|---|
| `/etc/cirrusux/gateway.env` | the gateway machine | `cirrusux-gateway` | cirrusux, 600 |
| `/etc/cirrusux/agent.env` | the server | `cirrusux-agent` | cirrusux, 600 |
| `/etc/cirrusux/helper.env` | the server | `cirrusux-helper` (root) | root, 644 or 600 |

`cirrusux-setup` adds two more: `host.env` (its own settings) and, with the
sleep module, `sleep.env`. The optional router check reads `router.env`.

Examples: `deploy/gateway/gateway.env.example`,
`deploy/server/agent.env.example`, `deploy/server/helper.env.example`.

## Setups

- **single:** the gateway and the agent run on the server itself, which
  doesn't sleep. The panel shows Online or No reports; there is no wake,
  sleep timer or stay-awake.
- **split:** the server sleeps, and the gateway runs on an always-on machine
  next to it. The panel shows Awake, Asleep, Waking and No reports, can wake
  the server, and answers from the last report while it sleeps.

## gateway.env

| Setting | Default | What it does |
|---|---|---|
| `CIRRUS_LISTEN` | `127.0.0.1:8064` | Address to listen on. Keep it on loopback: the front end (`tailscale serve` or a reverse proxy) publishes it |
| `CIRRUS_DATA_DIR` | `/var/lib/cirrusux-gateway` | Snapshot cache, settings, audit log |
| `CIRRUS_AGENT_TOKEN` | — (required) | Shared secret with the agent, 32+ characters |
| `CIRRUS_SERVER_NAME` | `server` | The server's name in the UI and the audit log |
| `CIRRUS_SERVER_HOST` | the server name | Host name or address app tiles open on |
| `CIRRUS_GATEWAY_NAME` | this machine's host name | Shown in notices about the gateway itself |
| `CIRRUS_SETUP` | `single` | `single` or `split` (above) |
| `CIRRUS_TZ` | this machine's zone | IANA zone for every time shown, e.g. `Europe/London` |
| `CIRRUS_SERVER_ABOUT` | — | A sentence or two for the assistant about the server |
| `CIRRUS_AUTH` | `tailscale` | `tailscale`: trust `Tailscale-User-Login` from `tailscale serve`. `header`: trust `CIRRUS_AUTH_HEADER` from a reverse proxy that signed the user in |
| `CIRRUS_AUTH_HEADER` | `Remote-User` | The header a reverse proxy sets (header mode) |
| `CIRRUS_TRUSTED_PROXIES` | `127.0.0.1,::1` | Addresses or prefixes the identity header is believed from |
| `CIRRUS_ALLOWED_USERS` | — (required) | Comma-separated logins allowed in |
| `CIRRUS_WAKE` | `none` (`command` if a command is set) | Split only: `wol`, `command` or `none` |
| `CIRRUS_WAKE_MAC` | — (the agent reports it) | `wol`, optional: the server's MAC address. Without it the gateway uses the one the agent last reported; set it only to override that, or if the agent hasn't reported yet |
| `CIRRUS_WAKE_BROADCAST` | `255.255.255.255:9` | `wol`: where the magic packet goes |
| `CIRRUS_WAKE_CMD` | — | `command`: argv to run, no shell. For a wake through a TR-064 router (when the gateway isn't on the server's LAN): `sudo -n /usr/local/sbin/cirrusux-router-wake` |
| `CIRRUS_TAILSCALE_PEER` | the server name | Split only: the server's Tailscale name, for awake/asleep. `-` to rely on the agent's reports only |
| `CIRRUS_POWER_CHECK_CMD` | — | Split only: a command that prints `online` or `offline` for the server, e.g. `sudo -n /usr/local/sbin/cirrusux-router-check` (TR-064). A second opinion beside Tailscale, the fallback when Tailscale can't be read, and a wake's "network up" step |
| `CIRRUS_POWER_CHECK_SECS` | `60` | How often the power check runs |
| `CIRRUS_CHANGELOG` | — | Also append every action to this Markdown file |
| `HTTPS_PROXY` | — (direct) | Check CirrusStore fetches catalogues through this proxy. Only when the owner presses it; verified files are cached in `CIRRUS_DATA_DIR/store/` |
| `ASSISTANT_PROVIDER` | — (off) | `xai`, `openai`, `anthropic`, `ollama` or `custom` |
| `ASSISTANT_MODEL` | — | The model; the assistant is off without one |
| `ASSISTANT_API_KEY` | — | Needed for hosted providers |
| `ASSISTANT_BASE_URL` | the provider's | Any OpenAI-compatible chat API (`custom` needs it) |
| `ASSISTANT_NAME` | Grok for xai, Claude for anthropic, else Assistant | Name shown in the UI |
| `CIRRUS_ASSISTANT_DAILY_TOKENS` | `200000` | Daily token budget |
| `CIRRUS_DIGEST_ASSISTANT` | off | `1` lets the 08:00 digest ask the assistant for a summary |
| `CIRRUS_GIT` | off | `1` turns on Git hosting (docs/GIT.md). Needs git 2.30 or newer on the gateway |
| `CIRRUS_GIT_DIR` | `$CIRRUS_DATA_DIR/git` | Where the Git repositories are kept |
| `CIRRUS_GIT_URL` | the panel's address | The address in clone URLs, e.g. `https://homeserver.example.ts.net` (no path) |
| `CIRRUS_GIT_MAX_PUSH_MB` | `1024` | The most one push may send |
| `CIRRUS_GIT_IMPORT_PRIVATE` | off | `1` lets Git import copy from addresses inside your own network (loopback, private, link-local, 100.64.0.0/10); off, those are refused |
| `CIRRUS_DEV` | off | `1` for development only: no sign-in, wakes are pretend |

## agent.env

| Setting | Default | What it does |
|---|---|---|
| `CIRRUS_GATEWAY_URL` | — (required) | The gateway's `/agent/snapshot` URL |
| `CIRRUS_AGENT_TOKEN` | — (required) | Same as the gateway's |
| `CIRRUS_DISK` | `/` | File system the disk widget shows; empty to skip |
| `CIRRUS_WAKE_IFACE` | the IPv4 default route's | The network card the server is woken on. The agent reports its MAC address, which the gateway wakes it at when `CIRRUS_WAKE_MAC` isn't set. Setup writes `NET_WOL_IFACE` here |
| `CIRRUS_IDLE_CONF` | `/etc/cirrusux/sleep.env` | Sleep module: the idle checker's config (`IDLE_MIN`, `GRACE_MIN`, `WAKE_TIMERS`) |
| `CIRRUS_PORT_NAMES` | — | Names for port numbers in activity reasons, e.g. `8080=Proxy,9000=Admin` |

## helper.env and modules

The helper runs as root, so it takes no paths from the agent: they come from
this file, which must belong to root and be writable only by root. Each
module is on when its settings are set. Without any, the panel still shows
containers, disk and temperature, and hides what the server doesn't have.

The helper reads `helper.env`, then every `helper.d/*.env` in name order
(written by `cirrusux-setup` modules); each must belong to root.

| Module | Settings | Adds |
|---|---|---|
| sleep | `IDLE_STATE_DIR`, `IDLE_LOG` | Sleep countdown, activity reasons, the sleep timeline |
| stay-awake | `STAY_AWAKE_FILE`, `STAY_AWAKE_BIN`, `ALLOW_SLEEP_BIN` | Stay awake / Allow sleep; actions keep the server up 10 minutes (D3) |
| backup | `BACKUP_UNIT`, `BACKUP_DIR`, `BACKUP_PREFIX` | Backups page and widget, Run backup now |
| av | `AV_LOG` | Virus-scan result |
| security | `FAIL2BAN_JAIL`, `FIREWALL_CHECKS` | fail2ban bans; firewall rule checks: `CHAIN:pattern` passes when `iptables -S CHAIN` contains the pattern, `family/table/chain:pattern` when `nft list chain …` does (e.g. `inet/cirrusux/input:drop` for the firewall module) |
| egress | `EGRESS_REPORT` | The outbound-traffic report on the Security page |
| storage | any of the folders above, `STORAGE_ROOTS` | The Storage page: folder sizes on request, and archive review |
| apps | `CONTAINER_RUNTIME`, `APPS_DIR`, `APPS_USER`, `MEDIA_ROOTS` (`APPS_ARCHIVE`, `APPS_BIND`) | CirrusStore: installs and removals as background jobs |
| store | `STORE_OFFICIAL` (`yes`), and the sources in `/etc/cirrusux/store.d/` | CirrusStore's online catalogues: the keys every app from one is checked against before it runs |

Paths must be absolute; unit and jail names may only use letters, digits and
`- _ . @`. The helper refuses the file otherwise.

The `cirrusux-setup` modules fill these in for you. Without them, a
module's settings can point at scripts and units of your own, as long as
they behave the same way (see each module in `deploy/host/modules/`).

## host.env

`cirrusux-setup` reads `/etc/cirrusux/host.env` (owner root, not writable by
anyone else). The example, `deploy/host/host.env.example`, lists every
setting; each module's file in `deploy/host/modules/` describes its own.

| Setting | Default | What it does |
|---|---|---|
| `HOST_MODULES` | `base cirrusux` | Modules to run when none are named |
| `HOST_TZ` | — | base: time zone to set |
| `HOST_AUTO_UPDATES` | `no` | base: `no` switches off apt's daily upgrade timer |
| `CIRRUS_ROLE` | `single` | cirrusux: `single`, `server` or `gateway` |
| `CIRRUS_ACTIONS` | `yes` | cirrusux: `no` installs a read-only sudoers rule |
| `CIRRUS_SERVER_NAME`, `CIRRUS_ALLOWED_USERS` | — | cirrusux: written into a new gateway.env |
| `CIRRUS_RELEASE` | this copy | cirrusux: folder holding `bin/` and `deploy/`. Leave it out with a release from `install.sh`, so `upgrade` and `rollback` take effect |
| `CIRRUS_TAILSCALE_SERVE` | `no` | cirrusux: `yes` publishes the panel on the tailnet with `tailscale serve --https=443` (single and gateway roles) |
| `CIRRUS_PAIR_URL` | `https://<this machine's Tailscale name>/agent/snapshot` | `pair`: the address the server's agent reports to |
| `CIRRUS_INIT` | `systemd` when it's running, else `runit` | `runit`: a gateway machine without systemd; only base, cirrusux (`CIRRUS_ROLE=gateway`) and network run (see INSTALL.md) |
| `FIREWALL_ALLOW` | — | firewall: networks allowed in (IPv4 and IPv6) |
| `FIREWALL_TAILSCALE` | `yes` | firewall: also allow the tailnet |
| `FIREWALL_PUBLIC_TCP` | — | firewall: TCP ports open to everyone, for services on the host and for containers' published ports alike |
| `FIREWALL_CONFIRM_SECONDS` | `120` | firewall: time to confirm before rollback |
| `NET_WOL_IFACE` | — | network: wired interface to keep Wake-on-LAN on |
| `NET_TAILSCALE` | `yes` | network: check tailscaled starts at boot and is logged in |
| `NET_TAILSCALE_SET` | — | network: preferences applied with `tailscale set`, e.g. `--accept-dns=false --accept-routes`. Only `--flag` and `--flag=value` words. Applied when it changes; the preferences before the first change are kept in `/var/lib/cirrusux/tailscale-prefs.before` for the undo |
| `NET_TAILSCALED_FLAGS` | — | network: tailscaled's own flags, e.g. `--no-logs-no-support`, written to `FLAGS=` in `/etc/default/tailscaled`, followed by a logged restart of tailscaled (Tailscale connections drop for a moment). Not used without systemd |
| `BACKUP_AGE_RECIPIENT` | — (required) | backup: age public key(s); the private key stays elsewhere |
| `BACKUP_DIR` | `/srv/backups` | backup: where archives go |
| `BACKUP_PATHS` | `/etc /root /home /usr/local /var/lib/cirrusux /var/lib/cirrusux-gateway /srv/cirrusux` (one default, in `deploy/host/lib.sh`) | backup: what goes in |
| `BACKUP_EXCLUDE` | — | backup: what stays out |
| `BACKUP_SCHEDULE` | `*-*-* 03:30` | backup: systemd `OnCalendar`; the timer wakes a sleeping server |
| `BACKUP_KEEP` | `7` | backup: newest archives kept, e.g. `14` for two weeks of nightly backups; `0` keeps all (the only automatic deletion) |
| `BACKUP_REQUIRE_MOUNT` | — | backup: a mount point `BACKUP_DIR` must be on (an external disk's `/mnt/media`). Without the disk mounted, the backup fails instead of filling the system disk. The unit also gets `RequiresMountsFor=` on `BACKUP_DIR`, so systemd mounts it first when it's in fstab |
| `SEC_FAIL2BAN` | `yes` | security: fail2ban `sshd` jail, reading the journal, banning through nftables |
| `SEC_FAIL2BAN_IGNOREIP` | `127.0.0.1/8 ::1` | security: addresses and networks never banned, e.g. add the tailnet, `100.64.0.0/10` |
| `SEC_FAIL2BAN_BANTIME` | `600` | security: how long a ban lasts, in seconds or with `s`, `m`, `h`, `d`, `w`; `-1` forever |
| `SEC_FAIL2BAN_FINDTIME` | `600` | security: failures count within this window |
| `SEC_FAIL2BAN_MAXRETRY` | `5` | security: failures before a ban |
| `SSH_ALLOW` | — (no restriction) | ssh: who may log in over SSH and from where, as sshd's `AllowUsers`: `USER@NETWORK`, or a `NETWORK` alone for anyone there. Written to `sshd_config.d/50-restrict-sources.conf`, checked with `sshd -t`; password logins are left alone. Refused if it would shut out the SSH session running setup |
| `SSH_CONFIRM_SECONDS` | `120` | ssh: a change rolls back by itself unless `cirrusux-setup ssh-confirm` runs within this time |
| `FTPS_USER` | `media` | ftps: the one login, a pure-ftpd virtual user. Its password is set with `sudo cirrusux-setup ftps-passwd`, which asks for it; it's never in a file of ours |
| `FTPS_HOME` | `/mnt/media` | ftps: where the user lands, and all it can see |
| `FTPS_PASSIVE_PORTS` | `39000 40000` | ftps: the passive data ports, first and last. The firewall module already allows every port from the LAN and the tailnet. If `SLEEP_WATCH_PORTS` is set, add `21` to it |
| `FTPS_UID`, `FTPS_GID` | `APPS_MEDIA_UID`, `APPS_MEDIA_GID` | ftps: the IDs files uploaded over FTPS get |
| `SEC_AV` | `yes` | security: weekly ClamAV scan; signatures update just before it |
| `SEC_AV_PATHS` | `/home /srv /usr/local /tmp /var/tmp` | security: what the scan covers |
| `SEC_AV_SCHEDULE` | `Sun *-*-* 04:00` | security: when it runs |
| `APPS_RUNTIME` | `podman` | apps: `podman` (Quadlet units) or `docker` (compose) |
| `APPS_DIR` | `/srv/cirrusux/apps` | apps: each app's own folders |
| `APPS_ARCHIVE` | `/srv/cirrusux/archive` | apps: removed apps' folders, kept |
| `APPS_MEDIA_ROOTS` | `/srv/media` | apps: folders media may be chosen from |
| `APPS_BIND` | — (all) | apps: publish apps' ports on this address only |
| `APPS_MEDIA_UID`, `APPS_MEDIA_GID` | — | apps: the media's owner. `cirrusux-apps` is created with these IDs, and a `media` group with the GID if no group has it, so files apps write match the rest of the media. Set both. An existing `cirrusux-apps` with other IDs is never changed, only warned about |
| `STORE_OFFICIAL` | `yes` | store: trust the official CirrusStore catalogue, whose key is built into CirrusUX. `no` leaves only the built-in apps and `STORE_SOURCES` |
| `STORE_SOURCES` | — | store: third-party catalogues, space-separated, each `name\|https://host/path/\|KEY`, where `KEY` is the last field of the publisher's `NAME.pub`. Each becomes `/etc/cirrusux/store.d/<name>.source` (root, 644); one dropped from the list is removed. See APPS.md, "Hosting your own source" |
| `SLEEP_IDLE_MIN` | `30` | sleep: quiet minutes before it suspends |
| `SLEEP_GRACE_MIN` | `15` | sleep: minutes after a wake before it may sleep again |
| `SLEEP_WATCH_PORTS` | — (all) | sleep: only these local ports count |
| `SLEEP_IGNORE_CLIENTS` | — | sleep: addresses or networks whose connections don't count |
| `SLEEP_IGNORE_CLIENTS_PLAY_MB` | `0` (off) | sleep: an ignored client pulling more than this many MB a minute is playing something, and counts after all |
| `SLEEP_IGNORE_WAKE_GRACE_MIN` | `0` (off) | sleep: after a wake that only ignored clients used, it sleeps this many minutes after the wake |
| `SLEEP_MAX_LOAD` | `0` (off) | sleep: a 5-minute load above this counts as activity |
| `SLEEP_COUNT_LOGINS` | `yes` | sleep: logged-in sessions count |
| `SLEEP_WAKE_TIMERS` | backup and scan timers | sleep: timers that wake it, shown as the next wake |
| `SLEEP_RESUME_FIXES` | `no` | sleep: `yes` adds `cirrusux-resume.service`, run after each resume: Wake-on-LAN set again on `NET_WOL_IFACE`, timesyncd restarted, and Tailscale's state written to the journal |
| `SLEEP_NO_WAKEUP_DEVICES` | — | sleep: PCI addresses (`lspci -D`) that may not wake the server, e.g. a USB controller that wakes it for nothing. A udev rule keeps them off at every boot |

The sleep module installs `cirrusux-idle`, CirrusUX's own sleep manager. A
timer runs `cirrusux-idle check` every minute: inbound connections (to the
machine, and through conntrack to containers' published ports), logins,
busy units (backups, scans, app jobs) and stay-awake holds count as
activity; outbound connections never do. Someone reaching an app through
a proxy on the server itself (`tailscale serve`) shows as Podman's bridge
gateway, and counts. The sleep module turns on per-connection byte counts
(`net.netfilter.nf_conntrack_acct=1`) for `SLEEP_IGNORE_CLIENTS_PLAY_MB`.
It suspends after the quiet time,
and timers with `WakeSystem=yes` wake it. `cirrusux-idle status` shows what
it sees without changing anything, and `cirrusux-idle check --dry-run`
writes nothing at all. Anything it can't read counts as activity.

Modules that the panel shows (backup, virus scan, security) tell the helper
about themselves in `/etc/cirrusux/helper.d/`, so `helper.env` stays yours.

## sleep.env

The sleep module writes `/etc/cirrusux/sleep.env` from the `SLEEP_*`
settings above. `cirrusux-idle` reads it every minute, and the agent reads
`IDLE_MIN`, `GRACE_MIN` and `WAKE_TIMERS` from it for the panel. Re-run
`cirrusux-setup apply sleep` after changing host.env, rather than editing it.

| Setting | Default | What it does |
|---|---|---|
| `IDLE_MIN` | `30` | Quiet minutes before it suspends |
| `GRACE_MIN` | `15` | Minutes after a wake before it may sleep again |
| `WATCH_PORTS` | — (all) | Space-separated local ports whose inbound connections count |
| `IGNORE_CLIENTS` | — | Addresses or networks whose connections don't count |
| `IGNORE_CLIENTS_PLAY_MB` | `0` (off) | An ignored client that was sent more than this many MB (millions of bytes) a minute counts as "playback by ignored client" |
| `IGNORE_WAKE_GRACE_MIN` | `0` (off) | When an ignored client was connected since the last wake and nothing else counted, it sleeps this many minutes after the wake, instead of waiting for `IDLE_MIN` and `GRACE_MIN` |
| `BUSY_UNITS` | `cirrusux-backup.service cirrusux-av-scan.service cirrusux-job-*` | Units that keep it awake while active; globs allowed. The module doesn't set it, and the next `apply sleep` replaces a hand edit, so the default is what runs |
| `MAX_LOAD` | `0` (off) | A 5-minute load above this counts as activity |
| `COUNT_LOGINS` | `yes` | `no`: logged-in sessions don't count |
| `WAKE_TIMERS` | `cirrusux-backup.timer cirrusux-av-scan.timer` | Timers that wake it, shown as the next wake |

## router.env

Read only by `cirrusux-router-check` and `cirrusux-router-wake` (split
setup, optional), as root. You
write it yourself, owner root and mode 600; the script refuses it otherwise.
The panel never sees it.

| Setting | Default | What it does |
|---|---|---|
| `ROUTER_URL` | `http://fritz.box:49000` | The router's TR-064 address |
| `ROUTER_USER` | — (required) | A router user allowed to read the host list |
| `ROUTER_PASSWORD` | — (required) | That user's password |
| `SERVER_MAC` | — (required) | The server's MAC address, as the router lists it |
| `ROUTER_PROXY` | — (direct) | Reach the router through a proxy: `socks5h://localhost:1055` (a userspace-mode Tailscale's SOCKS5 server, or a wireproxy tunnel), `socks5://…` or `http://…`. Given to curl on stdin, so a password in it stays out of the process list |

`cirrusux-router-wake` (a symlink to `cirrusux-router-check`) reads the same
file and asks the router to send the server a magic packet.
