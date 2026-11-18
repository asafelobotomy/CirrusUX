# CirrusUX

Self-hosting web panel (power state and wake, services, backups, security, assistant) with a Go backend (standard library only) and a Svelte 5 UI in `web/`.

## Commands
Full build, test, local-run and release steps are in `CONTRIBUTING.md`. For agents:
- Build the UI first (the gateway embeds `web/dist`): `make web`. A fresh clone gets a placeholder `web/dist` from `make web-stub`, which `make test` runs itself.
- Test everything CI runs: `make test`. Also `go test -race ./...` (CI), and `make test-host` (needs `shellcheck`, `nft`, user namespaces). The test fails on unformatted files: `gofmt -w .`.
- Run locally with demo data, two terminals: `make dev-gateway` (http://127.0.0.1:8064) and `make dev-agent`.
- `make store-site KEY=...` signs the CirrusStore catalogue and needs the offline key; don't run it.
- `scripts/sign-release.sh vX.Y.Z` signs a published release with the release key, which an SSH agent holds (CONTRIBUTING.md, Releases). A release installs nowhere until it is signed.
- `scripts/verify-release.sh vX.Y.Z` checks a release's signature and checksums as `install.sh` does; it needs no key. Never read, export or commit the release key's private half.

## Layout
- `cmd/`: `gateway`, `agent`, `helper` (root via sudo), `dockerfilter`, `idle`, `cirrusstore`. `internal/`: the Go packages (`gateway`, `agent`, `helperapi`, `catalogue` with `apps/*.json`, `store`, `assistant`, `idle`, `githost` (Git hosting, docs/GIT.md), ...).
- `web/`: Svelte UI (`src`, `tests`, `icons`; `web/embed.go` embeds `dist`). `deploy/`: `server`, `gateway`, `host` (`cirrusux-setup` and tests), `dev` (demo snapshot), `profiles` (git-ignored except `example`). `docs/`: INSTALL, CONFIG, APPS, GIT, ROADMAP (the status table), `history/` (superseded design).

## Conventions
- Go uses only the standard library: no new Go modules. New npm packages need a good reason and an exact pinned version.
- No source file over 400 lines, comments and blanks included (`scripts/check-file-length.sh`); split by what the parts do.
- Catalogue images are pinned by tag and digest, never `:latest`.
- No secrets and no personal details (real addresses, MACs, host names) in the repo; use `homeserver`, `192.0.2.x`, `aa:bb:cc:dd:ee:ff`.
- Anything that changes the server is confirmed by a person, logged, and undoable where possible. Nothing runs by itself that phones out, with one exception: the gateway asks GitHub once a day which CirrusUX release is the latest (`CIRRUS_UPDATE_CHECK=off` stops it). Installing a release is still a confirmed action.
- Plain English in UI and docs. Add tests for new behaviour, especially root-run code. AGPL-3.0-or-later.
- Versions: `VERSION` is the source. Bump with `make bump V=x.y.z` (writes `VERSION`, `web/package.json`, and a dated CHANGELOG section from `[Unreleased]`); it does not commit or tag. Then commit, tag and push as in CONTRIBUTING.md. Record changes under `## [Unreleased]` in `CHANGELOG.md`.

## Gotchas
- `bin/`, `web/dist/`, `web/node_modules/`, `.dev-data/` and `.claude/` are git-ignored. CI uses Go from `go.mod` and Node 26.
- `make test-host` runs host modules in a private user namespace, never as real root.
