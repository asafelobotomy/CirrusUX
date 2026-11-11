# Contributing

Thanks for wanting to help. CirrusUX is small on purpose, and a few rules
keep it that way.

## Before you start

For anything bigger than a fix, open an issue first and say what you'd like
to change, so nobody spends a weekend on something that won't fit.

## Building and testing

You need Go (the version in `go.mod`), Node.js 26, and for the host scripts
`shellcheck`.

```bash
make web        # build the UI (the gateway embeds web/dist)
make test       # file lengths, gofmt, go vet, Go tests, svelte-check, UI tests
make test-host  # the host setup scripts, in a user namespace
make build      # all binaries into bin/ (they print their version with -version)
```

All of these must pass; CI runs them, and `go test -race ./...`, on every
push. Format with `gofmt -w .`. UI checks alone: `cd web && npm run check &&
npm test`.

## Running it locally

Run the whole thing without a server or Tailscale, in two terminals:

```bash
make web                   # build the UI the gateway embeds
make dev-gateway           # http://127.0.0.1:8064, dev mode, data in .dev-data/
make dev-agent             # pushes deploy/dev/demo-snapshot.json every minute
```

`make dev-gateway SETUP=single` runs the single setup instead of the split
one. For the UI alone, `cd web && npm run dev` serves it on :5174 with demo
data, and proxies `/api` to the gateway when one runs. In the split setup,
stop the agent for about 7 minutes to see the asleep state.

## Releases

`VERSION` is the source of the version number. Record changes under
`## [Unreleased]` in `CHANGELOG.md` as you go. To release:

```bash
make bump V=x.y.z                       # VERSION, web/package.json, a dated CHANGELOG section
git commit -am 'Release vX.Y.Z'
git tag -a vX.Y.Z -m 'CirrusUX X.Y.Z'
git push origin main vX.Y.Z
```

The tag starts `.github/workflows/release.yml`. It builds a bundle for each
CPU with `scripts/bundle.sh` (bin/, deploy/, install.sh and the docs) and
attests the bundles. It then publishes them, with `install.sh` and
`SHA256SUMS`, as the GitHub release that `install.sh` and
`cirrusux-setup upgrade` download.

`make bump` does not commit or tag. Releases follow semantic versioning
(before 1.0.0, one minor version per plan phase). From 1.0.0, what a
release must keep working is listed in docs/ROADMAP.md, "What 1.0 keeps
stable"; breaking it needs a new major version or a migration that runs by
itself.

The official CirrusStore catalogue lives in its own repository,
`asafelobotomy/cirrusstore`, published with GitHub Pages. It is signed with
`make store-site KEY=...` (or that repository's own signing script), which
needs the offline key; only the maintainer runs it.

## House rules

- **Go uses only the standard library.** No new Go modules.
- **No source file over 400 lines**, comments and blank lines included;
  `make test` checks (`scripts/check-file-length.sh`). Split a long file
  by what its parts do.
- **New npm packages need a good reason**, pinned to an exact version.
- **Images are pinned** by tag and digest in the catalogue; never `:latest`.
- **No secrets** in code, tests, logs or the browser. Write `<set in env>`
  in examples.
- **No personal details** in the repository: no real addresses, MAC
  addresses, host names or account names. Use `homeserver`,
  `192.0.2.x` and `aa:bb:cc:dd:ee:ff`.
- **Anything that changes the server** is confirmed by a person, logged, and
  has an undo where one can exist. Deleting goes through the archive review.
- **A new action** goes in the agent's `fixedActions` (or `containerVerbs`),
  which feeds the list each snapshot reports, so a newer panel can switch off
  what an older server lacks. Bump `snapshot.Protocol` only when the snapshot
  or a command changes in a way an older side can't read, not for a new action.
- **Nothing runs by itself** that phones out: checks run only when asked.
- **Plain English** in the UI and docs: short sentences, no jargon a home
  user wouldn't know.
- Tests for new behaviour, especially anything that runs as root.

## Licence

CirrusUX is under the GNU AGPL 3.0 or later. By sending a change, you agree
it's released under the same licence.
