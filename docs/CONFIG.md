# Configuration

CirrusUX reads three small files, one per part. All of them use plain
`NAME=value` lines, which systemd's `EnvironmentFile=` reads for the gateway
and the agent. Secrets live only in these files (mode 600), never in the
browser, the logs or the repository (decision D12).

| File | On | Read by | Owner, mode |
|---|---|---|---|
| `/etc/cirrusux/gateway.env` | the gateway machine | `cirrusux-gateway` | cirrusux, 600 |
| `/etc/cirrusux/agent.env` | the server | `cirrusux-agent` | cirrusux, 600 |
| `/etc/cirrusux/helper.env` | the server | `cirrusux-helper` (root) | root, 644 or 600 |

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
| `CIRRUS_DATA_DIR` | `/var/lib/cirrusux` | Snapshot cache, settings, audit log |
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
| `CIRRUS_WAKE_MAC` | — | `wol`: the server's MAC address |
| `CIRRUS_WAKE_BROADCAST` | `255.255.255.255:9` | `wol`: where the magic packet goes |
| `CIRRUS_WAKE_CMD` | — | `command`: argv to run, no shell |
| `CIRRUS_TAILSCALE_PEER` | the server name | Split only: the server's Tailscale name, for awake/asleep. `-` to rely on the agent's reports only |
| `CIRRUS_POWER_CHECK_CMD` | — | Split only: a command that prints `online` or `offline` for the server, e.g. `sudo -n /usr/local/sbin/cirrusux-router-check` (TR-064). A second opinion beside Tailscale, the fallback when Tailscale can't be read, and a wake's "network up" step |
| `CIRRUS_POWER_CHECK_SECS` | `60` | How often the power check runs |
| `CIRRUS_CHANGELOG` | — | Also append every action to this Markdown file |
| `ASSISTANT_PROVIDER` | — (off) | `xai`, `openai`, `anthropic`, `ollama` or `custom` |
| `ASSISTANT_MODEL` | — | The model; the assistant is off without one |
| `ASSISTANT_API_KEY` | — | Needed for hosted providers |
| `ASSISTANT_BASE_URL` | the provider's | Any OpenAI-compatible chat API (`custom` needs it) |
| `ASSISTANT_NAME` | Grok for xai, Claude for anthropic, else Assistant | Name shown in the UI |
| `CIRRUS_ASSISTANT_DAILY_TOKENS` | `200000` | Daily token budget |
| `CIRRUS_DIGEST_ASSISTANT` | off | `1` lets the 08:00 digest ask the assistant for a summary |
| `CIRRUS_DEV` | off | `1` for development only: no sign-in, wakes are pretend |

## agent.env

| Setting | Default | What it does |
|---|---|---|
| `CIRRUS_GATEWAY_URL` | — (required) | The gateway's `/agent/snapshot` URL |
| `CIRRUS_AGENT_TOKEN` | — (required) | Same as the gateway's |
| `CIRRUS_DISK` | `/` | File system the disk widget shows; empty to skip |
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
| apps | `CONTAINER_RUNTIME`, `APPS_DIR`, `APPS_USER`, `MEDIA_ROOTS` (`APPS_ARCHIVE`, `APPS_BIND`) | The app store: installs and removals as background jobs |

Paths must be absolute; unit and jail names may only use letters, digits and
`- _ . @`. The helper refuses the file otherwise.

Today the sleep, backup and egress modules wrap scripts you already have.
CirrusUX's own versions of them are ROADMAP Phase 4.

## host.env

`cirrusux-setup` reads `/etc/cirrusux/host.env` (owner root, not writable by
anyone else). The example, `deploy/host/host.env.example`, lists every
setting; each module's file in `deploy/host/modules/` describes its own.

| Setting | Default | What it does |
|---|---|---|
| `HOST_MODULES` | `base` | Modules to run when none are named |
| `HOST_TZ` | — | base: time zone to set |
| `HOST_AUTO_UPDATES` | `no` | base: `no` switches off apt's daily upgrade timer |
| `CIRRUS_ROLE` | `single` | cirrusux: `single`, `server` or `gateway` |
| `CIRRUS_ACTIONS` | `yes` | cirrusux: `no` installs a read-only sudoers rule |
| `CIRRUS_SERVER_NAME`, `CIRRUS_ALLOWED_USERS` | — | cirrusux: written into a new gateway.env |
| `CIRRUS_RELEASE` | this checkout | cirrusux: folder holding `bin/` and `deploy/` |
| `FIREWALL_ALLOW` | — | firewall: networks allowed in (IPv4 and IPv6) |
| `FIREWALL_TAILSCALE` | `yes` | firewall: also allow the tailnet |
| `FIREWALL_PUBLIC_TCP` | — | firewall: ports open to everyone |
| `FIREWALL_CONFIRM_SECONDS` | `120` | firewall: time to confirm before rollback |
| `NET_WOL_IFACE` | — | network: wired interface to keep Wake-on-LAN on |
| `NET_TAILSCALE` | `yes` | network: check tailscaled starts at boot and is logged in |
| `BACKUP_AGE_RECIPIENT` | — (required) | backup: age public key(s); the private key stays elsewhere |
| `BACKUP_DIR` | `/srv/backups` | backup: where archives go |
| `BACKUP_PATHS` | `/etc /root /home /usr/local /var/lib/cirrusux /srv/cirrusux` | backup: what goes in |
| `BACKUP_EXCLUDE` | — | backup: what stays out |
| `BACKUP_SCHEDULE` | `*-*-* 03:30` | backup: systemd `OnCalendar`; the timer wakes a sleeping server |
| `BACKUP_KEEP` | `7` | backup: newest archives kept; `0` keeps all (the only automatic deletion) |
| `SEC_FAIL2BAN` | `yes` | security: fail2ban `sshd` jail, reading the journal |
| `SEC_AV` | `yes` | security: weekly ClamAV scan; signatures update just before it |
| `SEC_AV_PATHS` | `/home /srv /usr/local /tmp /var/tmp` | security: what the scan covers |
| `SEC_AV_SCHEDULE` | `Sun *-*-* 04:00` | security: when it runs |
| `APPS_RUNTIME` | `podman` | apps: `podman` (Quadlet units) or `docker` (compose) |
| `APPS_DIR` | `/srv/cirrusux/apps` | apps: each app's own folders |
| `APPS_ARCHIVE` | `/srv/cirrusux/archive` | apps: removed apps' folders, kept |
| `APPS_MEDIA_ROOTS` | `/srv/media` | apps: folders media may be chosen from |
| `APPS_BIND` | — (all) | apps: publish apps' ports on this address only |
| `SLEEP_IDLE_MIN` | `30` | sleep: quiet minutes before it suspends |
| `SLEEP_GRACE_MIN` | `15` | sleep: minutes after a wake before it may sleep again |
| `SLEEP_WATCH_PORTS` | — (all) | sleep: only these local ports count |
| `SLEEP_IGNORE_CLIENTS` | — | sleep: addresses or networks whose connections don't count |
| `SLEEP_MAX_LOAD` | `0` (off) | sleep: a 5-minute load above this counts as activity |
| `SLEEP_COUNT_LOGINS` | `yes` | sleep: logged-in sessions count |
| `SLEEP_WAKE_TIMERS` | backup and scan timers | sleep: timers that wake it, shown as the next wake |

The sleep module installs `cirrusux-idle`, CirrusUX's own sleep manager. A
timer runs `cirrusux-idle check` every minute: inbound connections (to the
machine, and through conntrack to containers' published ports), logins,
busy units (backups, scans, app jobs) and stay-awake holds count as
activity; outbound connections never do. It suspends after the quiet time,
and timers with `WakeSystem=yes` wake it. `cirrusux-idle status` shows what
it sees without changing anything, and `cirrusux-idle check --dry-run`
writes nothing at all. Anything it can't read counts as activity.

Modules that the panel shows (backup, virus scan, security) tell the helper
about themselves in `/etc/cirrusux/helper.d/`, so `helper.env` stays yours.
