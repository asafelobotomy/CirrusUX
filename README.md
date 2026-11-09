# CirrusUX

A self-hosting web panel: power state and wake, services, backups, security
and an assistant. It runs on the server itself, or beside a server that
sleeps to save power, answering from the last report while it does.

To install, on a Debian or Ubuntu server with Tailscale:

```bash
curl -fsSLO https://github.com/asafelobotomy/CirrusUX/releases/latest/download/install.sh
less install.sh       # read it first
sudo bash install.sh  # checks the release, then asks a few questions
```

- `docs/INSTALL.md` — installing, in either setup (start here)
- `docs/CONFIG.md` — every setting and module
- `docs/APPS.md` — writing and publishing apps for CirrusStore
- `docs/GIT.md` — Git hosting in the gateway
- `docs/ROADMAP.md` — the status of every phase and what comes next
- `docs/history/` — the original design and how phases 4–6 were built
- `CONTRIBUTING.md` — building, testing, house rules and the release steps
- `web/` — the UI (Svelte 5); runs on sample data when no gateway answers
- `cmd/gateway` — serves the UI, caches reports, wakes a sleeping server, runs the assistant, hosts Git
- `cmd/agent` — on the server: collects a snapshot and pushes it (no listening port)
- `cmd/helper` — on the server, root via sudo: fixed reads plus a few fixed actions, paths from its own config
- `cmd/dockerfilter` — on the server, root: Docker API on a unix socket, reads plus start/stop/restart only
- `cmd/idle` — on the server: the sleep manager that decides when the machine may sleep
- `cmd/cirrusstore` — for publishers: signs a CirrusStore catalogue (`keygen`, `sign`, `index`, `verify`)
- `deploy/server`, `deploy/gateway` — systemd units, sudoers rules, example config
- `internal/catalogue` — the built-in CirrusStore catalogue (`apps/*.json`) and its compose/Quadlet renderers
- `internal/store` — CirrusStore's signed online catalogues: keys, sources, envelopes and their checks
- `internal/idle` — the sleep manager's checks and decisions
- `internal/githost` — Git repositories, smart HTTP and access tokens
- `deploy/host` — `cirrusux-setup`: re-runnable host modules (apps, backup, base, cirrusux, firewall, ftps, network, security, sleep, ssh, store) with a check mode
- `deploy/profiles` — your own server's settings; only an example is tracked
- `deploy/dev` — the demo snapshot for local runs
- `scripts/bump-version.sh` — used by `make bump`
- `scripts/make-icons.sh` — renders the app icons in `web/public` from `web/icons/*.svg`

## Develop

Building, testing, running locally with demo data, and making a release are
in [CONTRIBUTING.md](CONTRIBUTING.md). The short version:

```bash
make web && make test
```

`VERSION` holds the version number; releases follow semantic versioning and
are described in `CHANGELOG.md`. The gateway logs its version, `/api/me`
reports it, the agent puts its own in every snapshot, and Settings → About
shows all three with a warning when they differ.

## Licence

CirrusUX is free software under the GNU Affero General Public License,
version 3 or (at your option) any later version; see `LICENSE`. If you run a
changed version for other people over a network, the AGPL asks you to offer
them its source code.
