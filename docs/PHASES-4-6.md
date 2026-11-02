# Phases 4–6: the detailed plan

ROADMAP.md says what each phase is for; this file says how it's built, in
the order it's built. Each part ends in something that runs and is tested,
and each lands as its own commit. The rules in ROADMAP §4 hold throughout:
changes are confirmed and logged with an undo, nothing is deleted outside
archive review, image versions are pinned, nothing updates by itself, and
nothing the panel does keeps a sleeping server awake.

## Phase 4 — host modules

`cirrusux-setup check|apply <module>`, one shell module per concern, each
re-runnable and tested as root in a private namespace (`make test-host`).

### 4a. Framework, base, firewall, cirrusux — done

### 4b. Network, backups, security — done

- **Module files for the helper.** The helper also reads
  `/etc/cirrusux/helper.d/*.env` (same rules: root-owned, known keys only),
  so a setup module can switch a panel module on by writing its own file,
  never editing the owner's `helper.env`.
- **network**
  - Wake-on-LAN that survives reboots: a systemd `.link` file with
    `WakeOnLan=magic` for the chosen interface (`NET_WOL_IFACE`), checked
    against `ethtool`.
  - Tailscale: `tailscaled` enabled at boot (the cloud-box lesson). Logging
    in stays manual (`tailscale up` needs a browser), so setup reports it
    rather than doing it.
- **backups**
  - `cirrusux-backup`: tar + zstd, encrypted with `age` to a public key
    (`BACKUP_AGE_RECIPIENT`); the private key never goes on the server.
    Folders from `BACKUP_PATHS` (default: `/etc /root /home /usr/local
    /var/lib/cirrusux /srv/cirrusux`), media never by default. Writes to
    `BACKUP_DIR` as `.partial`, renamed only when complete.
  - A service and a timer (`BACKUP_SCHEDULE`, `WakeSystem=yes`, so a
    sleeping server wakes for it and the idle check counts it as activity).
  - Retention `BACKUP_KEEP` (default 7). The one place anything is deleted
    automatically; `BACKUP_KEEP=0` keeps everything. Logged.
  - Writes `helper.d/backup.env`, so the panel's backup page works at once.
- **security**
  - fail2ban with the `sshd` jail; `helper.d/security.env` names it.
  - ClamAV: a weekly scan of `AV_PATHS` by a timer (`WakeSystem=yes`),
    logging to `/var/log/cirrusux/av-scan.log` in the format the agent
    already reads; `helper.d/av.env` names the log.
  - If the firewall module is on: a firewall check for the panel
    (`inet/cirrusux/input:drop`).
- Packages a module needs (age, zstd, fail2ban, clamav, nftables, ethtool)
  are installed by `apply` and listed by `check`; nothing else is ever
  installed or upgraded.

### 4c. The sleep manager — done

The panel already speaks the idle checker's language: a state folder
(`last-msg`, `last-active`, `last-wake`, `last-suspend`) and a log of
`ACTIVE`/`IDLE`/`SUSPEND`/`RESUMED` lines. CirrusUX's own checker writes the
same, so the agent reads it unchanged.

- `cirrusux-idle` (Go, root), run every minute by a timer:
  - activity: inbound TCP connections to watched ports (`ss`), login
    sessions, a stay-awake hold, running units that must finish (backups,
    scans, app jobs), and `IDLE_IGNORE` clients that don't count;
  - grace after a wake (`GRACE_MIN`), then idle for `IDLE_MIN` minutes;
  - suspends with `systemctl suspend`; systemd sets the RTC alarm for the
    next `WakeSystem` timer itself;
  - never counts the agent: its connections are outbound.
- `stay-awake` / `allow-sleep` built in (`cirrusux-idle hold 30|120|forever`,
  `cirrusux-idle release`), so the stay-awake module needs no outside scripts.
- A `sleep` setup module installs it and writes `helper.d/sleep.env`.
- `--dry-run` that really writes nothing (the cloud64 checker's didn't).
- Tests: the decision logic is pure Go with table tests; suspend is behind an
  interface.
- When the cloud64 capture arrives, its tuning is compared and folded in.

**Done when** (all of Phase 4): a clean Debian VM goes from install to a
firewalled, backed-up server that sleeps and wakes for its timers, by
`cirrusux-setup apply` alone, and the panel shows every module.

## Phase 5 — app store

### 5a. Catalogue and renderers (Go, no UI yet) — done

- `internal/catalogue/apps/<id>.json`, one file per app, built into the
  binaries; JSON so Go reads it with the standard library. Fields: name, description, icon, colour, group, website;
  image, tag, digest; ports (container port, default host port, protocol,
  https); volumes (config, media read-only by default, cache); devices
  (`/dev/dri`); extra groups; environment (fixed values, or a choice from a
  list); health check; minimum resources.
- `internal/catalogue`: load and validate (pinned tag and digest required,
  `:latest` refused, no privileged mode, no host network, mounts only under
  allowed roots).
- Renderers: a compose file for Docker and a Quadlet `.container` unit for
  Podman, from the same definition and the owner's options. Golden-file tests.
- Starter set: Jellyfin, Navidrome, Audiobookshelf, Kavita, Nextcloud,
  Home Assistant, Syncthing, Vaultwarden, Uptime Kuma, each pinned to a real
  release tag and its digest from Docker Hub (2 Nov 2026). Immich needs
  several containers (server, machine learning, Postgres, Redis); it waits
  for multi-container definitions, after 5c.
- Output checked against the real tools: the compose file parses as YAML,
  and Podman's Quadlet generator turns the unit into the expected
  `podman run`.

### 5b. Installing, as background jobs — done

- The agent can't pass free text to root, so a request goes as a file: the
  agent writes `/run/cirrusux/requests/<job>.json` (app ID + chosen options),
  and the fixed sudoers line `cirrusux-helper app-job` makes the helper read
  it, check it against its own built-in copy of the catalogue, and start the
  job with `systemd-run` (so it outlives the call).
- Job steps, each reported in `/run/cirrusux/jobs/<job>.json`: render, pull
  (with progress), create the config folder under `/srv/cirrusux/apps/<id>`,
  write the compose file or Quadlet unit, start, wait for the health check.
  The agent includes running jobs in its reports; the server stays awake
  while one runs.
- Remove: stop, then archive the app's folder to `/srv/cirrusux/archive/`.
- Runtime from `CONTAINER_RUNTIME` in `helper.env` (docker or podman).
- Undo: an install's undo is removing it; a removal's folder waits in the
  archive (restoring it is Phase 6's archive review).
- Ports can be published on one address only (`APPS_BIND`: the LAN or
  tailnet address, or 127.0.0.1).
- Tested for real: `go test -tags live ./internal/appjob` installs and
  removes Syncthing with rootless Podman on the machine running it.

### 5c. The store in the UI — done

- An App store page: browse by group, search, an app page showing exactly
  what the app can reach (folders, devices, ports), options, a confirmation
  with the rendered file, and live progress.
- Installed apps get their tile and container row from the app store's own
  records, not the container name.
- The preview comes from the gateway (`POST /api/apps/preview`), made with
  the facts the server reports (`cirrusux-helper apps-info`): the same code
  and inputs the job uses. It also warns about ports another container
  already publishes.
- While a job runs the agent reports every ~15 s and the page polls every
  2 s, so progress shows live.
- Demo mode reads the same catalogue files at build time and simulates jobs.

**Done when:** an app installs, updates and is removed (archived) through
the panel on Docker and on Podman, and its tile appears without any naming
guesswork.

## Phase 6 — maintenance

In four parts, each its own commit.

### 6a. App updates, on request — done

- **Checking** happens on the gateway, only when asked, so a sleeping
  server isn't woken: for each installed app it lists the image's tags from
  its registry (the standard registry API, anonymous tokens for Docker Hub,
  GHCR and others), keeps those shaped like the pinned one (same suffix, no
  beta/rc/dev), and offers the newest higher version with its digest. The
  result is cached with the time it was checked.
- **Updating** is a job (`app_update`): back up the app's own folders
  (settings and data, not cache) → write the file with the new tag and
  digest → pull → restart → health check → record. If the health check
  fails, the job puts the previous file and folders back and restarts the
  old version by itself. The update's undo is an update back to the previous
  tag and digest.
- The helper accepts an update only for the catalogue's own image name;
  the tag and digest must be well-formed and never `latest`.
- An Updates page: installed apps with their version, "Check now", what's
  newer, and Update with the same confirmation and live progress as installs.
- Tested: version picking on real tag lists, the registry client against
  the real Docker Hub (`go test -tags live ./internal/registry`; its digest
  for Navidrome's pin matches the catalogue), and the restore path: a new
  version that breaks its data and never answers gets the old version and
  its data back by itself. And for real on rootless Podman: Syncthing 2.1.6
  → 2.1.7 with its data kept and backed up, then back to 2.1.6.

### 6b. OS updates and the Logs page — done

- Pending OS packages, checked on request (`apt-get update` then the list),
  and an upgrade run as a job. Nothing updates by itself.
- Per-container logs (the Docker filter allows the logs endpoint, but never
  `follow`) and the journal of CirrusUX's own units (`cirrusux-helper journal
  <unit>`, unit names checked against `cirrusux-….service`); "Follow"
  refreshes every 5 s and stops by itself after 5 minutes; "Explain this"
  sends the chosen lines (redacted) to the assistant.
- An OS upgrade is `apt-get upgrade` with the old config files kept: it never
  removes packages or adds new ones. It can't be undone from the panel, and
  its confirmation says so; a restart that's needed afterwards is shown.

### 6c. Storage and archive review — done

- Folder sizes measured on request as a job and cached, with a note of when.
- Archived app folders, pre-update backups and old system backups, with the
  only deletion in CirrusUX: two steps, the second showing exactly what goes.
- Measured folders: the media roots, the apps folder, the backup folder, the
  archive, and any `STORAGE_ROOTS` (`du -x`, with each disk's free space).
- Deleting takes an entry's exact name: it must be in the archive the server
  last reported (gateway and helper both check), the owner reviews it, then
  types it. Only names directly inside the archive or backup folders, of the
  kinds CirrusUX makes, can be deleted; a link goes as a link.

### 6d. Router power check and custom widgets

- A router power check as a wake-driver option (TR-064
  `GetSpecificHostEntry`), through a root wrapper so the gateway never holds
  the router password.
- Custom widgets (customisation level 5): declarative widgets built from the
  snapshot's data, no code.

**Done when:** one app update goes through backup → tag → pull → health →
changelog, and its restore path is tested once.
