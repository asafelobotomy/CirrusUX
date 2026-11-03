# CirrusUX

A self-hosting web panel: power state and wake, services, backups, security
and an assistant. It runs on the server itself, or beside a server that
sleeps to save power, answering from the last report while it does.

- `docs/INSTALL.md` — installing, in either setup (start here)
- `docs/CONFIG.md` — every setting and module
- `docs/APPS.md` — writing and publishing apps for CirrusStore
- `docs/ROADMAP.md` — what comes next; `docs/PHASES-4-6.md` — how phases 4–6 are built
- `docs/PLAN.md` — the original plan and design spec (Phases 0–2)
- `web/` — the UI (Svelte 5); runs on sample data when no gateway answers
- `cmd/gateway` — serves the UI, caches reports, wakes a sleeping server, runs the assistant
- `cmd/agent` — on the server: collects a snapshot and pushes it (no listening port)
- `cmd/helper` — on the server, root via sudo: fixed reads plus a few fixed actions, paths from its own config
- `cmd/dockerfilter` — on the server, root: Docker API on a unix socket, reads plus start/stop/restart only
- `cmd/cirrusstore` — for publishers: signs a CirrusStore catalogue (`keygen`, `sign`, `index`, `verify`)
- `deploy/server`, `deploy/gateway` — systemd units, sudoers rules, example config
- `internal/catalogue` — the built-in CirrusStore catalogue (`apps/*.json`) and its compose/Quadlet renderers
- `internal/store` — CirrusStore's signed online catalogues: keys, sources, envelopes and their checks
- `deploy/host` — `cirrusux-setup`: re-runnable host modules (base, firewall, cirrusux) with a check mode
- `deploy/profiles/cloud64` — the owner's own setup (private; leaves before the repo goes public)
- `deploy/dev` — the demo snapshot for local runs
- `scripts/bump-version.sh` — used by `make bump`
- `scripts/make-icons.sh` — renders the app icons in `web/public` from `web/icons/*.svg`

## Develop

```bash
make test                  # gofmt, go vet, go test, svelte-check, UI unit tests
make test-host             # shellcheck and the host modules, in private namespaces
cd web && npm run dev      # UI on :5174, demo data unless a gateway runs
```

Run the whole thing locally without a server or Tailscale, in two terminals:

```bash
make web                   # build the UI the gateway embeds
make dev-gateway           # http://127.0.0.1:8064, dev mode, data in .dev-data/
make dev-agent             # pushes deploy/dev/demo-snapshot.json every minute
```

`make dev-gateway SETUP=single` runs the single setup instead of the split
one. `npm run dev` proxies `/api` to that gateway, so the dev server then
shows live mode too. In the split setup, stop the agent for ~7 minutes to see
the asleep state.

Go code uses only the standard library.

## Versions

`VERSION` holds the version number; everything else reads it. Releases follow
semantic versioning (before 1.0.0, one minor version per plan phase), are
tagged `vX.Y.Z`, and are described in `CHANGELOG.md`.

```bash
make bump V=0.2.0     # VERSION, web/package.json, a dated CHANGELOG section
make build            # binaries print their version with -version
```

The gateway logs its version, `/api/me` reports it, the agent puts its own in
every snapshot, and Settings → About shows all three with a warning when
they differ.

## Licence

CirrusUX is free software under the GNU Affero General Public License,
version 3 or (at your option) any later version; see `LICENSE`. If you run a
changed version for other people over a network, the AGPL asks you to offer
them its source code.
