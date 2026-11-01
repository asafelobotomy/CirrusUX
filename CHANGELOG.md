# Changelog

All notable changes to CirrusUX. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, each plan phase
gets a minor version.

## [Unreleased]

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
