# Installing CirrusUX

CirrusUX has four parts: the **gateway** (serves the panel), the **agent**
(reports on the server and runs confirmed actions), the **helper** (root
reads and actions, through one sudoers rule per command line) and the
**Docker filter** (lets the agent see containers and start, stop or restart
them, nothing more).

Pick a setup (docs/CONFIG.md):

- **single:** everything on the server, which doesn't sleep.
- **split:** the server sleeps; the gateway runs on an always-on machine.

The agent never listens on a port: it pushes reports to the gateway and
fetches actions over its own outbound connection.

## 1. Build

```bash
make test
make build        # bin/cirrusux-{gateway,agent,helper,dockerfilter}
```

Static binaries with no runtime dependencies. Copy them to the machines.

## 2. The server

| Step | Command | Undo |
|---|---|---|
| User | `sudo useradd --system --no-create-home --shell /usr/sbin/nologin cirrusux` | `sudo userdel cirrusux` |
| Binaries | `sudo install -m 755 cirrusux-agent /usr/local/bin/` and `sudo install -m 755 cirrusux-helper cirrusux-dockerfilter /usr/local/sbin/` | `sudo rm` the three files |
| Config folder | `sudo install -d -m 750 -o root -g cirrusux /etc/cirrusux` | `sudo rm -r /etc/cirrusux` |
| Helper config | from `helper.env.example`: `sudo install -m 644 -o root -g root helper.env /etc/cirrusux/` | `sudo rm /etc/cirrusux/helper.env` |
| Agent config | from `agent.env.example`, mode 600, owner cirrusux | `sudo rm /etc/cirrusux/agent.env` |
| sudoers | `sudo install -m 440 sudoers-cirrusux /etc/sudoers.d/cirrusux && sudo visudo -c` | `sudo rm /etc/sudoers.d/cirrusux` |
| Units | copy the three unit files from `deploy/server/` to `/etc/systemd/system/`, `sudo systemctl daemon-reload` | `sudo rm` them, daemon-reload |
| Start | `sudo systemctl enable --now cirrusux-dockerfilter.socket cirrusux-agent.service` | `sudo systemctl disable --now …` |

Check: `sudo -u cirrusux cirrusux-agent -once | head -40` prints a snapshot.
`features` lists the modules from `helper.env`, and `errors` should be empty.

The sudoers file has two parts: `CIRRUS_READ` and `CIRRUS_ACT`. Leave
`CIRRUS_ACT` off its last line to keep the panel read-only.

## 3. The gateway

On the server itself (single) or the always-on machine (split).

| Step | Command | Undo |
|---|---|---|
| User | as above, if this is another machine | `sudo userdel cirrusux` |
| Binary | `sudo install -m 755 cirrusux-gateway /usr/local/bin/` | `sudo rm /usr/local/bin/cirrusux-gateway` |
| Config | from `gateway.env.example`, mode 600, owner cirrusux | `sudo rm /etc/cirrusux/gateway.env` |
| Unit | copy `deploy/gateway/cirrusux-gateway.service`, daemon-reload, `sudo systemctl enable --now cirrusux-gateway` | disable, remove, daemon-reload |

Then put a front end before it, which signs users in:

- **Tailscale** (`CIRRUS_AUTH=tailscale`):
  `sudo tailscale serve --bg --https=443 http://127.0.0.1:8064`, then open
  `https://<machine>.<tailnet>.ts.net/`.
- **A reverse proxy with sign-in** (`CIRRUS_AUTH=header`): Authelia,
  Authentik or oauth2-proxy in front, passing the user in
  `CIRRUS_AUTH_HEADER`. Keep the gateway on 127.0.0.1 so nothing can reach it
  around the proxy.

Logins not in `CIRRUS_ALLOWED_USERS` get 403. Over HTTPS the browser offers
to install CirrusUX as an app.

### Waking (split setup)

- `CIRRUS_WAKE=wol` sends a magic packet from the gateway. The gateway must
  be on the server's LAN, and Wake-on-LAN must be on in the server's network
  card (and its BIOS).
- `CIRRUS_WAKE=command` runs a command of your own, e.g. a script that asks
  the router to wake it. Run anything that needs a secret through a small
  root wrapper and one sudoers rule, so the gateway never sees the secret.

### The assistant and the daily digest

The assistant is off until `ASSISTANT_MODEL` is set (plus
`ASSISTANT_API_KEY` for hosted providers), and then it's only called when you
ask it something. The 08:00 digest is built from cached data and sends
nothing out, unless `CIRRUS_DIGEST_ASSISTANT=1`.

## 4. Upgrading

Install the new binaries and unit files, `sudo systemctl daemon-reload`,
then restart: `cirrusux-agent` and `cirrusux-dockerfilter.socket` on the
server (and `sudo systemctl stop cirrusux-dockerfilter.service`, so the socket
starts the new filter), and `cirrusux-gateway`. Version-specific steps are in
CHANGELOG.md.
