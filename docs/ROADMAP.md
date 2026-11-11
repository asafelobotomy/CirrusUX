# CirrusUX roadmap and status

This is the one place that says where each phase stands. The original design
is kept for history in [history/PLAN.md](history/PLAN.md), and how phases 4–6
were built is in [history/PHASES-4-6.md](history/PHASES-4-6.md).

CirrusUX began as a panel for one server and is now a general self-hosting
panel for any machine, published as a public repository.

- **Any server, not one.** Everything is written for any host from the start.
- **Private setups stay private.** A server's own names, paths and wake method
  live in a profile kept out of the public repo (`deploy/profiles/`).
- **No migration from other panels.** A server is reinstalled from scratch
  with CirrusUX's own tooling, and its apps start clean from the CirrusUX app
  store (D8).

What carries forward:

- **The UI layout as a foundation.** The shell (sidebar, top bar, command
  palette, assistant panel), the boards with their widgets, the themes and the
  design tokens are the starting point. When a change affects saved boards or
  settings, they are migrated rather than reset.
- **The safety rules.** The design rules from the first phases are
  CirrusUX's defaults for everyone (§3).

## 1. Status

| Phase | What | Status |
|---|---|---|
| 0–2 | Plan, read-only panel, actions (one server) | Done (v0.2.0) |
| 3 | Generalise the core: runs from a config file alone, in both setups | Done (v0.3.0) |
| 4 | Host modules: a clean Debian VM goes to a sleeping, backed-up, firewalled server by re-runnable scripts | Done |
| 5 | App store: install, update and remove (archive) apps on Docker and Podman | Done |
| 6 | Maintenance: app and OS updates, logs, storage and archive review, router power check, custom widgets | Done |
| 7 | Public release: licence, docs, CI, project files | Done |
| Git | Git hosting in the gateway ([GIT.md](GIT.md)) | Done |
| Releases | Binaries with checksums, an install script, upgrade and rollback | Done: bundles, `install.sh`, `upgrade` and `rollback` since v0.7.0; v0.7.1 is the first published release. `.deb` packages moved to after 1.0 |
| Clean first commit | The public repository starts from a scrubbed tree | Done (9 November 2026); the repository is public |
| Official catalogue | CirrusStore's signed catalogue at `asafelobotomy.github.io/cirrusstore/` | Done (11 November 2026): 19 apps (the 11 built in, and a Downloads group), each installed, opened and removed through a panel on a Debian 13 test VM first; the signed site verified, and a 0.7.1 panel installed an app from it |
| 8 | Reinstall a real server on the public tooling | Private; its outcome gates 1.0 (§5) |
| 1.0 | The first stable release | Not started; what is left is in §5 |

Later, not started: `.deb` packages, built-in accounts (D11), a wake-on-LAN
proxy on an always-on LAN device, offsite backup status, an AVI re-encode
queue, and multi-container apps (for example Immich).

## 2. Phases

### Phase 3 — generalise the core

Done in v0.3.0, and tried on a second machine (a workstation with Podman,
single setup, real data). Settings are in [CONFIG.md](CONFIG.md).

What was tied to one server, and what replaced it:

| Was tied to one server | Became |
|---|---|
| Fixed server and gateway names in the UI, assistant prompt and units | A server name and settings from the env files (D12, CONFIG.md) |
| Two machines only: an always-on gateway and a server that sleeps | Two setups. **Single host:** gateway and agent on one machine that doesn't sleep (the common case). **Split:** a sleeping server and an always-on gateway |
| Scripts at fixed paths in the helper | Helper modules with a defined interface (sleep, backups, security), so the paths come from the module, not the code |
| Wake through the router and a private script | Wake drivers: a magic packet sent by the gateway on the LAN (default), the router's TR-064, or a custom command |
| Sign-in only through Tailscale's login header | Sign-in providers (D11): Tailscale, or a trusted header from a reverse proxy (Authelia, Authentik, oauth2-proxy). Built-in accounts are not built |
| UK time, LAN + Tailscale only | Defaults from the config: time zone, allowed networks |
| One assistant provider | An "Assistant" with provider presets: xAI, OpenAI, Anthropic, or a local model through Ollama. The client is OpenAI-compatible |
| Container names from one old panel and a fixed list of app names | The app store's own records (Phase 5) |

### Phases 4–6 — host modules, app store, maintenance

- **Phase 4, host modules** (`cirrusux-setup`): base, firewall, cirrusux
  (4a), network, backup, security (4b), CirrusUX's own sleep manager (4c),
  and later apps, ftps, sleep, ssh and store. The set was tried on clean
  Debian 13 VMs: the single setup with every module, then a sleeping server
  and a gateway.
- **Phase 5, app store:** a catalogue of app definitions rendered to compose
  files or Quadlet units (5a), installs as background jobs on the server
  (5b), and the store in the UI (5c).
- **Phase 6, maintenance:** app updates on request (6a), OS updates and the
  Logs page with "Explain this" (6b), the Storage page and archive review
  (6c), the router power check and custom widgets (6d).

### Phase 7 — public release

- **Licence** (D10): AGPL-3.0-or-later, in `LICENSE`. Done.
- **Docs:** an install guide for both setups ([INSTALL.md](INSTALL.md)), a full
  configuration reference ([CONFIG.md](CONFIG.md)) and an app-definition
  reference ([APPS.md](APPS.md)). Done; the install guide starts from the
  released `install.sh`, and building from source is the fallback.
- **CI:** GitHub Actions running `make test`, the race detector, the host
  script tests and the build on every push (`.github/workflows/test.yml`,
  actions pinned to commits). Done.
- **Project files:** `SECURITY.md` (private reporting through GitHub),
  `CONTRIBUTING.md`, issue templates. Done.
- **Name:** no project or product called CirrusUX turned up (checked
  2026-11-02); the nearest are Cirrus CI, Cirrus Logic and CirrusLabs, none
  of them in self-hosting. Done.
- **History:** the public repository starts from a clean first commit of the
  scrubbed tree; the full earlier history is kept privately. Done.
- **Releases:** bundles with checksums for amd64 and arm64, `install.sh`,
  and `cirrusux-setup upgrade` and `rollback`. Done; `.deb` packages come
  after 1.0.
- **Official key:** the public half is built in since v0.7.1. Done.

### Phase 8 — reinstall a real server

Reinstalling the maintainer's own server on the public tooling is tracked
privately. The generic steps are in [INSTALL.md](INSTALL.md): capture the old
setup read-only, prove a backup restores, reinstall, run the host modules,
install apps from the app store, and watch the idle log for 24 hours, since
the sleep manager is the riskiest part to rebuild.

## 3. Defaults for everyone

The design rules every feature follows. The ones marked * can be changed in
the config.

- No secrets in plain text, in the browser, URLs or logs.
- Every change is confirmed, logged and has an undo where one exists.
- Archive instead of delete; deletion is a separate two-step review. Only
  downloaded images go without one: when their app is removed, or when an
  update leaves them unused.
- Pinned image versions; `:latest` is refused.
- No telemetry and no automatic updates; checks run only when asked.
- The assistant only proposes actions; a person confirms every one, and what
  it is sent is redacted.
- Low energy: nothing the panel does keeps a sleeping server awake.
- Reachable on the LAN and the tailnet only.*
- Local time zone shown with its label.*

## 4. Decisions

| # | Decision | Outcome |
|---|---|---|
| D8 | How a server moves off another panel | Full reinstall, Debian included; apps start clean; old app folders archived. **Decided** |
| D9 | Default container runtime for the app store | Podman with Quadlet units (no always-running daemon, containers are systemd units); Docker supported. **Decided** |
| D10 | Licence | AGPL-3.0-or-later: copyleft that also covers hosted use. **Decided** |
| D11 | Sign-in providers for v1 of the public release | Tailscale and a reverse proxy's trusted header (`CIRRUS_AUTH=tailscale\|header`). **Decided.** Built-in accounts with passkeys or TOTP: **Not started** |
| D12 | Config format | Plain `NAME=value` env files, one per part (gateway, agent, helper). **Decided:** Go's standard library has no TOML reader, and systemd reads env files natively |


## 5. Road to 1.0

The code is in place: the audit of 7 November 2026
([history/AUDIT-2026-11-07.md](history/AUDIT-2026-11-07.md)) is fully fixed,
and CI passes. What is left before 1.0.0 is publishing and proving it on
real machines. In order:

| # | Step | Who | Status |
|---|---|---|---|
| 1 | Turn on GitHub's private vulnerability reporting, which `SECURITY.md` points to | Maintainer (repository setting) | Done (11 November 2026) |
| 2 | Publish the official catalogue: the `cirrusstore` repository with Pages, its first site signed offline with `make store-site`, and a fresh panel that fetches and verifies it | Maintainer (offline key) | Done (11 November 2026) |
| 3 | Release 0.8.0 and confirm its bundles carry build attestations | Maintainer | Done (11 November 2026): `gh attestation verify` passes for both bundles and `install.sh` |
| 4 | Install 0.8.0 from `install.sh` on clean machines: both setups, Debian 13 and Ubuntu LTS, amd64 and arm64 where possible; then `upgrade`, `rollback`, `pair` and `join` | Maintainer, real hardware or VMs | In progress: amd64 VMs passed on 11 November 2026 (Debian 13 single and split, Ubuntu 26.04.1 single, 0.7.1 → 0.8.0 upgrade and rollback) once three setup fixes are in, for 0.8.1; arm64 not tried |
| 5 | Phase 8: reinstall a real server on 0.8.x and watch the idle log for 24 hours | Maintainer | Private |
| 6 | Fix what steps 4 and 5 find, in 0.8.x releases | Anyone | — |
| 7 | Tag 1.0.0 after about a week of quiet running on the real server | Maintainer | — |

Not in 1.0: `.deb` packages and built-in accounts (D11). Both stay on the
"Later" list.

### What 1.0 keeps stable

From 1.0.0, these change only in a new major version, or with a migration
that runs by itself and is noted in the changelog:

- **Settings:** the names and meanings of the env-file settings in
  [CONFIG.md](CONFIG.md) and `host.env`. A renamed setting keeps working
  under its old name for at least one minor release.
- **Split setups:** a gateway and an agent one minor version apart work
  together (`snapshot.Protocol`, and the actions list for anything newer).
- **Saved boards and settings:** migrated when their shape changes, never
  reset.
- **CirrusStore:** the definition format, envelopes, signatures and key
  format in [APPS.md](APPS.md). New fields arrive behind `minVersion`.
- **The install layout:** `/opt/cirrusux/releases`, `/opt/cirrusux/current`
  and `/etc/cirrusux`, so `upgrade` and `rollback` keep working across
  releases.
