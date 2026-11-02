# Changelog

All notable changes to CirrusUX. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, each plan phase
gets a minor version.

## [Unreleased]

### Added

- `cirrusux-setup` (roadmap Phase 4, first part): re-runnable host modules
  with a check mode that reports what apply would change. Every change is
  logged with its undo and replaced files keep a dated copy.
  - base: the cirrusux user, time zone, no automatic updates.
  - firewall: an nftables table of its own, LAN and tailnet only, closing
    containers' published ports to everything else. A new ruleset rolls
    back by itself unless confirmed, setup refuses one that would cut off
    the SSH session running it, and every ruleset is kept.
  - cirrusux: binaries, units, a visudo-checked sudoers rule and config
    files (created once, with a shared token in the single setup); services
    start once their config has no placeholders left.
- More `cirrusux-setup` modules (Phase 4b):
  - network: Wake-on-LAN kept on across reboots (a `.link` file that keeps
    the card's name), tailscaled started at boot, login reported.
  - backup: `cirrusux-backup`, tar + zstd encrypted with age to a public
    key, written as `.partial` and renamed only when complete; a timer that
    wakes a sleeping server; `BACKUP_KEEP` retention (0 keeps all).
  - security: fail2ban's `sshd` jail on the journal, a weekly ClamAV scan
    that updates its signatures just before it runs (no daemon).
- `cirrusux-idle`, CirrusUX's own sleep manager (Phase 4c), and the `sleep`
  setup module that installs it on a split setup's server. Once a minute it
  counts inbound connections (and, through conntrack, connections to
  containers' published ports), logins, busy units and stay-awake holds;
  outbound connections never count. It suspends after the quiet time with
  a grace period after each wake, records wakes through a systemd sleep
  hook, and writes the state files and log the agent already reads.
  Anything it can't read counts as activity, and `--dry-run` writes
  nothing. It also answers as `cirrusux-stay-awake` and
  `cirrusux-allow-sleep` for the panel's stay-awake buttons.
- The agent splits activity reasons on `; ` too, and reads
  `/etc/cirrusux/sleep.env` by default.
- The helper reads `/etc/cirrusux/helper.d/*.env` after `helper.env`, so
  setup modules switch panel modules on without touching the owner's file.
- `docs/PHASES-4-6.md`: the build plan for phases 4–6.
- `make test-host`: shellcheck plus each module tested as root in a private
  namespace, never on the real machine.
- The helper can check nftables chains (`family/table/chain:pattern`).
- `capture.sh` in the cloud64 profile: a read-only copy of cloud64's own
  scripts and settings before the reinstall.

### Changed

- The power card in the single setup shows the server's vitals (containers
  running, disk, CPU temperature, last report) instead of standing empty.
- The assistant's starter questions follow the server's modules and setup.
- Test fixtures use generic names instead of cloud64's.

### Fixed

- App tiles cut a four-letter suffix off any container name (`cirrus-test`
  became "Cirrus"); only 1Panel's own names get that treatment now.

### Tested

- The single setup on a second machine with real data and Podman's
  Docker-compatible API behind the filter: containers, disk, temperature
  and a confirmed restart all work, and the filter refuses a delete.

## [0.3.0] - 2026-11-02

Fixes from the full code review of 0.2.0, a new roadmap, and its Phase 3:
CirrusUX now works for any server, not just cloud64.

### Changed (Phase 3: generalise the core)

- Everything that named cloud64 or cloud-box now comes from config: server
  and gateway names, the host apps open on, the time zone (shown with its
  own abbreviation) and a description for the assistant. See docs/CONFIG.md.
- Two setups: `single` (everything on a server that doesn't sleep) and
  `split` (a sleeping server beside an always-on gateway, as before).
  `CIRRUS_SETUP` defaults to `single`.
- Wake methods: Wake-on-LAN sent by the gateway itself (`CIRRUS_WAKE=wol`)
  or a command (`command`, as before).
- Sign-in: Tailscale as before, or a trusted header from a reverse proxy
  (`CIRRUS_AUTH=header`). The identity header is now only believed from
  `CIRRUS_TRUSTED_PROXIES`.
- Grok is now "the assistant", with presets for xAI, OpenAI, Anthropic and
  Ollama (`ASSISTANT_*` settings replace `XAI_*`; the digest switch is now
  `CIRRUS_DIGEST_ASSISTANT`). The API moved to `/api/assistant`; saved boards
  with the old Grok widget carry over.
- The root helper reads its paths from `/etc/cirrusux/helper.env` (root-owned)
  instead of built-in cloud64 paths, and reports which modules the server
  has: sleep, stay-awake, backup, virus scan, security, egress. The panel
  hides pages and widgets for modules a server doesn't have.
- Firewall checks are a configurable list instead of the two cloud64
  chains; the backup unit, idle files and port names are configurable too.
- App tiles come from the containers that publish a port, instead of a fixed
  list.
- `deploy/` is split into `server/`, `gateway/` and the owner's private
  `profiles/cloud64/`; docs/INSTALL.md is now a general guide.

### Fixed

- The agent leaked a Docker connection on every collection: it built a new
  HTTP client per call, and the Docker filter never closed idle connections.
  It now reuses one client (idle connections close after 30 s), and the
  filter closes connections idle for 60 s.
- An action whose result report failed once was never logged. The agent now
  retries the report for about a minute.
- The egress report could be cut off by the helper's 30 s limit; it now gets
  150 s. The gateway waits 4 minutes for a result, and the UI waits 5.
- The gateway exited without waiting for requests in flight to finish, and
  the agent's long poll could hold up a shutdown.
- A wake is logged as "Sent a wake to cloud64" when asked for, plus a
  "(failed)" entry with the error if the wake command fails, instead of
  "Woke cloud64" whether or not it worked.
- Grok's disk sizes are no longer rounded down to whole GB.
- Settings changed while the gateway is unreachable are kept and saved when
  it's back (retried every 30 s and when the browser comes online), instead
  of being lost without a word. The service worker also updates its saved
  copy of the settings after each save, so a reload while offline no longer
  starts from an older version whose first save is then refused as a
  conflict (found while testing this fix).
- The accent contrast fix could push a colour the wrong way on mid-tone
  backgrounds (possible in a custom theme) and never reach 4.5:1. It now
  heads towards whichever of white or black can contrast more. Found by
  the new contrast tests.
- Settings → Layouts said "in the real panel" in live mode too.
- Failed Grok replies are no longer sent back to Grok as its own answers.
- The digest and the wallpaper now work offline.

### Changed

- The 08:00 digest only asks Grok for a summary when `CIRRUS_DIGEST_GROK=1`.
  By default Grok is called only when you use it (PLAN §8 rule 5).
- Wallpaper images are stored on the gateway on their own
  (`GET`/`PUT /api/wallpaper`: PNG, JPEG, WebP, GIF or AVIF up to 5 MB,
  checked from the bytes; no SVG), not inline in the settings, which every
  change re-sent and the 20 kept versions each copied. Demo mode says the
  image isn't kept after a reload.
- The audit log keeps its last 200 entries in memory instead of re-reading
  the whole file for every request. Past 5 MB, `audit.jsonl` is archived as
  `audit-YYYYMMDD-HHMMSS.jsonl` (never deleted).
- App tiles come from `web/src/lib/apps.ts`, matched to containers by app
  name; `lib/fake` is now `lib/demo` and holds only demo data.
- The CSP comes from the gateway's header only. The dev server sends its own,
  with the websocket hot reload needs; the copy in `index.html` is gone.
- PLAN, README, INSTALL and the Docker filter units no longer describe the
  helper and filter as read-only or tell you to run
  `cloud64-idle-check --dry-run`. INSTALL has upgrade steps from 0.2.0.

### Added

- `docs/ROADMAP.md`: CirrusUX becomes a general panel for any server, with
  host setup modules, an app store for Docker and Podman, and a public
  release; cloud64 becomes a private profile and is reinstalled from scratch.
- Tests: Docker client reuse, result retries, audit memory and rotation,
  UI-history pruning, the wallpaper endpoint, the helper's pure functions,
  the digest opt-in, and (with Node's built-in runner, no new dependencies)
  contrast maths and container-to-app names. `make test` runs them all.

### Known

- Dependabot reports a low-severity advisory in `deepmerge`, pulled in at
  build time only by `@sveltejs/vite-plugin-svelte`. No fixed version exists
  yet; nothing from it ships in the UI.

## [0.2.0] - 2026-11-01

### Added
- Phase 2 actions, each confirmed in the panel and logged with who asked:
  stay awake (30 min, 2 h, until allowed to sleep), allow sleep, start,
  stop and restart a container, run a backup now.
- Command channel: the agent fetches actions over an outbound long poll,
  checks them against its own list and reports results; cloud64 still opens
  no port.
- Undo on changelog entries that have a safe inverse.
- Security page: ClamAV, firewall lockdown, fail2ban, and an on-demand
  outbound-traffic (egress) report.
- Daily digest at 08:00 UK from cached data, with a Grok summary when
  configured, and a Daily digest widget.
- Grok can propose any of the actions; the gateway drops proposals that
  couldn't run, and you confirm the rest.

### Changed
- Container actions and backups keep cloud64 awake for at least 10 minutes
  (decision D3).
- The sudoers rule is split into read and action parts; remove the action
  part to keep the panel read-only.

### Fixed
- The UI dropped the body of 202 replies, which actions use for their ID.
- The settings-conflict reply was sent without its JSON content type.

## [0.1.1] - 2026-11-01

### Added
- Installable app (PWA): web manifest, app icons (regular, maskable and
  Apple touch, rendered by `scripts/make-icons.sh`) and a service worker.
- Offline mode: the panel opens without a connection and shows the last data
  the gateway sent, with an "Offline · saved data" badge, a dated notice on
  the power card and "as of" labels. No countdown and no wake while offline.
  Only reads are saved; anything that changes state always goes to the
  network.

## [0.1.0] - 2026-11-01

### Added
- Phase 0.5 design prototype: Svelte 5 UI with four themes, a theme editor
  with contrast checks, editable boards with per-device layouts, a widget
  catalogue, command palette, Grok panel and a component gallery.
- Phase 1 read-only panel:
  - gateway (cloud-box): serves the UI, caches agent snapshots, tracks
    power state from Tailscale, wakes cloud64, stores UI settings with
    versions, keeps an audit log, and runs Grok over cached data with
    redaction and a daily token cap;
  - agent (cloud64): pushes snapshots, opens no listening port;
  - root helper and Docker filter with fixed, read-only access;
  - systemd units, sudoers rules and example env files.
- Live mode in the UI, with demo data when no gateway answers.
- Versioning: `VERSION` file, `make bump`, versions stamped into every
  binary (`-version`), reported by the agent and gateway, and shown in
  Settings → About with a mismatch warning.

### Changed
- Project renamed from cloud64-panel to CirrusUX.
- Parsers fitted to the real cloud64 log formats (INSTALL.md §0).
- The agent reads the idle checker's state files; it never runs
  `cloud64-idle-check --dry-run`, which writes state.
- Waking goes through `cirrusux-wake`, which reads the router password from
  a root-only file.

### Fixed
- The gateway no longer reports cloud64 as asleep while cloud-box itself is
  offline from Tailscale, and the agent retries failed pushes after a minute.
