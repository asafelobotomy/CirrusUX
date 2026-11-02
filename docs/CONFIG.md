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
| `CIRRUS_IDLE_CONF` | — | Sleep module: the idle checker's config (`IDLE_MIN`, `GRACE_MIN`, `WAKE_TIMERS`) |
| `CIRRUS_PORT_NAMES` | — | Names for port numbers in activity reasons, e.g. `8080=Proxy,9000=Admin` |

## helper.env and modules

The helper runs as root, so it takes no paths from the agent: they come from
this file, which must belong to root and be writable only by root. Each
module is on when its settings are set. Without any, the panel still shows
containers, disk and temperature, and hides what the server doesn't have.

| Module | Settings | Adds |
|---|---|---|
| sleep | `IDLE_STATE_DIR`, `IDLE_LOG` | Sleep countdown, activity reasons, the sleep timeline |
| stay-awake | `STAY_AWAKE_FILE`, `STAY_AWAKE_BIN`, `ALLOW_SLEEP_BIN` | Stay awake / Allow sleep; actions keep the server up 10 minutes (D3) |
| backup | `BACKUP_UNIT`, `BACKUP_DIR`, `BACKUP_PREFIX` | Backups page and widget, Run backup now |
| av | `AV_LOG` | Virus-scan result |
| security | `FAIL2BAN_JAIL`, `FIREWALL_CHECKS` | fail2ban bans; firewall rule checks (`CHAIN:pattern`, passes when `iptables -S CHAIN` contains the pattern) |
| egress | `EGRESS_REPORT` | The outbound-traffic report on the Security page |

Paths must be absolute; unit and jail names may only use letters, digits and
`- _ . @`. The helper refuses the file otherwise.

Today the sleep, backup and egress modules wrap scripts you already have.
CirrusUX's own versions of them are ROADMAP Phase 4.
