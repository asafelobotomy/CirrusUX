# CirrusUX roadmap after v0.2.0

Phases 0–2 (PLAN.md §6) built CirrusUX for one server, cloud64, around the
owner's own scripts and 1Panel. This roadmap changes direction:

- **Any server, not one.** CirrusUX becomes a general self-hosting panel that
  anyone can install, and is published as a public repository once Phase 7 is
  done. Everything new is written for any host from the start.
- **cloud64 becomes a private profile.** Its names, paths, wake method and the
  owner's rules live in a configuration kept out of the public repo.
- **No migration from 1Panel.** cloud64 is reinstalled from scratch (Debian
  included) with CirrusUX's own tooling, and its apps start clean from the
  CirrusUX app store (D8).

What carries forward:

- **The UI layout as a foundation.** The shell (sidebar, top bar, command
  palette, assistant panel), the boards with their widgets, the themes and the
  design tokens are the starting point. New pages build on them, and the
  layout changes wherever a phase needs it to; it isn't fixed. When a change
  affects saved boards or settings, they're migrated rather than reset.
- **The safety rules.** The design rules that made Phases 0–2 safe become
  CirrusUX's defaults for everyone (§4).

## 1. Order of work

| Phase | What | Done when |
|---|---|---|
| 3 | Generalise the core | A fresh machine that isn't cloud64 runs CirrusUX from a config file alone, in both setups |
| 4 | Host modules | A clean Debian VM goes from install to a sleeping, backed-up, firewalled server by re-runnable scripts |
| 5 | App store | An app installs, updates and is removed (archived) through the panel on Docker and on Podman |
| 6 | Maintenance | The old Phase 3 items below work on top of the app store |
| 7 | Public release | The repo can be made public: licence, docs, CI, packages, history checked |
| 8 | Reinstall cloud64 | cloud64 runs entirely on the public tooling plus its private profile, and sleeps as well as before |

The read-only capture of cloud64 (§3.1) can run any time before Phase 8, and
is worth doing early: its idle-check tuning is the starting point for the
generic sleep manager in Phase 4.

## 2. Phases

### Phase 3 — generalise the core

**Status:** done in v0.3.0, and tried on a second machine (a workstation with Podman, single setup, real data). Settings are in docs/CONFIG.md.

What is tied to cloud64 today, and what replaces it:

| Tied to cloud64 now | Becomes |
|---|---|
| The names cloud64 and cloud-box in the UI, assistant prompt and units | A server name and settings from the env files (D12, docs/CONFIG.md) |
| Two machines only: an always-on gateway and a server that sleeps | Two setups. **Single host:** gateway and agent on one machine that doesn't sleep (the common case). **Split:** a sleeping server and an always-on gateway, as now |
| The owner's scripts at fixed paths in the helper | Helper modules with a defined interface (sleep, backups, security), so the paths come from the module, not the code |
| Wake through the router and `cloud64-wake` | Wake drivers: a magic packet sent by the gateway on the LAN (default), the router's TR-064, or a custom command |
| Sign-in only through Tailscale's login header | Sign-in providers (D11): Tailscale, a trusted header from a reverse proxy (Authelia, Authentik, oauth2-proxy), or built-in accounts with passkeys or TOTP |
| UK time, LAN + Tailscale only | Defaults from the config: time zone, allowed networks |
| Grok only | An "Assistant" with provider presets: xAI, OpenAI, Anthropic, or a local model through Ollama. The client is already OpenAI-compatible |
| 1Panel container names and a fixed list of app names | The app store's own records (Phase 5) |

- Rename the cloud64-specific identifiers in code and units; the private
  profile supplies "cloud64" as the server name.
- The docs stop citing the owner's brief; anything still needed from it moves
  into the docs themselves or the private profile.

### Phases 4–6 — host modules, app store, maintenance

The build plan, part by part, is in [PHASES-4-6.md](PHASES-4-6.md).

- **Phase 4, host modules** (`cirrusux-setup`): base, firewall, cirrusux
  (4a), network, backup, security (4b) and CirrusUX's own sleep manager
  (4c) are built. Left: trying the whole set on a clean Debian VM.
- **Phase 5, app store:** a catalogue of app definitions rendered to compose
  files or Quadlet units (5a), installs as background jobs on the server
  (5b), and the store in the UI (5c).
- **Phase 6, maintenance:** updates on request, Logs page with "Explain
  this", Storage page, archive review, router power check, custom widgets.

### Phase 7 — public release

- **Licence** (D10): no LICENSE file yet.
- **Docs:** a public install guide for both setups, a configuration reference
  and an app-definition reference.
- **History:** check the git history for personal details once more, or start
  the public repository from a clean first commit.
- **CI:** GitHub Actions running `make test` on every push.
- **Releases:** binaries with checksums; an install script and `.deb`
  packages for Debian and Ubuntu.
- **Project files:** `SECURITY.md`, `CONTRIBUTING.md`, issue templates.
- **Name:** check that "CirrusUX" doesn't clash with an existing project.

### Phase 8 — reinstall cloud64

1. Run the capture (§3.1) if it hasn't run yet.
2. Prove a backup restores on the admin machine, where the private key lives.
3. Reinstall Debian. The media disk is not formatted.
4. Run the Phase 4 modules with the private profile.
5. Install the apps from the app store; they start clean (D8).
6. Reinstall Jellyfin's Trakt plugin with the same settings (owner's rule 9).
7. Watch the idle log for 24 h: the sleep manager is the riskiest part to
   rebuild.

Expect cloud64 to be out of service from step 3 until step 5 finishes.

## 3. cloud64 specifics (private profile)

### 3.1 Capture before the wipe

A read-only script copies off cloud64, to the admin machine:

- the owner's scripts in `/usr/local/bin` and `/usr/local/sbin` (idle check,
  stay-awake, allow-sleep, backup, egress report);
- `/etc/cloud64-idle.conf` and the related systemd units and timers;
- sudoers, fail2ban and firewall rules;
- the media disk's `fstab` entry and the network card's wake-on-LAN settings;
- the current compose files, as a reference for mounts, devices and groups.

Each app's config folder is archived to the media disk rather than deleted,
even though the new apps start clean.

### 3.2 What the profile holds

Server and gateway names, the wake driver (router TR-064 with the password in
a root-only file, as now), allowed Tailscale logins, UK time, LAN + Tailscale
only, and the owner's 13 rules (PLAN.md §8) as settings where the code allows.

## 4. Defaults for everyone

The owner's rules, generalised. Each is the default; the ones marked * can be
changed in the config.

- No secrets in plain text, in the browser, URLs or logs.
- Every change is confirmed, logged and has an undo where one exists.
- Archive instead of delete; deletion is a separate two-step review.
- Pinned image versions; `:latest` is refused.
- No telemetry and no automatic updates; checks run only when asked.
- The assistant only proposes actions; a person confirms every one, and what
  it is sent is redacted.
- Low energy: nothing the panel does keeps a sleeping server awake.
- Reachable on the LAN and the tailnet only.*
- Local time zone shown with its label.*

## 5. Decisions

| # | Decision | Proposed |
|---|---|---|
| D8 | How cloud64 moves off 1Panel | Full reinstall, Debian included; apps start clean; old app folders archived. **Decided** |
| D9 | Default container runtime for the app store | Podman with Quadlet units (no always-running daemon, containers are systemd units); Docker supported |
| D10 | Licence | Open. Permissive (MIT or Apache-2.0) or copyleft that also covers hosted use (AGPL-3.0) |
| D11 | Sign-in providers for v1 of the public release | Tailscale and a reverse proxy's trusted header first; built-in accounts with passkeys after |
| D12 | Config format | Plain `NAME=value` env files, one per part (gateway, agent, helper). **Decided:** Go's standard library has no TOML reader, and systemd reads env files natively |
