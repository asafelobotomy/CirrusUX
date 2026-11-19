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
scripts/sign-release.sh --check-agent   # the agent offers the release key (fix it now, not after tagging)
make bump V=x.y.z                       # VERSION, web/package.json, a dated CHANGELOG section
git commit -am 'Release vX.Y.Z'
git tag -a vX.Y.Z -m 'CirrusUX X.Y.Z'
git push origin main vX.Y.Z
scripts/sign-release.sh vX.Y.Z          # once the release workflow has finished
scripts/verify-release.sh vX.Y.Z        # what install.sh will check, from GitHub
```

The tag starts `.github/workflows/release.yml`. It builds a bundle for each
CPU with `scripts/bundle.sh` (bin/, deploy/, install.sh and the docs) and
attests the bundles. It then publishes them, with `install.sh` and
`SHA256SUMS`, as the GitHub release that `install.sh` and
`cirrusux-setup upgrade` download.

A release installs nowhere until it is signed: `install.sh` and the panel's
Upgrade button accept it only with a `SHA256SUMS.sig` made by the release
key, whose public half is `RELEASE_KEY` in `install.sh`.
`scripts/sign-release.sh` downloads the release, checks every file against
`SHA256SUMS` and GitHub's build attestation, signs `SHA256SUMS` and uploads
the signature. The private key stays in an SSH agent (a password manager's,
a hardware key's): point `SSH_AUTH_SOCK` at it. It is never written to
disk, and GitHub never has it, so a release changed on GitHub fails the
check. Replacing the key means a release with a new `RELEASE_KEY`; machines
on older releases then upgrade once with `install.sh --unsigned` or a fresh
`install.sh`.

### Signing a release, step by step

The release key is an SSH key item in Proton Pass. Its agent signs with it;
the private key never leaves Proton Pass.

1. **Before tagging**, start Proton Pass's agent and check it offers the
   key:

   ```bash
   pass-cli ssh-agent daemon start        # once logged in with pass-cli; socket ~/.ssh/proton-pass-agent.sock
   scripts/sign-release.sh --check-agent  # prints the key's fingerprint; passes only if the agent offers it
   ```

   The script uses `~/.ssh/proton-pass-agent.sock` when `SSH_AUTH_SOCK` is
   unset. The desktop app's agent works too (Settings, General, "Use Proton
   Pass as SSH agent"); export the `SSH_AUTH_SOCK` its setup command gives.
   Only public keys are read (`ssh-add -L`).
2. **Tag and push**, then wait for the release workflow to finish
   (`gh run watch`).
3. **Sign:** `scripts/sign-release.sh vX.Y.Z`. It checks the agent again,
   downloads the release, checks it against `SHA256SUMS` and the build
   attestations, has the agent sign `SHA256SUMS`, verifies the signature
   locally, uploads `SHA256SUMS.sig` and then checks the copy GitHub serves.
4. **Verify:** `scripts/verify-release.sh vX.Y.Z` downloads the release as
   a stranger would and checks the signature against `RELEASE_KEY` in your
   checkout's `install.sh`, then every file against `SHA256SUMS`. It
   needs no key and no sign-in, so anyone can run it. `--from DIR` checks a
   folder you already have; `--sums-only` skips downloading the bundles.

`deploy/host/tests/release-signing.sh` (part of `make test-host`) runs
the same steps with a throwaway key in a scratch `ssh-agent`.

### When the agent doesn't offer the key

`sign-release.sh` stops before it downloads anything and lists the keys the
agent does offer (fingerprints only). The release key's fingerprint is
`SHA256:x+sDW0ML8OfvNNrIWXZHSrxe1xFCwhs8Pox/7Bdjxhg`. Check, in order:

- **A key added since the agent started.** Proton Pass's agent looks for
  new SSH key items only once an hour (`--refresh-interval`, default 3600
  seconds). Restart it to load the key now:
  `pass-cli ssh-agent daemon stop && pass-cli ssh-agent daemon start`.
- **The wrong agent.** `echo $SSH_AUTH_SOCK` should be
  `~/.ssh/proton-pass-agent.sock` (or the desktop app's socket), not
  another `ssh-agent` or one forwarded over SSH.
- **The wrong vault or item.** The agent offers only items of type "SSH
  key", from every vault unless it was started with `--vault-name`; a key
  saved as a note or login, or in the trash, isn't offered.
- **Logged out.** The daemon fails silently when pass-cli isn't logged in;
  `pass-cli ssh-agent daemon start --log-file /tmp/pp-agent.log` shows why.

To use a plain `ssh-agent` instead, `pass-cli ssh-agent load --vault-name
VAULT` copies the vault's SSH keys into the agent `SSH_AUTH_SOCK` names.
Run `scripts/sign-release.sh --check-agent` again until it passes.

Don't work around it by exporting the private key to a file: the key is
only ever in Proton Pass and its agent, never on disk or in the
repository. If the agent can't be fixed, the release waits; it installs
nowhere until it is signed, so waiting is safe.

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
- **Nothing runs by itself** that phones out: checks run only when asked. The one exception is the gateway's daily look at which CirrusUX release is the latest (`CIRRUS_UPDATE_CHECK=off` stops it); it installs nothing.
- **Plain English** in the UI and docs: short sentences, no jargon a home
  user wouldn't know.
- Tests for new behaviour, especially anything that runs as root.

## Licence

CirrusUX is under the GNU AGPL 3.0 or later. By sending a change, you agree
it's released under the same licence.
