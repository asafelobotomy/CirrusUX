# Changelog

All notable changes to CirrusUX. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0, each plan phase
gets a minor version; from 1.0.0, docs/ROADMAP.md says what stays stable.

## [Unreleased]

### Changed

- The roadmap has a "Road to 1.0" section: the steps left before 1.0.0, and
  what 1.0 keeps stable (settings, split setups one minor version apart,
  saved boards, the CirrusStore format and the install layout). `.deb`
  packages and built-in accounts move to after 1.0.
- The finished November 2026 audit moved to `docs/history/`.

### Fixed

- Right after setup, or at boot when the agent starts before the gateway,
  the panel said the server wasn't awake for up to 5 minutes: the agent's
  first report failed and it waited a full interval to try again. It now
  retries every 15 seconds for 3 minutes.

## [0.7.1] - 2026-11-09

### Added

- The official CirrusStore signing key's public half is built in
  (`internal/store/official.pub`, fingerprint
  `SHA256:dkYf4/3zSROcaLiUWxNg72QUjAgz/TymfRKE5bOZq4o`), so panels can check
  and fetch the official catalogue once it's published.

### Fixed

- The release workflow skips build attestations while the repository is
  private, since GitHub only keeps them for public repositories. v0.7.0's
  release stopped at that step and published nothing, so 0.7.1 is the first
  release with bundles.
- Two checks in `cirrusux-setup init` and `rollback` are written as plain
  `if` statements, which newer shellcheck versions require.

## [0.7.0] - 2026-11-07

### Added

- Git hosting, off by default (`CIRRUS_GIT=1`): Git repositories kept by the
  gateway, so your own server is the remote instead of GitHub. Clone, push
  and pull over HTTPS (`git http-backend`), signed in through `tailscale
  serve` or with access tokens made in the panel. The new Git page creates,
  imports, renames and deletes repositories and browses files, commits,
  branches, tags and diffs. Every change is confirmed, logged and undoable
  (deleted repositories wait in Deleted repositories), and works while the
  server sleeps. See docs/GIT.md.
- An easier install:
  - **`install.sh`:** downloads the release for this CPU, checks it against
    `SHA256SUMS` and unpacks it to `/opt/cirrusux/releases/vX.Y.Z`, with a
    `current` link. Then it starts `cirrusux-setup init`. `--from DIR` installs
    a release you've already downloaded.
  - **`cirrusux-setup init`:** asks a few questions, filling in the time zone,
    the Tailscale login and the wired network card where it can find them. It
    writes `host.env`, shows the check, and applies it once you agree.
  - **`cirrusux-setup pair` and `join`:** connect a split setup's gateway
    and server with one code, in place of a token made by hand and copied
    into two files. The server's agent now reports its MAC address
    (`CIRRUS_WAKE_IFACE` picks the card), so `CIRRUS_WAKE=wol` no longer
    needs `CIRRUS_WAKE_MAC`.
  - **`cirrusux-setup upgrade [VERSION]` and `rollback`:** move to another
    release and back. Both show the check and ask first; `-y` skips the
    question.
  - **`CIRRUS_TAILSCALE_SERVE=yes`:** publishes the panel on the tailnet
    with `tailscale serve`, as a logged change.
- Split setups that are part-way through an upgrade: the agent now reports
  which protocol it speaks and which actions it runs. The gateway refuses an
  action the server's older agent doesn't know (409, with a plain message)
  instead of letting it fail as "unknown action". The panel greys those
  buttons out, and a banner names the machine to upgrade. The protocol number
  (`snapshot.Protocol`) changes only when the snapshot or command shapes do.
- `minVersion` in CirrusStore app definitions and index entries: the oldest
  CirrusUX that can install the app. A panel that's older lists the app as
  "Needs CirrusUX X.Y.Z" and won't install it or update to it, instead of
  failing to read the definition. The signature, source and expiry checks
  still come first. Unknown fields in the signed index are now accepted, so
  publishers can add entry fields without breaking older panels; app
  definitions stay strict. See docs/APPS.md.
- A release workflow: pushing a `v*` tag builds a ready-to-install bundle for
  amd64 and arm64 (`scripts/bundle.sh`: binaries, setup and docs). It
  publishes them with `install.sh`, `SHA256SUMS` and build attestations.
  `make build-go` builds the binaries without the UI.
- `CIRRUS_GIT_IMPORT_PRIVATE=1` lets Git import copy from addresses inside
  your own network, which are refused otherwise.

### Changed

- UI dependencies updated: Svelte 5.57.3, svelte-check 4.7.8, Vite 8.3.6,
  Lucide 1.70.0 and the Inter and JetBrains Mono fonts 5.3.0. TypeScript stays
  on 6.0.3 because svelte-check does not support TypeScript 7 yet.
- Setup: `HOST_MODULES` defaults to `base cirrusux`, as in `host.env.example`
  (it was `base`). The default backup paths are defined in one place.
- An OS upgrade records which packages moved, from → to, in the job's detail.
- One reader for the `.env` config files: a `# comment` after a value now
  works in the helper's and the sleep manager's settings too.
- Old job status files in `/run/cirrusux-jobs` are removed after 7 days.
- Sizes use one formatter across the panel; failed Changelog and settings
  loads are logged to the browser console.
- CI runs on pushes to `main` and on pull requests, cancels superseded runs,
  has time limits, and checks Go and npm dependencies for known
  vulnerabilities.
- The private server profile and the original design notes are out of the
  repository: PLAN and PHASES-4-6 now live in `docs/history/`, ROADMAP is the
  one status page, and CONTRIBUTING holds the build and release steps.

### Fixed

- A sleep is no longer lost from the history when `systemctl suspend` times
  out after the machine really slept (it blocks until the resume). The sleep
  manager checks the kernel's suspend counter, or failing that the clocks,
  before rolling the record back.
- On narrow screens the page behind the assistant panel is now inert, so
  screen readers and the keyboard stay in the panel. Its confirm dialogs
  still work.
- The Store's app page greys out Install and Remove when the server's agent
  can't run them yet.
- The base module no longer stops when the machine has no `/etc/localtime`.
- Installing or updating an app with `APPS_BIND` set no longer fails its
  health check. The check was always asked on 127.0.0.1, where nothing
  listens when ports are published on one address, so every install was
  stopped after three minutes and every update rolled back.
- An app job no longer hangs until its 40-minute limit when a command prints
  a very long line.
- A refused app request is recorded at once, instead of waiting for another
  job of the same app to finish first.
- The helper refuses an app request whose job id isn't the one it was sent
  under, so one request can't overwrite another job's status or files.
- Git: merge commits show their files and non-ASCII file names show
  properly; repository sizes are cached for a minute, so the Git page loads
  fast with many repositories; paths starting with `-` can be browsed, log
  path filters aren't treated as patterns, and browsing, downloads and page
  numbers have limits.
- Git: a rename, delete or default-branch change can't race a push, and the
  number of git requests served at once is limited.
- Git: a long import is no longer dropped from the command history, so it is
  always logged with its undo; half-finished imports are cleaned up when the
  gateway starts, and stopping the gateway stops running Git actions.
- CirrusStore keeps the last good copy of an app when its download or
  signature check fails, and swaps its cache so a failed refresh can't empty
  it.
- The assistant's daily token budget is reserved before a question runs, so
  parallel questions can't overshoot it, and providers that report no usage
  are counted by an estimate.
- The sleep manager no longer records a suspend that failed (which logged a
  resume that never happened), and stops commands that hang after 20 s, so a
  stuck `ss` or `conntrack` can't keep the server awake for good.
- Storage: deleted entries no longer come back when a scan finishes at the
  same moment, and Delete only removes what the Storage page lists.
- The helper's log tail skips over-long lines instead of failing, and the
  backup summary reads only the backup script's own "OK" line.
- Firewall: ports in `FIREWALL_PUBLIC_TCP` are open for containers'
  published ports too, not just for services on the host.
- SSH: works with `sshd.service` as well as `ssh.service`, and stops with a
  clear message if neither exists. The timed rollback keeps `CONFIG` and
  `ROOT` when systemd runs it.
- Backup: `*.partial` files left by failed runs are removed after 3 days.
- Panel: a few failed checks while waiting for an action no longer report it
  as failed; overlapping settings saves no longer lose the newest edit; the
  Git "Deleted repositories" list shows an error instead of vanishing; and
  copying an access token says so when the browser blocks it.
- Panel: the assistant closes with Escape and keeps focus while it covers
  the page; the wallpaper and import file pickers have labels.

### Security

- The Docker filter passes on only what the panel shows. Container inspect
  no longer carries each container's variables (passwords and API keys
  among them), mounts or settings, and the container list no longer carries
  command lines. Log reads need a tail of at most 5000 lines.
- The helper runs tools only from root's own folders: the sudoers file sets
  `secure_path` and the helper sets `PATH` itself, so the agent's `PATH`
  can't choose what runs as root.
- Measuring storage moves to the actions part of the sudoers file
  (`CIRRUS_ACT`), so a read-only panel (`CIRRUS_ACTIONS=no`) can't start it.
- The helper checks every subcommand's arguments before it runs, and opens
  an app request so that a file swapped for a pipe can't hold it up.
- The gateway and agent services run with more of systemd's sandbox
  (kernel logs, realtime, set-uid files, personality; and for the gateway
  read-only home folders, kernel settings, the clock and namespaces).
- Git over HTTP asks for sign-in before it says whether a repository exists.
- Git import refuses addresses inside your own network (see
  `CIRRUS_GIT_IMPORT_PRIVATE`), keeps an import password out of git's
  command line, and drops query strings from addresses it logs.
  `GIT_SSL_NO_VERIFY` is no longer read from the environment.
- Pushes over the size limit are refused up front, and the limit holds after
  unzipping too.
- The official CirrusStore source always uses the signing key built into
  CirrusUX, never keys reported by the agent.
- The firewall's lockout guard checks IPv6 SSH sessions against the allowed
  networks instead of assuming they are allowed.

## [0.6.1] - 2026-11-03

### Fixed

- The ftps module's passive port check is written so ShellCheck 0.9 (the
  version CI uses) accepts it. Its behaviour is unchanged. v0.5.2 and v0.6.0
  failed CI on this lint step only.

## [0.6.0] - 2026-11-03

### Added

- CirrusStore's online catalogue: apps and newer versions published as
  signed static files, so they arrive without a CirrusUX release. Each app
  is an Ed25519-signed envelope with its source, a serial and an expiry;
  the index is signed too. Nothing is fetched until the owner presses
  **Check CirrusStore**, through `HTTPS_PROXY` if it's set, and verified
  files are cached in `CIRRUS_DATA_DIR/store/`.
- Third-party sources: the `store` setup module writes `STORE_SOURCES` to
  `/etc/cirrusux/store.d/` (root only), and `STORE_OFFICIAL=no` leaves the
  official catalogue out. The panel's Sources card lists each source's URL,
  key fingerprint and status (ok, expired or unreachable), but can't add one.
- The root helper verifies every signed definition again (signature,
  source, expiry, serial) before an install or update runs, so a
  compromised gateway or panel can't add an app or replay an old one. App
  records keep the source, serial and exact definition installed, plus the
  previous ones for a rollback.
- Definition updates: an app whose source has a newer serial (new ports,
  settings or pin) shows on the Updates page as a new definition.
- `cirrusstore`, the publisher tool: `keygen`, `sign`, `index` and
  `verify`. `make store-site KEY=…` builds the official catalogue from the
  built-in apps.
- Source badges on apps, search on the CirrusStore page, and apps from two
  sources with the same id listed with the reason one is unavailable.
- Docs: APPS.md is now "Publishing to CirrusStore" (envelopes, serials,
  expiry, hosting your own source), SECURITY.md describes the trust model,
  and CONFIG.md the store settings.

### Changed

- The app store is called CirrusStore wherever people see it. Routes,
  packages and settings keep their names.
- An official definition with the same id as a built-in one replaces it
  once the catalogue has been checked; the built-in apps stay as the
  offline fallback.

## [0.5.2] - 2026-11-03

### Added

- Idle: an `IGNORE_CLIENTS` peer that streams more than
  `SLEEP_IGNORE_CLIENTS_PLAY_MB` a minute counts as playback, from the
  conntrack byte counters (setup switches on `nf_conntrack_acct`). After a
  wake that only an ignored client caused, the server sleeps again
  `SLEEP_IGNORE_WAKE_GRACE_MIN` minutes later. IPv6 connections are read
  too, and traffic that reaches a container through the host (Podman's
  bridge, Tailscale serve) counts.
- Backups: `BACKUP_REQUIRE_MOUNT` makes the unit wait for that mount and the
  script refuse to write when the folder isn't on it, so a missing disk
  never fills the system disk.
- Router: `ROUTER_PROXY` (socks5h, socks5 or http) for a router reached
  through a proxy, and `cirrusux-router-wake`, a wake through the
  FRITZ!Box's TR-064 Wake-on-LAN request (`CIRRUS_WAKE=command`).
- The gateway runs without systemd: `-env FILE` reads its settings the way
  `EnvironmentFile` does, and `deploy/gateway/runit/` has a runit service
  with svlogd. Setup detects runit (`CIRRUS_INIT`) and leaves systemd-only
  steps out.
- `APPS_MEDIA_UID`/`APPS_MEDIA_GID` create the apps user with the media
  files' owner. `NET_TAILSCALE_SET` applies `tailscale set` preferences
  (undo restores the old ones) and `NET_TAILSCALED_FLAGS` sets tailscaled's
  flags.
- Optional modules: `ssh` (restrict SSH to `SSH_ALLOW`, with the firewall's
  confirm-or-rollback), `ftps` (pure-ftpd with TLS only and a virtual user;
  `cirrusux-setup ftps-passwd` sets the password without storing it), and
  fail2ban settings (`SEC_FAIL2BAN_*`). The sleep module adds a resume
  service and `SLEEP_NO_WAKEUP_DEVICES`.
- Apps: tinyMediaManager and MusicBrainz Picard, with writable media.
  Jellyfin's Trakt 33 plugin is checked on the pinned image.
- An example profile: example settings for every component and an
  install guide for the reinstall.

### Security

- The root helper takes the app request the agent names (`app-job <id>`)
  and reads it only through a handle on its own folder, so a link there
  can't make root read or delete other files; a request the helper never
  got to is removed instead of running with a later job. Replace
  `/etc/sudoers.d/cirrusux` when upgrading (`cirrusux-setup apply` does).
- The gateway keeps its files in `/var/lib/cirrusux-gateway`. Its systemd
  state folder was `/var/lib/cirrusux`, which systemd handed to the
  gateway's user although setup and the root helper keep their own state
  there. Setup moves an older gateway's files and updates gateway.env.
- The agent's token never appears on a command line while setup writes it.
- The firewall's SSH lockout guard now works under sudo and run0 (which
  drop `SSH_CONNECTION`): it also looks at the session's parent processes
  and the logged-in SSH sessions. The tailnet counts as 100.64.0.0/10,
  not any 100.x address.
- The Docker filter refuses a POST with a body, or one naming an API
  version before 1.24. The registry client follows the next page of tags
  only on the registry's own host.
- Setup warns when root would run CirrusUX from a folder another user can
  change.

### Fixed

- Two jobs for the same app no longer run at once: the second waits, then
  is checked against what the first left.
- An update whose rollback couldn't put the app's folders back says so, and
  where the backup is, instead of starting the old version on half its data.
- A job killed on the way (out of memory, stopped, past its time limit)
  shows as failed instead of running for ever; jobs now have a real time
  limit (`RuntimeMaxSec`).
- The firewall: its rollback is set up before the new rules load; a second
  apply before confirming goes back past both; a manual rollback becomes
  what loads at boot; a changed boot unit is reloaded.
- fail2ban restarts when its jail changes. Setup installs sudo when it's
  missing, refuses paths and addresses the helper would reject (which broke
  every module), and points the Docker filter back at Docker when
  `APPS_RUNTIME` changes from podman.
- The router check works with a password containing `"` or `\`.
- The daily digest counted one wake too many, mentioned backups without the
  backup module, and called the disk "Media disk".
- A server whose zone has no name shows as UTC+hh:mm; an unknown zone falls
  back to UTC everywhere, not only in the browser.
- The agent no longer reports a slow cycle as a resume, and no longer races
  on its Docker client.
- In the panel: the install form no longer resets every poll; the sleep
  timeline is cut at midnight; Follow stops when the server sleeps; Wake
  isn't offered without a wake method; Undo of an app action loads the
  catalogue first; a gateway that answers 5xx shows as unreachable instead
  of demo data; picking a theme keeps the wallpaper's glass; a failed save
  is reported as one; app tiles say "no reports" or "waking" when that's
  the state.
- Leftovers from the owner's server: the default Media board's note, a
  fixed memory total on Services, the assistant's note about a USB disk,
  and the Media page's prototype text.

### Changed

- Duplicated code is shared: one atomic file writer (`internal/fsutil`,
  replacing four, three of which could collide on a fixed temporary name),
  the unit and job-ID patterns (`helperapi`), the helper's job launcher,
  the setup modules' "start this unit" step, and one size formatter and one
  set of power-state labels in the UI.
- No source file may be over 400 lines: `make test` (and so CI) checks it.
  The five that were longer are split by what their parts do: the gateway's
  wake, router check, command queue and app-store helpers, the app jobs'
  update, removal and records, and the agent's Docker, system and report
  readers each have a file of their own. No behaviour changed.

## [0.5.1] - 2026-11-02

### Added

- Licence: the GNU AGPL 3.0 or later (`LICENSE`), shown in Settings → About.
- CI on GitHub Actions: `make test`, the race detector, the host script
  tests and the build, on every push and pull request.
- `SECURITY.md`, `CONTRIBUTING.md` and issue templates.
- `docs/APPS.md`: how to write an app definition for the store.

### Changed

- Removing an app also removes its downloaded images (the current one and
  the one kept for going back), and an update removes the image that falls
  out of use; a failed update removes the version that didn't work. Its
  folder still goes to the archive. Images hold none of the owner's data,
  and a reinstall downloads them again.
- Setup keeps one previous copy of each program it updates, in
  `/var/lib/cirrusux/previous/`, instead of a dated `.bak-` copy beside it
  on the PATH every time. Config and unit files still keep dated copies.

- `docs/INSTALL.md` walks through both setups from a fresh machine, and
  explains each `cirrusux-setup` module. `docs/CONFIG.md` adds `sleep.env`
  and `router.env`.

### Fixed

Found by installing on clean Debian 13 VMs, in both setups:

- Setup refreshes the package lists before its first install: a fresh cloud
  image has none, so every module that installs a package failed.
- The agent couldn't reach the Docker filter: systemd made the socket's
  folder `/run/cirrusux` root-only.
- The Docker filter couldn't start on a Podman server (its sandbox named
  `/run/docker.sock`). Setup now restarts the filter's socket after changing
  it, which also clears a failed socket.
- Podman apps couldn't start: Debian's Podman needs nftables, which setup
  didn't install (it skips "recommends"). The apps module installs it and
  aardvark-dns; the firewall module installs nftables itself.
- OS updates held kernel security fixes back for ever: a new kernel is a new
  package, and `apt-get upgrade` never adds one. Upgrades now use
  `--with-new-pkgs` (still never removing anything).
- "A restart is needed" stayed after the restart.
- `cirrusux-setup` wasn't on the PATH, though its messages say
  `sudo cirrusux-setup …`; the cirrusux module now links it there.
- The firewall's checks reached the Security page only after a second setup
  run; the firewall module now writes them when its ruleset is confirmed.
- No backups yet, no virus scan yet and a machine without temperature
  sensors were reported as errors.
- Backups card: the next run was unknown on a server that doesn't sleep,
  backups under 1 GB showed as "0 GB", a new server showed a red warning
  before its first backup, and an unencrypted backup showed a leftover date.
- A new gateway showed the demo's sample data until the server's first
  report, with "as of 01:00" on its widgets.
- The wake confirmation described a router and a command whatever the wake
  driver was; Allow sleep always said "30-minute".
- The command palette offered sleep controls on a server that doesn't sleep,
  and the Apps widget was blank with no apps installed.

### Removed

- A stray `helper` binary and the local `.claude/` preview config from the
  repository.

## [0.5.0] - 2026-11-02

### Added

- A router power check for the split setup (Phase 6d, optional): the gateway
  asks a TR-064 router (a FRITZ!Box, for one) whether it sees the server,
  every `CIRRUS_POWER_CHECK_SECS` (60), through `cirrusux-router-check` and
  one sudoers line. The router password stays in a root-only file and never
  reaches the gateway. It makes the power state clearer when Tailscale
  can't tell, and points at Tailscale when the router sees the server but
  Tailscale doesn't.
- Custom widgets: a title and up to six values from a fixed list, as tiles
  or a list, set up in Edit layout.

- The Storage page (Phase 6c): "Measure now" sizes the media, apps, backup
  and archive folders (and any `STORAGE_ROOTS`) as a job, with each disk's
  free space; the result is kept. The Storage widget shows it.
- Archive review: removed apps' folders, backups taken before updates and
  system backups, each with its size and date. Deleting one is the only
  deletion in CirrusUX and takes two steps: a review of exactly what goes,
  then typing its name. The gateway and the helper both check the name is
  in the archive the server last reported.
- Sudoers: `storage-scan` (read) and `archive-delete *` (action; the helper
  checks the name).

- OS updates, on request (Phase 6b): "Check now" refreshes the package
  lists and lists what's upgradable (security updates marked); "Install
  updates" runs `apt-get upgrade`, keeping changed config files and never
  removing or adding packages. Both run as jobs on the server with live
  progress, and a needed restart is shown. The confirmation says plainly
  that package upgrades can't be undone from the panel.
- The Logs page: recent lines of any container (through the Docker filter,
  which allows the logs endpoint but never `follow`) or of CirrusUX's own
  units (`cirrusux-helper journal`), "Follow" for up to 5 minutes, and
  "Explain this" sending the lines you pick to the assistant, redacted.
- Sudoers: `journal *` (read; the helper checks the unit name), `os-check`
  and `os-upgrade` (actions). Replace `/etc/sudoers.d/cirrusux` when
  upgrading.

- App updates, on request (Phase 6a). "Check now" on the new Updates page
  asks the registries from the gateway (standard registry API, anonymous
  tokens; a sleeping server isn't woken) and offers the newest release of
  the same shape as each app's pinned version: same suffix, never betas or
  `:latest`. An update is a job: download → back up the app's settings and
  data → restart on the new version → health check → record. If the new
  version doesn't answer, the previous version and its data come back by
  themselves. The gateway only lets an update move to a version the check
  found, or back to the previous one; each update's undo goes back.
- Installed apps' records keep their digest and the version before the
  last update.
- A live update test, `go test -tags live -run TestLivePodmanUpdate
  ./internal/appjob`: installs Syncthing 2.1.6 on rootless Podman, updates
  it to 2.1.7 with its data intact and backed up, rolls it back, removes it.

### Fixed

- A finished update's status said the app was removed.
- Confirmations for actions with no undo no longer mention an undo command.

### Changed

- Podman apps are started with `systemctl restart`, so an update moves a
  running app to its new unit.

## [0.4.0] - 2026-11-02

### Added

- `cirrusux-setup` (roadmap Phase 4, first part): re-runnable host modules
  with a check mode that reports what apply would change. Every change is
  logged with its undo and replaced files keep a dated copy.
  - base: the cirrusux user, time zone, no automatic updates.
  - firewall: an nftables table of its own, LAN and tailnet only, closing
    containers' published ports to everything else. A new ruleset rolls
    back by itself unless confirmed, setup refuses one that would cut off
    the SSH session running it, and every ruleset is kept.
  - cirrusux: binaries, units, a visudo-checked sudoers rule and config
    files (created once, with a shared token in the single setup); services
    start once their config has no placeholders left.
- More `cirrusux-setup` modules (Phase 4b):
  - network: Wake-on-LAN kept on across reboots (a `.link` file that keeps
    the card's name), tailscaled started at boot, login reported.
  - backup: `cirrusux-backup`, tar + zstd encrypted with age to a public
    key, written as `.partial` and renamed only when complete; a timer that
    wakes a sleeping server; `BACKUP_KEEP` retention (0 keeps all).
  - security: fail2ban's `sshd` jail on the journal, a weekly ClamAV scan
    that updates its signatures just before it runs (no daemon).
- The App store page (Phase 5c): browse and search the catalogue; each
  app's page shows exactly what it can reach (ports, its own folders, the
  media folders you choose and whether it can write there, devices, its
  memory needs) with your choices; the confirmation shows the exact file
  the server will write, rendered by the gateway with the server's own
  settings; installs and removals show live progress. App tiles come from
  the app store's records. Demo mode has the full catalogue and simulated
  jobs.
- `cirrusux-helper apps-info` (read-only, in sudoers) and
  `POST /api/apps/preview`.
- The dev server's proxy now passes the gateway's same-origin check, so
  actions work through `npm run dev`.
- App-store installs and removals as background jobs (Phase 5b). The
  gateway checks the app and the options' shape; the agent writes the
  request as a file in its own folder (built from parsed parts, so options
  can't carry an action or app of their own) and runs one fixed sudoers
  line, `cirrusux-helper app-job`; the helper checks everything again
  against its built-in catalogue and the server (free ports, existing media
  folders) and starts a `cirrusux-job-*` unit, which keeps a sleeping server
  awake. The job creates the app's folders, pulls the pinned image, writes
  the compose file or Quadlet unit, starts it and waits for its health
  check, stopping it again if that fails. Removing stops it and archives
  its folder. Jobs and installed apps are in the agent's reports.
- The `apps` setup module: Podman (default) or Docker, folders, the
  `cirrusux-apps` user, and with Podman the Docker filter pointed at its
  API socket, so the panel's containers work on either runtime.
- Ports can be published on one address only (`APPS_BIND`).
- Containers pulled by digest show their version from the app store's
  label (or the digest), not `:latest`.
- A live test, `go test -tags live ./internal/appjob`, installs and removes
  Syncthing with rootless Podman for real.
- The app store's catalogue (Phase 5a): nine app definitions (Jellyfin,
  Navidrome, Audiobookshelf, Kavita, Nextcloud, Home Assistant, Syncthing,
  Vaultwarden, Uptime Kuma), each pinned to a release tag and digest. A
  strict loader refuses unknown fields (so no privileged mode or host
  networking), `:latest`, missing digests and devices off a short
  allow-list. The owner's choices are checked (ports, media folders inside
  the allowed roots and read-only by default, env choices, optional
  devices), and one definition renders to a Docker compose file or a
  Podman Quadlet unit, both checked against the real tools.
- `cirrusux-idle`, CirrusUX's own sleep manager (Phase 4c), and the `sleep`
  setup module that installs it on a split setup's server. Once a minute it
  counts inbound connections (and, through conntrack, connections to
  containers' published ports), logins, busy units and stay-awake holds;
  outbound connections never count. It suspends after the quiet time with
  a grace period after each wake, records wakes through a systemd sleep
  hook, and writes the state files and log the agent already reads.
  Anything it can't read counts as activity, and `--dry-run` writes
  nothing. It also answers as `cirrusux-stay-awake` and
  `cirrusux-allow-sleep` for the panel's stay-awake buttons.
- The agent splits activity reasons on `; ` too, and reads
  `/etc/cirrusux/sleep.env` by default.
- The helper reads `/etc/cirrusux/helper.d/*.env` after `helper.env`, so
  setup modules switch panel modules on without touching the owner's file.
- `docs/PHASES-4-6.md` (now `docs/history/`): the build plan for phases 4–6.
- `make test-host`: shellcheck plus each module tested as root in a private
  namespace, never on the real machine.
- The helper can check nftables chains (`family/table/chain:pattern`).
- A read-only capture script in the private profile: a copy of the old server's own
  scripts and settings before the reinstall.

### Changed

- The power card in the single setup shows the server's vitals (containers
  running, disk, CPU temperature, last report) instead of standing empty.
- The assistant's starter questions follow the server's modules and setup.
- Test fixtures use generic names instead of a real server's.

### Fixed

- App tiles cut a four-letter suffix off any container name (`cirrus-test`
  became "Cirrus"); only the old panel's own names get that treatment now.

### Tested

- The single setup on a second machine with real data and Podman's
  Docker-compatible API behind the filter: containers, disk, temperature
  and a confirmed restart all work, and the filter refuses a delete.

## [0.3.0] - 2026-11-02

Fixes from the full code review of 0.2.0, a new roadmap, and its Phase 3:
CirrusUX now works for any server, not just one particular server.

### Changed (Phase 3: generalise the core)

- Everything that named one particular server and gateway now comes from config: server
  and gateway names, the host apps open on, the time zone (shown with its
  own abbreviation) and a description for the assistant. See docs/CONFIG.md.
- Two setups: `single` (everything on a server that doesn't sleep) and
  `split` (a sleeping server beside an always-on gateway, as before).
  `CIRRUS_SETUP` defaults to `single`.
- Wake methods: Wake-on-LAN sent by the gateway itself (`CIRRUS_WAKE=wol`)
  or a command (`command`, as before).
- Sign-in: Tailscale as before, or a trusted header from a reverse proxy
  (`CIRRUS_AUTH=header`). The identity header is now only believed from
  `CIRRUS_TRUSTED_PROXIES`.
- The assistant has presets for xAI, OpenAI, Anthropic and
  Ollama (`ASSISTANT_*` settings replace `XAI_*`; the digest switch is now
  `CIRRUS_DIGEST_ASSISTANT`). The API moved to `/api/assistant`; saved boards
  with the old assistant widget carry over.
- The root helper reads its paths from `/etc/cirrusux/helper.env` (root-owned)
  instead of built-in homeserver paths, and reports which modules the server
  has: sleep, stay-awake, backup, virus scan, security, egress. The panel
  hides pages and widgets for modules a server doesn't have.
- Firewall checks are a configurable list instead of the two homeserver
  chains; the backup unit, idle files and port names are configurable too.
- App tiles come from the containers that publish a port, instead of a fixed
  list.
- `deploy/` is split into `server/`, `gateway/` and a private profile
  folder; docs/INSTALL.md is now a general guide.

### Fixed

- The agent leaked a Docker connection on every collection: it built a new
  HTTP client per call, and the Docker filter never closed idle connections.
  It now reuses one client (idle connections close after 30 s), and the
  filter closes connections idle for 60 s.
- An action whose result report failed once was never logged. The agent now
  retries the report for about a minute.
- The egress report could be cut off by the helper's 30 s limit; it now gets
  150 s. The gateway waits 4 minutes for a result, and the UI waits 5.
- The gateway exited without waiting for requests in flight to finish, and
  the agent's long poll could hold up a shutdown.
- A wake is logged as "Sent a wake to homeserver" when asked for, plus a
  "(failed)" entry with the error if the wake command fails, instead of
  "Woke homeserver" whether or not it worked.
- The assistant's disk sizes are no longer rounded down to whole GB.
- Settings changed while the gateway is unreachable are kept and saved when
  it's back (retried every 30 s and when the browser comes online), instead
  of being lost without a word. The service worker also updates its saved
  copy of the settings after each save, so a reload while offline no longer
  starts from an older version whose first save is then refused as a
  conflict (found while testing this fix).
- The accent contrast fix could push a colour the wrong way on mid-tone
  backgrounds (possible in a custom theme) and never reach 4.5:1. It now
  heads towards whichever of white or black can contrast more. Found by
  the new contrast tests.
- Settings → Layouts said "in the real panel" in live mode too.
- Failed assistant replies are no longer sent back to the assistant as its own answers.
- The digest and the wallpaper now work offline.

### Changed

- The 08:00 digest only asks the assistant for a summary when `CIRRUS_DIGEST_ASSISTANT=1`.
  By default the assistant is called only when you use it.
- Wallpaper images are stored on the gateway on their own
  (`GET`/`PUT /api/wallpaper`: PNG, JPEG, WebP, GIF or AVIF up to 5 MB,
  checked from the bytes; no SVG), not inline in the settings, which every
  change re-sent and the 20 kept versions each copied. Demo mode says the
  image isn't kept after a reload.
- The audit log keeps its last 200 entries in memory instead of re-reading
  the whole file for every request. Past 5 MB, `audit.jsonl` is archived as
  `audit-YYYYMMDD-HHMMSS.jsonl` (never deleted).
- App tiles come from `web/src/lib/apps.ts`, matched to containers by app
  name; `lib/fake` is now `lib/demo` and holds only demo data.
- The CSP comes from the gateway's header only. The dev server sends its own,
  with the websocket hot reload needs; the copy in `index.html` is gone.
- PLAN, README, INSTALL and the Docker filter units no longer describe the
  helper and filter as read-only or tell you to run
  the old idle checker's `--dry-run`. INSTALL has upgrade steps from 0.2.0.

### Added

- `docs/ROADMAP.md`: CirrusUX becomes a general panel for any server, with
  host setup modules, an app store for Docker and Podman, and a public
  release; the first server's setup becomes a private profile and is reinstalled from scratch.
- Tests: Docker client reuse, result retries, audit memory and rotation,
  UI-history pruning, the wallpaper endpoint, the helper's pure functions,
  the digest opt-in, and (with Node's built-in runner, no new dependencies)
  contrast maths and container-to-app names. `make test` runs them all.

### Known

- Dependabot reports a low-severity advisory in `deepmerge`, pulled in at
  build time only by `@sveltejs/vite-plugin-svelte`. No fixed version exists
  yet; nothing from it ships in the UI.

## [0.2.0] - 2026-11-01

### Added
- Phase 2 actions, each confirmed in the panel and logged with who asked:
  stay awake (30 min, 2 h, until allowed to sleep), allow sleep, start,
  stop and restart a container, run a backup now.
- Command channel: the agent fetches actions over an outbound long poll,
  checks them against its own list and reports results; homeserver still opens
  no port.
- Undo on changelog entries that have a safe inverse.
- Security page: ClamAV, firewall lockdown, fail2ban, and an on-demand
  outbound-traffic (egress) report.
- Daily digest at 08:00 UK from cached data, with an assistant summary when
  configured, and a Daily digest widget.
- The assistant can propose any of the actions; the gateway drops proposals that
  couldn't run, and you confirm the rest.

### Changed
- Container actions and backups keep homeserver awake for at least 10 minutes
  (decision D3).
- The sudoers rule is split into read and action parts; remove the action
  part to keep the panel read-only.

### Fixed
- The UI dropped the body of 202 replies, which actions use for their ID.
- The settings-conflict reply was sent without its JSON content type.

## [0.1.1] - 2026-11-01

### Added
- Installable app (PWA): web manifest, app icons (regular, maskable and
  Apple touch, rendered by `scripts/make-icons.sh`) and a service worker.
- Offline mode: the panel opens without a connection and shows the last data
  the gateway sent, with an "Offline · saved data" badge, a dated notice on
  the power card and "as of" labels. No countdown and no wake while offline.
  Only reads are saved; anything that changes state always goes to the
  network.

## [0.1.0] - 2026-11-01

### Added
- Phase 0.5 design prototype: Svelte 5 UI with four themes, a theme editor
  with contrast checks, editable boards with per-device layouts, a widget
  catalogue, command palette, the assistant panel and a component gallery.
- Phase 1 read-only panel:
  - gateway: serves the UI, caches agent snapshots, tracks
    power state from Tailscale, wakes homeserver, stores UI settings with
    versions, keeps an audit log, and runs the assistant over cached data with
    redaction and a daily token cap;
  - agent (homeserver): pushes snapshots, opens no listening port;
  - root helper and Docker filter with fixed, read-only access;
  - systemd units, sudoers rules and example env files.
- Live mode in the UI, with demo data when no gateway answers.
- Versioning: `VERSION` file, `make bump`, versions stamped into every
  binary (`-version`), reported by the agent and gateway, and shown in
  Settings → About with a mismatch warning.

### Changed
- Project renamed to CirrusUX.
- Parsers fitted to the real log formats of the first server.
- The agent reads the idle checker's state files; it never runs
  the old idle checker's `--dry-run`, which writes state.
- Waking goes through `cirrusux-wake`, which reads the router password from
  a root-only file.

### Fixed
- The gateway no longer reports homeserver as asleep while the gateway itself is
  offline from Tailscale, and the agent retries failed pushes after a minute.
