# Security

CirrusUX runs with real power over a server: it starts and stops
containers, installs updates and, through a few fixed sudo rules, acts as
root. Security reports are welcome and taken seriously.

## Reporting a problem

Please don't open a public issue. Use GitHub's private reporting instead:
the repository's **Security** tab → **Report a vulnerability**. Say what
you found, how to reproduce it, and which version (`cirrusux-gateway
-version`) and setup (single or split) you saw it on.

You'll get an answer within a week. Fixes go into a new release, and the
changelog credits you unless you'd rather it didn't.

## Supported versions

Before 1.0.0, only the latest release gets fixes.

## What CirrusUX promises

These are design rules; a way around any of them is a security problem:

- Secrets never reach the browser, URLs or logs, and the router password
  never reaches the panel at all.
- The panel is reachable only from the LAN and the tailnet by default,
  and answers only on names it knows (`CIRRUS_HOSTS`), so a DNS-rebinding
  page can't reach it through a local browser.
- With the panel on its socket (`CIRRUS_SOCKET`, set up with Tailscale
  by `cirrusux-setup`), only the front end is believed about who is
  signed in: other programs on the machine reach only the agent's routes.
- The agent's token travels only over HTTPS, or to this machine or a
  Tailscale address.
- Every change is confirmed by a person; the assistant only proposes.
- The gateway reaches the server's root only through the helper's fixed
  sudoers lines, and the helper checks every argument it's given.
- Containers are reached through a filter that allows a fixed list of
  Docker API calls.
- Images are pinned by digest; `:latest` is refused.
- Nothing is sent anywhere without being asked, and nothing installs
  itself: no telemetry, no automatic updates. The one thing looked up
  unasked is which CirrusUX release is the latest: the gateway asks
  GitHub once a day (`CIRRUS_UPDATE_CHECK=off` stops it) and sends
  nothing but the request.
- A CirrusUX release is installed only if it is signed with the release
  key (below), whether from `install.sh` or from the panel.
- An app from CirrusStore runs only if root on the server trusts its
  source (below), and the root helper has checked its signature itself.

## CirrusStore's trust model

CirrusStore's online catalogues (the official one, and any third-party
source) are static files signed with Ed25519. The server is the source of
trust:

- **Keys live on the server.** The official public key is built into the
  binaries; third-party keys are in `/etc/cirrusux/store.d/`, written by
  `cirrusux-setup` from `host.env`. The helper refuses files there that
  root doesn't own or others can write. The panel shows each key's
  fingerprint, but it can't add, change or remove a source. It can check
  one before you trust it (the gateway fetches that catalogue once with
  the key you give, verifies it and keeps nothing); the result is a line
  for `host.env`, which root applies.
- **The helper checks again.** The gateway fetches and verifies catalogues
  only for browsing. An install or update hands the helper the exact signed
  envelope, and the helper verifies it against its own keys (signature,
  source name, expiry, serial) before the definition is even parsed. A
  compromised gateway or panel can't install an unsigned app, swap an app
  for another source's, or replay an older definition.
- **Definitions stay limited.** A signed definition is checked like a
  built-in one: pinned by digest, no privileged mode, host networking,
  capabilities or arbitrary mounts, and unknown fields refused. Trusting a
  source means trusting its publisher to pick sensible images; it doesn't
  give the publisher root.
- **Old files can't come back.** Every file carries a serial and an expiry.
  A serial older than one already seen is refused, and an expired catalogue
  can't be installed from until it's re-signed.
- **Offline first.** Nothing is fetched until the owner presses Check
  CirrusStore. Built-in apps never need the network, and installed apps keep
  the definition they were installed from.
- **The official signing key is kept offline** by the maintainers. A leaked
  or lost key is a security problem: report it as above. A release then
  replaces the built-in key, and `STORE_OFFICIAL=no` stops trusting the
  official catalogue in the meantime.

Report a way around any of these as a vulnerability.

## How releases are trusted

- **Signed away from GitHub.** Each release's `SHA256SUMS` is signed with
  the release key after the build, and `install.sh` checks that signature
  (with `ssh-keygen`) and then the bundle's checksum before unpacking
  anything. The key's public half is in `install.sh`; the private half is
  held by the maintainers in an SSH agent and never reaches GitHub or its
  build runners. A release or account changed on GitHub can't produce a
  bundle that installs.
- **The panel adds no new way in.** Upgrading from the panel runs
  `cirrusux-setup upgrade` through the root helper, with one argument: a
  release tag, checked against `vX.Y.Z` by the gateway, the agent and the
  helper. The gateway only passes on the release its last check found. The
  download, the signature check and the apply are the same code as at a
  terminal.
- **The first signed release** can't be checked by the copy of
  `install.sh` already on a machine, since that copy predates signing.
  Download `install.sh` afresh for that one upgrade if you want the check.
- A release from before signing began installs only with
  `install.sh --unsigned`, which says so.
