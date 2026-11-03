# Contributing

Thanks for wanting to help. CirrusUX is small on purpose, and a few rules
keep it that way.

## Before you start

For anything bigger than a fix, open an issue first and say what you'd like
to change, so nobody spends a weekend on something that won't fit.

## Building and testing

You need Go (the version in `go.mod`), Node.js 26, and for the host scripts
`shellcheck`. See the README's "Develop" section for running it locally.

```bash
make web        # build the UI
make test       # gofmt, go vet, Go tests, svelte-check, UI tests
make test-host  # the host setup scripts, in a user namespace
```

All of these must pass; CI runs them on every push.

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
- **Nothing runs by itself** that phones out: checks run only when asked.
- **Plain English** in the UI and docs: short sentences, no jargon a home
  user wouldn't know.
- Tests for new behaviour, especially anything that runs as root.

## Licence

CirrusUX is under the GNU AGPL 3.0 or later. By sending a change, you agree
it's released under the same licence.
