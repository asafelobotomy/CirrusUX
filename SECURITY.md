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
- The panel is reachable only from the LAN and the tailnet by default.
- Every change is confirmed by a person; the assistant only proposes.
- The gateway reaches the server's root only through the helper's fixed
  sudoers lines, and the helper checks every argument it's given.
- Containers are reached through a filter that allows a fixed list of
  Docker API calls.
- Images are pinned by digest; `:latest` is refused.
- Nothing is sent anywhere without being asked: no telemetry, no automatic
  updates.
