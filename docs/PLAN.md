# CirrusUX — build plan

Written 1 Nov 2026. Based on `cloud64-server-brief.pdf` (1 Nov 2026, 20:30 GMT).
Section numbers like "brief §5.4" point into that brief. Status: **Phase 0.5 signed off. Phase 1 code built and tested locally; not installed yet (Phase 0 checks pending, see INSTALL.md).**

## 1. Goal

One web panel for cloud64 that:

- shows whether the server is awake or asleep, and wakes it, **without ever keeping it awake by accident**;
- replaces the parts of 1Panel and CasaOS that are actually used day to day (containers, updates, storage, backups, security, logs);
- includes a Grok assistant that can answer questions about the server and propose actions that you approve;
- follows the owner's rules in brief §9 in every feature (§8 below shows how each rule is met).

Non-goals: replacing 1Panel's firewall, FTP-account or OpenResty management; restoring backups (the private key never goes on the server); anything reachable from the internet.

## 2. Architecture

```
  phone / laptop / iPad (tailnet)
            │  HTTPS (Tailscale Serve, tailnet identity)
            ▼
┌───────────────────────── cloud-box (always on) ─────────────────────────┐
│  panel-gateway (Go, single binary, web UI embedded)                     │
│   • power state, Wake button, snapshot cache (SQLite)                   │
│   • Grok assistant (xAI API, tools, redaction)                          │
│   • audit log + CHANGELOG writer                                        │
└──────────────┬───────────────────────────────▲──────────────────────────┘
               │ actions, live views            │ snapshots (agent pushes,
               │ (only while you use it)        │  outbound from cloud64)
               ▼                                │
┌──────────────────────────── cloud64 (sleeps) ───────────────────────────┐
│  panel-agent (Go, unprivileged user `cirrusux`, tailnet IP only)        │
│   │ unix sockets only, never TCP loopback                               │
│   ├── panel-helper (root, fixed allow-list of subcommands, via sudoers) │
│   └── docker filter (root, socket-activated, unix socket, allow-list)   │
└─────────────────────────────────────────────────────────────────────────┘
```

Why this shape:

- **Gateway on cloud-box.** A UI hosted only on cloud64 can't say "asleep" or wake the server (brief §5.6, §11.1). cloud-box is always on, is on the tailnet, and already wakes cloud64 through the router (`~/bin/cloud64-wake`).
- **Agent pushes, gateway rarely pulls.** The agent sends a snapshot to the gateway every 5 minutes while awake, and when something changes. These are outbound connections from cloud64 on an ephemeral port, which the idle check does not count (brief §5.4 counts inbound connections to host and app ports). So you can browse the last-known state while the server sleeps, and the gateway only connects in when you act or open a live view (logs).
- **Unix sockets only on cloud64.** A TCP connection to 22/443/8080, the 1Panel port or into a container's published port counts as activity (brief §5.4 items 4–5). A `docker-socket-proxy` *container* reached over TCP would keep the server awake forever, so the Docker filter is a host service on a unix socket instead.
- **Go + Svelte.** Two static binaries, assets embedded, no CDN or runtime downloads (brief §11.5). The agent target is under 30 MB RSS and no CPU while idle (6 GB RAM is shared with Jellyfin).

## 3. Components

### 3.1 panel-gateway (cloud-box)

| Area | Detail |
|---|---|
| Serving | Listens on 127.0.0.1 only. Published to the tailnet with `tailscale serve` (HTTPS with the `*.ts.net` cert). Never port-forwarded. |
| Login | Tailscale Serve identity headers (`Tailscale-User-Login`), matched against an allow-list in the config. No passwords. Destructive actions need re-confirmation in the UI (typed confirmation for anything irreversible). Session cookies: HttpOnly, Secure, SameSite=Strict; CSRF tokens; rate limit. |
| Power state | Primary: the cloud64 peer `Online` / `LastSeen` from the local tailscaled API (tailscaled on cloud64 is stopped during sleep, brief §5.5). Confirmed by the age of the last snapshot. Phase 2: router `GetSpecificHostEntry` as a second opinion. States shown: Awake, Asleep, Waking (≤ 6 min), Unreachable (not asleep, but not answering). |
| Wake | Runs the existing `~/bin/cloud64-wake` through `cirrusux-wake` (one sudoers rule, no arguments; the router password stays in a root-only file). The UI shows progress until the agent's first snapshot after resume (~20–30 s). |
| Cache | SQLite: latest snapshot per area, wake/suspend history, audit log, Grok conversations (redacted). |
| Changelog | Every action is appended to `/workspace/cloud64/CHANGELOG.md` (the existing file, with a lock) **and** to the gateway's audit table: UK time, who, what, backup path, undo command. |
| Secrets | `/etc/cirrusux/gateway.env` (600): xAI API key, agent token. Shown in the UI only as `set` / `not set`. |

### 3.2 panel-agent (cloud64)

- Runs as a new system user `cirrusux` (no shell, no login), member of nothing privileged.
- Listens on the **tailnet IP only** (`100.64.0.10`), on a new port (proposed 47064). It accepts only requests from the cloud-box node, checked with `tailscale whois` on the peer address, plus a shared token.
- This port is **not** added to `HOST_PORTS` in `cloud64-idle.conf`. Viewing the panel never holds the server awake; the Stay-awake control does that on purpose (brief §11.2 option (b)).
- Reads directly, without root: `/etc/cloud64-idle.conf`, `/var/log/cloud64-idle.log`, `/sys/class/thermal`, `/sys/power/mem_sleep`, systemd over D-Bus (timers, unit state), `tailscale status --json` (local socket).
- Everything else goes through the helper or the Docker filter.
- Pushes snapshots to the gateway over the tailnet (HTTPS, token).

### 3.3 panel-helper (cloud64, root)

A single binary called with `sudo /usr/local/sbin/cirrusux-helper <subcommand>`. The sudoers entry lists the subcommands literally; arguments are validated against fixed lists (container names, unit names) inside the helper. No generic shell, no paths from the caller.

| Subcommand | Does | Phase |
|---|---|---|
| `idle-status` | `cloud64-idle-check --dry-run` + `/run/cloud64-idle/*` → JSON | 1 |
| `stay-awake <min\|forever>` / `allow-sleep` | runs the existing `stay-awake` / `allow-sleep` | 1 |
| `backup-list` | names, sizes, encrypted yes/no, ages under `/mnt/media/BackUp/cloud64-system/` | 1 |
| `journal <unit>` | last N lines for allow-listed units (`cloud64-backup`, `cloud64-idle`, …) | 1 |
| `security-status` | fail2ban `sshd` jail, LOCKDOWN-PUBLISHED and 1PANEL_* chains present, sshd restriction file present | 2 |
| `backup-run` | `systemctl start cloud64-backup.service` | 2 |
| `egress-report` | `egress-report.sh -n 20` | 2 |
| `compose-backup <app>` / `compose-set-tag <app> <tag>` / `compose-up <app>` / `compose-restore <app> <bak>` | the update workflow (§3.5) | 3 |
| `apt-check` | `apt-get update` + `apt list --upgradable` (only on request: it contacts the mirrors) | 3 |
| `archive-list` / `archive-approve-delete <date>` | archive review (two steps) | 3 |

### 3.4 Docker filter (cloud64, root)

A small socket-activated service: `/run/cirrusux/docker.sock` (group `cirrusux`, 660) → `/var/run/docker.sock`. Allows only `GET` list/inspect/logs/stats and `POST` start/stop/restart on the known container names. Everything else (create, exec, pull, volumes, networks) returns 403. Pulls happen only in the helper's update workflow.

### 3.5 App updates (phase 3)

Follows brief §3.2 and §11.3 item 3. **The panel never upgrades App-Store apps through 1Panel**, because that rewrites `docker-compose.yml` and drops the media mounts, TZ, `/dev/dri` and `group_add`.

1. The agent checks registry tags for each pinned image, only when you ask (outbound check; rule 5).
2. You pick a version. The UI shows the diff of the compose file (tag line only).
3. Helper: back up the compose file (`.bak-YYYYMMDD-HHMM`), edit the tag, `docker compose pull && up -d`.
4. Health check (container healthcheck or HTTP probe) for up to 2 minutes.
5. Write the CHANGELOG entry with the undo command (`compose-restore <app> <bak>`). If the health check fails, offer the restore at once.

### 3.6 Grok assistant (gateway)

- xAI API (OpenAI-compatible chat + tool calling). The model name lives in the config; nothing is hard-coded.
- Runs on the gateway, so questions are answered from the cache **without waking the server**. A tool that needs live data says so ("cloud64 is asleep — wake it?") instead of waking it on its own.
- **Read tools (run freely):** `power_status`, `awake_reasons`, `wake_history`, `containers`, `container_logs_summary`, `backups`, `timers`, `av_last_scan`, `egress_summary`, `disk_usage`, `pending_updates`, `changelog_recent`.
- **Action tools (proposal only):** `wake`, `stay_awake`, `allow_sleep`, `restart_container`, `run_backup`, `propose_update`. Grok returns a proposal; the UI shows a confirm card; your click runs it through the normal action path (same audit and changelog as a button).
- **Never available to Grok:** a shell, file reads, `.env` or any secrets, deletion, 1Panel, firewall or SSH changes.
- **Redaction** on every tool result before it leaves for xAI: secrets patterns, MACs, public IPs, tokens, file contents. Tailnet and LAN IPs and paths are masked too under the default policy (decision D2).
- Spending cap set in the xAI console; per-day token budget enforced in the gateway as well.
- Phase 2 extras: "Explain this" on any log line; a daily digest written on cloud-box from the cache (no wake needed).

## 4. Web UI

The UI is a first-class part of the project, not a skin over an API. It gets its own design phase (Phase 0.5) and its own done criteria in every phase.

### 4.1 Design principles

1. **State first.** Awake / asleep / waking is the most important fact. It is the hero of every page, readable from across the room on a phone.
2. **Calm by default, loud when it matters.** Neutral surfaces and one accent colour; colour is reserved for state (healthy, warning, failed). Nothing blinks unless something needs you.
3. **Glanceable, then deep.** Each widget answers one question at a glance ("did the backup work?"). Clicking opens the detail.
4. **Asleep is a designed state, not an error.** When cloud64 sleeps, the whole UI shifts to a dimmed "night" look. Every number keeps its last-known value with an "as of 18:08 GMT" label, and the Wake button becomes the hero. No spinners, no red error screens.
5. **Honest motion.** Short transitions that explain change (a container going from stopping to stopped, the waking progress). Everything respects `prefers-reduced-motion`.
6. **Safe-looking actions.** Destructive actions look different (outlined, never primary), and confirmation cards show exactly what will run and its undo.

### 4.2 Design system

- **Tokens** for colour, type, spacing, radius, shadow, blur and motion, defined once as CSS custom properties. Every component uses tokens only — no hard-coded colours — so a theme is just a set of token values.
- **Themes** ship in one file each (`themes/*.toml`). Built in at launch:
  - *Graphite* (dark, default), *Paper* (light), *Midnight* (true black, for OLED phones at night), *Dusk* (warm low-contrast dark).
  - Follow-system mode switches between a chosen light and dark theme.
- **Accent colour** picker with a live contrast check. If a colour fails WCAG AA against the theme, the panel adjusts its lightness and says so, instead of shipping unreadable text.
- **Type:** a bundled variable sans (e.g. Inter) and mono (e.g. JetBrains Mono), both OFL-licensed and served locally (no Google Fonts; rule 5). Tabular numerals for all figures so values don't jitter while updating.
- **Icons:** one consistent outline icon set (e.g. Lucide, ISC licence) bundled. App tiles use a local copy of app logos, and you can upload your own.
- **Components:** Svelte 5 components on accessible headless primitives (focus handling, menus, dialogs), styled only through tokens. A component gallery page (`/dev/components`) shows every component in every theme, used for review and screenshot tests.
- **Charts:** a small, fast library (e.g. uPlot) for sparklines and timelines, themed from the same tokens.
- **Status is never colour alone:** every state has an icon and a word as well (colour-blind safe).

### 4.3 Layout and dashboards

- **Dashboard grid** of widgets: drag to move, drag the corner to resize, add and remove from a widget picker. Snap-to-grid with sensible minimum sizes.
- **Several boards**, e.g. "Home" (power, apps, backups), "Media", "Maintenance". Each board has its own name, icon and optional wallpaper (solid, gradient or an uploaded image with automatic blur and dim behind cards for readability).
- **Per-device layouts:** separate saved layouts for phone, tablet and desktop, so the phone can be a single column with Power and Wake on top.
- **App launcher tiles** (the CasaOS strength): logo, name, live status dot, group/folder, custom colour; opens the app on the right host (LAN IP or MagicDNS) depending on where you are.
- **Navigation:** sidebar on desktop, bottom bar on phone, and a command palette (Ctrl/⌘-K) that jumps to any page, container or action — and doubles as the entry point to Grok ("ask Grok…").
- **Density:** comfortable / compact, plus a global text-size setting.

### 4.4 Widget catalogue

| Widget | Shows |
|---|---|
| Power hero | State, uptime or "asleep since", sleep countdown, Stay-awake and Wake |
| Sleep timeline | Today's awake/asleep bands with wake reasons (backup, TV streamer, manual) |
| App tiles | One or a group of apps, with status |
| Containers | Compact list with health and restart |
| Backups | Last run, size, encrypted, next run |
| Storage | Media disk bar per folder, free space, USB link warning |
| Temperatures | Sparklines from the cached snapshots |
| Security | fail2ban, ClamAV, lockdown integrity in one line each |
| Changelog feed | Last N actions with undo links |
| Grok quick ask | One-line prompt with suggested questions |
| Link / note | A custom link, or a short Markdown note |

App UIs are **not** embedded in iframes: each embed would hold a connection into the app's container, which counts as activity and stops the server sleeping.

### 4.5 Customisation levels

1. **Settings screen:** theme, accent, density, text size, wallpaper, time format (always UK time zone).
2. **Layout editing:** boards, widgets, tiles, groups — directly on the dashboard with an Edit toggle.
3. **Theme editor:** edit any token with a live preview, then save as a new theme; import and export as a file.
4. **Custom CSS** for anything the tokens don't cover. The strict content security policy still blocks external URLs, so custom CSS can't load remote assets or leak data.
5. **Declarative custom widgets:** a small file describing a data source (one of the panel's existing read endpoints) and a layout (stat, list, sparkline). No custom JavaScript, so a widget can't run code or reach anything the panel doesn't already expose.

All of this is stored **on the gateway** (SQLite + an exportable `ui.toml`), so it follows you across devices, never sits in browser localStorage, and every save keeps a version you can roll back to.

### 4.6 Quality bar

- **Accessibility:** WCAG 2.2 AA; full keyboard use; visible focus; screen-reader labels on every control.
- **Performance budget:** first load under 150 KB of compressed JavaScript; interactive in under 1 s on the tailnet; 60 fps drag and resize; no layout shift when live data arrives.
- **PWA:** installable on phone and desktop (manifest + offline app shell), so it opens like an app and shows the cached "asleep" state even with a weak connection. No push notifications in v1 (they need a third-party push service).
- **Visual regression:** Playwright screenshots of every page and the component gallery in every built-in theme, at phone and desktop sizes, compared on each build.

### 4.7 Pages

Pages, in build order:

1. **Power** — big state badge, Wake button, "Sleeps in ~X min" plus the current reasons it's awake, Stay-awake (30 min / 2 h / until I say) and Allow-sleep, next RTC wake (backup / ClamAV), last suspend and resume with wake IRQ, today's sleep timeline.
2. **Services** — containers with pinned tag, state, health, ports, uptime; Start/Stop/Restart with confirmation; links to each app opening on the right host (LAN IP or MagicDNS, depending on where you are).
3. **Backups** — list, last run, next run, Run-now; restore instructions only.
4. **Storage** — `/mnt/media` per folder (cached, refreshed rarely — `du` over USB 2 is slow and counts as activity), USB link-speed warning, temperatures; SMART/btrfs once the tools are installed (brief §10 item 12).
5. **Security** — fail2ban, ClamAV last scan, firewall/lockdown integrity, egress top destinations.
6. **Updates** — image tags and apt upgrades with the approval workflow.
7. **Media** — library counts, trigger scans, archive review, AVI re-encode queue (later).
8. **Logs**, **Changelog**, and a **Grok** side panel available on every page.

Behaviour rules: polling only while the tab is visible, at most once a minute, with back-off; no WebSockets or SSE to cloud64 (the browser talks only to the gateway); live log tails time out after 5 minutes. All times shown in Europe/London with GMT/BST. Works offline on the tailnet (no fonts or scripts from the internet).

## 5. Repository layout

```
cirrusux/
  cmd/gateway/   cmd/agent/   cmd/helper/   cmd/dockerfilter/
  internal/      power, snapshot, docker, systemd, grok, redact, audit, changelog
  web/           SvelteKit (static adapter), built into cmd/gateway via embed
  deploy/
    cloud64/     systemd units, sudoers.d/cirrusux, install.sh, uninstall.sh
    cloud-box/   systemd unit, serve config, install.sh
  docs/          PLAN.md, SECURITY.md, ACTIONS.md (every action + its undo)
```

Pinned versions everywhere (Go toolchain, Node for the build only, every npm package with a lockfile). Reproducible build with `make`. Install scripts back up anything they touch and print the undo.

## 6. Phases

### Phase 0 — prerequisites (no code on the server yet)
- Confirm decisions D1–D5 (§9).
- Verify on cloud64, read-only: does tailnet traffic to a new port pass `ts-input` / `1PANEL_BASIC_AFTER`, or does 1Panel's firewall need a tailnet-only rule? (The brief lists only UDP 41641 in `ts-input`.)
- Check that cloud-box's userspace tailscaled works with `tailscale serve` (brief §10 item 19 notes that it sometimes loses the coordination server).
- **Done when:** the decisions are recorded here and the firewall answer is known.

### Phase 0.5 — design prototype (no server access needed)
A clickable prototype with fake data: the design tokens, the four built-in themes, the component gallery, the Home board in awake, waking and asleep states, the phone layout, layout editing, and the theme editor.
- **Done when:** you have picked the visual direction and default theme, and signed off the Home board on phone and desktop. Phase 1 builds on these components rather than redrawing them.

### Phase 1 — read-only panel
Built (1 Nov 2026), with three changes from the original plan:
- **No listening port on cloud64 yet.** The agent only pushes; Phase 1 needs nothing from the gateway that would require connecting in. The agent port (D5) and its firewall question move to Phase 2.
- **JSON files instead of SQLite** on the gateway (snapshot, versioned UI settings, audit log). Enough for one household and keeps the Go code dependency-free.
- **Log parsers checked against real output** (INSTALL.md §0, 1 Nov). The agent reads the idle checker's state files rather than running `--dry-run`, which turned out not to be write-free.
- **Wake goes through `cirrusux-wake`**, a root wrapper that reads the router password from a root-only file and runs the existing `cloud64-wake` as the admin user. The password was only in the admin bot's process environment, which `sudo -u` can't reach.
Gateway (login, Power page, Wake, cache), agent (snapshots), helper subcommands marked 1, Docker filter (read-only verbs), Services and Backups pages (view only), Grok with read tools only.
Includes from the UI work: themes, boards with drag-and-drop layout, app tiles, command palette, PWA install, settings screen (customisation levels 1–2). PWA added 1 Nov: installable (Chrome reports no installability errors) with an offline shell that shows the last saved data, clearly marked as offline.
- **Done when:** with the server asleep, the panel shows the designed "Asleep" state, last-known containers and backups, and Grok answers "did last night's backup work?" without waking it. Wake brings it up and the panel goes green within ~60 s. Over 24 h of normal use, `cloud64-idle.log` shows no extra wakes and no suspend delayed by the panel.

### Phase 2 — actions with confirmation
Stay-awake/Allow-sleep, start/stop/restart containers, Run backup now, Security page, CHANGELOG writing, Grok action proposals, TR-064 power check, "Explain this", daily digest. UI: theme editor and custom CSS (customisation levels 3–4).
- **Done when:** every action leaves an audit row and a CHANGELOG entry with a working undo, and Grok can't run anything without a click.

### Phase 3 — maintenance
Update workflow (images, then apt), Storage page, Media helpers, archive review with two-step delete, Logs page. UI: declarative custom widgets (customisation level 5).
- **Done when:** one image update (e.g. Navidrome) goes through backup → tag → pull → health → changelog, and the restore path is tested once.

### Later
WoL proxy on an always-on LAN device (brief §5.6); offsite backup status; AVI re-encode queue.

## 7. Testing

- Unit tests for the redactor (it must never let a secret pattern through), helper argument validation, and the Docker filter allow-list.
- A fake agent and a fake helper for UI work, so most development needs no access to cloud64.
- Visual regression and accessibility checks (§4.6) run on every build: screenshots per page, theme and screen size, plus an automated a11y scan.
- On cloud64: `cloud64-idle-check --dry-run` before and after each install to confirm the panel adds no activity reasons. Watch `cloud64-idle.log` for 24 h after each phase.

## 8. How the owner's rules (brief §9) are met

| # | Rule | How |
|---|---|---|
| 1 | No plain-text secrets | Secrets in 600 env files; UI shows `set`/`not set`; redactor in front of Grok and logs; nothing in URLs or localStorage |
| 2 | Confirm destructive actions; archive, don't delete | Confirm cards; deletion only in archive review, two steps |
| 3 | Back up configs, log every change with undo | Helper writes `.bak-YYYYMMDD-HHMM` first; CHANGELOG + audit table |
| 4 | Pinned image versions | Update workflow only sets explicit tags; `:latest` is rejected |
| 5 | No telemetry, no auto-updates | Update checks only on request; Grok calls only when you use it (D2) |
| 6 | Low energy | Push model, unix sockets, port not in `HOST_PORTS`, visible-tab polling only |
| 7 | LAN + tailnet only | Gateway on tailnet via Serve; agent on the tailnet IP only |
| 8 | UK time | Europe/London everywhere with GMT/BST label |
| 9 | Leave Trakt as is | Panel never touches Jellyfin plugins |
| 10 | Keep `cloud` SSH password login | Panel never changes sshd |
| 11 | Media read-only in players | Panel never edits app mounts except restoring them from `.bak` |
| 12 | Backups exclude media; key never on server | No restore in UI; only instructions |
| 13 | No Proton Pass | Not used or installed |

## 9. Decisions needed

| # | Decision | Proposed default |
|---|---|---|
| D1 | Where the gateway runs | cloud-box |
| D2 | What Grok may send to xAI | Summaries only, with IPs/MACs/paths masked; raw logs only when you ask for one |
| D3 | Does acting in the panel extend wake time? | Yes: any action sets Stay-awake for 10 min; viewing does not |
| D4 | Who may log in | Only your Tailscale login |
| D5 | Agent port | 47064, tailnet IP only |
| D6 | Visual direction and default theme | Decided from the Phase 0.5 prototype; *Graphite* (dark) proposed |
| D7 | Wallpapers and uploaded logos | Allowed, stored on the gateway, size-limited (5 MB each) |

## 10. Risks

- **Calls to app APIs count as activity.** Jellyfin/Navidrome/ABS/Kavita API calls open TCP into the container's published port (brief §5.4 item 5). Keep them on-demand and short, and cache the results.
- **cloud-box tailscaled drops out** (brief §10 item 19; seen again 1 Nov): handled. The gateway checks its own tailnet link, falls back to snapshot age and shows a notice; the agent retries after a minute. tailscaled there was also started by hand, not as a service: make it one before installing (INSTALL.md §0).
- **1Panel firewall rules may be rewritten by 1Panel.** Any rule added for the agent port must be added through 1Panel itself, or re-checked like the lockdown timer.
- **Prompt injection through logs.** Log text sent to Grok may contain instructions. Mitigated by proposal-only actions and confirm cards; Grok output is never executed.
