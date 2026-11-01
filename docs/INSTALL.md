# Installing Phase 1

Phase 1 only reads and wakes. Nothing here changes how cloud64 sleeps, its
firewall, Tailscale or any app. Every step lists its undo.

Do the Phase 0 checks first (§0). They are read-only.

## 0. Checks before installing (read-only)

### Done 1 Nov 2026

The read-only checks ran on cloud64 and cloud-box. What they settled:

- **Log formats** for the idle log, the backup journal, the ClamAV log and
  the timers JSON are now covered by tests built from the real lines.
- **`cloud64-idle-check --dry-run` is not write-free.** The panel never runs
  it. The agent reads the checker's own state instead:
  `/run/cloud64-idle/last-msg` (its verdict, rewritten every minute),
  `last-active`, `last-wake` and `last-suspend` (unix seconds).
- **The router password is not in the admin user's shell environment**, only
  in the admin bot's process. `sudo -u <admin-user>` can't see it, so the
  gateway wakes cloud64 through `cirrusux-wake` (§3), which reads the
  password from a root-only file. You copy the value there yourself; it never
  passes through the panel.
- A `RESUMED` line can appear with no `SUSPEND` before it (19:08 that day).
  The timeline places that sleep from the `WAKE-DIAG` line's
  `slept ~Ns` when there is one, and otherwise leaves it out.

- **Tailscale on cloud-box** (checked 1 Nov): tailscaled runs as root in
  userspace mode with its socket at `/run/tailscale/tailscaled.sock`
  (`root:root 0666`), so the gateway's own user can call `tailscale status`.
  The gateway unit doesn't hide `/run`. Changing `tailscale serve` needs sudo
  (no operator is set), as §3 does. HTTPS certificates are enabled for the
  node; issuing one wasn't tested because it writes files.

### Before installing: tailscaled on cloud-box must survive a reboot

tailscaled on cloud-box was started by hand, not as a service. After a
reboot there's no tailnet, so no `tailscale serve`, no panel and no
reports from cloud64. Making it a service is a change to the admin box
itself, outside the panel, so it's your call. With the official package it's
usually the shipped unit, with userspace mode set in `/etc/default/tailscaled`:

```bash
systemctl cat tailscaled.service      # read-only: is a unit installed?
```

The node has also lost its connection to Tailscale's servers a few times and
recovered on its own. The panel copes with that:
- the gateway notices cloud-box is offline from the tailnet and stops trusting
  peer states (it won't call cloud64 asleep just because the link dropped),
  and the power card says so;
- the agent retries a failed push after a minute instead of five.

The agent opens **no listening port** in Phase 1, so no firewall change is
needed on cloud64. It only connects out to cloud-box over the tailnet.

## 1. Build

On this machine:

```bash
make test
make build        # bin/cirrusux-{gateway,agent,helper,dockerfilter}
```

Static binaries, no runtime dependencies. Copy them with `scp`.

## 2. cloud64

| Step | Command | Undo |
|---|---|---|
| User | `sudo useradd --system --no-create-home --shell /usr/sbin/nologin cirrusux` | `sudo userdel cirrusux` |
| Binaries | `sudo install -m 755 cirrusux-agent /usr/local/bin/` and `sudo install -m 755 cirrusux-helper cirrusux-dockerfilter /usr/local/sbin/` | `sudo rm` the three files |
| sudoers | `sudo install -m 440 sudoers-cirrusux /etc/sudoers.d/cirrusux && sudo visudo -c` | `sudo rm /etc/sudoers.d/cirrusux` |
| Env | `sudo install -d -m 750 -o root -g cirrusux /etc/cirrusux`, then create `agent.env` from the example, mode 600, owner cirrusux | `sudo rm -r /etc/cirrusux` |
| Units | copy the three unit files to `/etc/systemd/system/`, `sudo systemctl daemon-reload` | `sudo rm` them, daemon-reload |
| Start | `sudo systemctl enable --now cirrusux-dockerfilter.socket cirrusux-agent.service` | `sudo systemctl disable --now …` |

Check: `sudo -u cirrusux cirrusux-agent -once | head -40` prints a snapshot;
its `errors` list should be empty. `sudo cat /run/cloud64-idle/last-msg`
should show no new activity reasons (don't use `--dry-run`: it writes state).

## 3. cloud-box

| Step | Command | Undo |
|---|---|---|
| User | `sudo useradd --system --no-create-home --shell /usr/sbin/nologin cirrusux` | `sudo userdel cirrusux` |
| Binary | `sudo install -m 755 cirrusux-gateway /usr/local/bin/` | `sudo rm /usr/local/bin/cirrusux-gateway` |
| Wake wrapper | set `ADMIN_USER` in `cirrusux-wake`, then `sudo install -m 755 cirrusux-wake /usr/local/sbin/` | `sudo rm /usr/local/sbin/cirrusux-wake` |
| Router password | you, not the panel: `sudo install -m 600 -o root -g root /dev/null /etc/cirrusux/router.env`, then `sudoedit` it and add the one line `NAME=value` that `cloud64-wake` reads | `sudo rm /etc/cirrusux/router.env` |
| Wake test | `sudo /usr/local/sbin/cirrusux-wake` while cloud64 sleeps: exits 0 once SSH answers | — |
| sudoers | `sudo install -m 440 sudoers-cirrusux-wake /etc/sudoers.d/cirrusux-wake && sudo visudo -c` | `sudo rm /etc/sudoers.d/cirrusux-wake` |
| Env | `sudo install -d -m 750 -o root -g cirrusux /etc/cirrusux` (before the password step), then create `gateway.env` from the example, mode 600, owner cirrusux | `sudo rm -r /etc/cirrusux` |
| Unit | copy `cirrusux-gateway.service`, daemon-reload, `sudo systemctl enable --now cirrusux-gateway` | disable, remove, daemon-reload |
| Tailnet | `sudo tailscale serve --bg --https=443 http://127.0.0.1:8064` | `sudo tailscale serve --https=443 off` |

Open `https://cloud-box.<tailnet>.ts.net/` from a tailnet device. Logins not
in `CIRRUS_ALLOWED_USERS` get 403. The browser then offers to install
CirrusUX as an app (it needs the HTTPS that `tailscale serve` provides).

## 4. After 24 hours

Check `/var/log/cloud64-idle.log` on cloud64: no extra wakes, no delayed
suspends. That's Phase 1's done criterion (PLAN §6).
