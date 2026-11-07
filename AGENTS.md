# CirrusUX

Self-hosting web panel (power state and wake, services, backups, security, assistant) with a Go backend (standard library only) and a Svelte 5 UI in `web/`.

## Commands
- Install UI deps and build (the gateway embeds `web/dist`): `make web` (`cd web && npm ci && npm run build`).
- Test everything CI runs first: `make test` (file-length check, gofmt, `go vet ./...`, `go test ./...`, `svelte-check`, UI unit tests). A fresh clone gets a placeholder `web/dist` from `make web-stub`, which `make test` runs itself.
- Race detector (CI): `go test -race ./...`. Host scripts (needs `shellcheck`, `nft`, user namespaces): `make test-host`.
- Format: `gofmt -w .` (the test fails on unformatted files). UI checks: `cd web && npm run check && npm test`.
- Build all binaries into `bin/`: `make build`.
- Run locally with demo data, two terminals: `make web`, then `make dev-gateway` (http://127.0.0.1:8064; `SETUP=single` for the single setup) and `make dev-agent`. UI dev server: `cd web && npm run dev` (port 5174).
- `make store-site KEY=...` signs the CirrusStore catalogue and needs the offline key; don't run it.

## Layout
- `cmd/`: `gateway`, `agent`, `helper` (root via sudo), `dockerfilter`, `idle`, `cirrusstore`. `internal/`: the Go packages (`gateway`, `agent`, `helperapi`, `catalogue` with `apps/*.json`, `store`, `assistant`, ...).
- `web/`: Svelte UI (`src`, `tests`, `icons`; `web/embed.go` embeds `dist`). `deploy/`: `server`, `gateway`, `host` (`cirrusux-setup` and tests), `dev` (demo snapshot), `profiles`. `docs/`: INSTALL, CONFIG, APPS, ROADMAP, PLAN.

## Conventions
- Go uses only the standard library: no new Go modules. New npm packages need a good reason and an exact pinned version.
- No source file over 400 lines, comments and blanks included (`scripts/check-file-length.sh`); split by what the parts do.
- Catalogue images are pinned by tag and digest, never `:latest`.
- No secrets and no personal details (real addresses, MACs, host names) in the repo; use `homeserver`, `192.0.2.x`, `aa:bb:cc:dd:ee:ff`.
- Anything that changes the server is confirmed by a person, logged, and undoable where possible. Nothing runs by itself that phones out.
- Plain English in UI and docs. Add tests for new behaviour, especially root-run code. AGPL-3.0-or-later.
- Versions: `VERSION` is the source. Bump with `make bump V=x.y.z` (writes `VERSION`, `web/package.json`, and a dated CHANGELOG section from `[Unreleased]`); it does not commit or tag. Then `git commit -am 'Release vX.Y.Z'`, `git tag -a vX.Y.Z -m 'CirrusUX X.Y.Z'`, `git push origin main vX.Y.Z`. Record changes under `## [Unreleased]` in `CHANGELOG.md`.

## Gotchas
- `bin/`, `web/dist/`, `web/node_modules/`, `.dev-data/` and `.claude/` are git-ignored. CI uses Go from `go.mod` and Node 26.
- `make test-host` runs host modules in a private user namespace, never as real root.
