# cloud64 panel

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

## Develop

```bash
make test                  # go vet, go test, svelte-check
cd web && npm run dev      # UI on :5174, demo data unless a gateway runs
```

Run the whole thing locally without cloud64 or Tailscale, in two terminals:

```bash
make dev-gateway           # http://127.0.0.1:8064, dev mode, data in .dev-data/
make dev-agent             # pushes deploy/dev/demo-snapshot.json every minute
```

`npm run dev` proxies `/api` to that gateway, so the dev server then shows
live mode too. Stop the agent for ~7 minutes to see the asleep state.

Go code uses only the standard library.
