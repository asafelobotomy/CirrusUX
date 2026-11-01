# Installing Phase 1

Phase 1 only reads and wakes. Nothing here changes how cloud64 sleeps, its
firewall, Tailscale or any app. Every step lists its undo.

Do the Phase 0 checks first (§0). They are read-only.

## 0. Checks before installing (read-only)

Run these on cloud64 and paste the output back, so the parsers can be
confirmed against real data (they were written from the brief, which didn't
show exact log formats):

```bash
sudo cloud64-idle-check --dry-run
sudo ls -l /run/cloud64-idle/ /run/stay-awake 2>/dev/null; sudo head -c 400 /run/cloud64-idle/* 2>/dev/null
sudo tail -n 30 /var/log/cloud64-idle.log
sudo journalctl -u cloud64-backup.service -n 20 -o short-iso --no-pager
sudo tail -n 15 /var/log/clamav/weekly-scan.log
ls -l /etc/cloud64-idle.conf
systemctl list-timers --all --output=json | head -c 300
```

On cloud-box:

```bash
tailscale status --json | head -c 300
sudo -u <admin-user> -n true && echo sudo-ok
grep -n ROUTER_PASSWORD ~<admin-user>/.profile ~<admin-user>/.bashrc ~<admin-user>/.config/environment.d/* 2>/dev/null
```

The last line matters: the gateway runs `cloud64-wake` through
`sudo -u <admin-user>`, which starts with a clean environment. If the router
password is only exported by the admin user's login shell, the script won't see it.
Phase 1 then needs a small wrapper that loads it (decide together; the
password itself must never pass through the panel).

The agent opens **no listening port** in Phase 1, so no firewall change is
needed on cloud64. It only connects out to cloud-box over the tailnet.

## 1. Build

On this machine:

```bash
make test
make build        # bin/c64panel-{gateway,agent,helper,dockerfilter}
```

Static binaries, no runtime dependencies. Copy them with `scp`.

## 2. cloud64

| Step | Command | Undo |
|---|---|---|
| User | `sudo useradd --system --no-create-home --shell /usr/sbin/nologin c64panel` | `sudo userdel c64panel` |
| Binaries | `sudo install -m 755 c64panel-agent /usr/local/bin/` and `sudo install -m 755 c64panel-helper c64panel-dockerfilter /usr/local/sbin/` | `sudo rm` the three files |
| sudoers | `sudo install -m 440 sudoers-c64panel /etc/sudoers.d/c64panel && sudo visudo -c` | `sudo rm /etc/sudoers.d/c64panel` |
| Env | `sudo install -d -m 750 -o root -g c64panel /etc/cloud64-panel`, then create `agent.env` from the example, mode 600, owner c64panel | `sudo rm -r /etc/cloud64-panel` |
| Units | copy the three unit files to `/etc/systemd/system/`, `sudo systemctl daemon-reload` | `sudo rm` them, daemon-reload |
| Start | `sudo systemctl enable --now c64panel-dockerfilter.socket c64panel-agent.service` | `sudo systemctl disable --now …` |

Check: `sudo -u c64panel c64panel-agent -once | head -40` prints a snapshot;
its `errors` list should be empty. `sudo cloud64-idle-check --dry-run`
should show no new activity reasons.

## 3. cloud-box

| Step | Command | Undo |
|---|---|---|
| User | `sudo useradd --system --no-create-home --shell /usr/sbin/nologin c64panel` | `sudo userdel c64panel` |
| Binary | `sudo install -m 755 c64panel-gateway /usr/local/bin/` | `sudo rm /usr/local/bin/c64panel-gateway` |
| sudoers | `sudo install -m 440 sudoers-c64panel-wake /etc/sudoers.d/c64panel-wake && sudo visudo -c` | `sudo rm /etc/sudoers.d/c64panel-wake` |
| Env | create `/etc/cloud64-panel/gateway.env` from the example, mode 600, owner c64panel | `sudo rm -r /etc/cloud64-panel` |
| Unit | copy `c64panel-gateway.service`, daemon-reload, `sudo systemctl enable --now c64panel-gateway` | disable, remove, daemon-reload |
| Tailnet | `sudo tailscale serve --bg --https=443 http://127.0.0.1:8064` | `sudo tailscale serve --https=443 off` |

Open `https://cloud-box.<tailnet>.ts.net/` from a tailnet device. Logins not
in `C64_ALLOWED_USERS` get 403.

## 4. After 24 hours

Check `/var/log/cloud64-idle.log` on cloud64: no extra wakes, no delayed
suspends. That's Phase 1's done criterion (PLAN §6).
