# CirrusUX

A web panel for the cloud64 home server: power state and wake, services,
backups and a Grok assistant, designed around a server that sleeps.

- `docs/PLAN.md` — the plan and design spec
- `docs/INSTALL.md` — installing Phase 1 on cloud64 and cloud-box
- `web/` — the UI (Svelte 5); runs on sample data when no gateway answers
- `cmd/gateway` — on cloud-box: serves the UI, caches snapshots, wakes cloud64, Grok
- `cmd/agent` — on cloud64: collects a snapshot and pushes it (no listening port)
- `cmd/helper` — on cloud64, root via sudo: fixed read-only subcommands
- `cmd/dockerfilter` — on cloud64, root: read-only Docker API on a unix socket
- `deploy/` — systemd units, sudoers rules, example env files, demo snapshot
- `scripts/bump-version.sh` — used by `make bump`

## Develop

```bash
make test                  # gofmt, go vet, go test, svelte-check
cd web && npm run dev      # UI on :5174, demo data unless a gateway runs
```

Run the whole thing locally without cloud64 or Tailscale, in two terminals:

```bash
make web                   # build the UI the gateway embeds
make dev-gateway           # http://127.0.0.1:8064, dev mode, data in .dev-data/
make dev-agent             # pushes deploy/dev/demo-snapshot.json every minute
```

`npm run dev` proxies `/api` to that gateway, so the dev server then shows
live mode too. Stop the agent for ~7 minutes to see the asleep state.

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
