# Installing CirrusUX

CirrusUX has four parts: the **gateway** (serves the panel), the **agent**
(reports on the server and runs confirmed actions), the **helper** (root
reads and actions, through one sudoers rule per command line) and the
**Docker filter** (lets the agent see containers and start, stop or restart
them, nothing more).

## Pick a setup

- **single:** everything runs on the server, which stays on. The simplest.
- **split:** the server sleeps when nobody uses it, and the gateway runs on
  a small always-on machine beside it (a Raspberry Pi, a NAS, a router that
  runs Linux). The panel answers from the server's last report while it
  sleeps, and can wake it.

The agent never listens on a port: it pushes reports to the gateway and
fetches actions over its own outbound connection.

## Before you start

- **The server:** Debian or Ubuntu, with systemd. `cirrusux-setup` uses
  apt; the manual steps work on any systemd Linux.
- **Split setup:** an always-on Linux machine for the gateway, on the same
  LAN as the server if it should wake it with a magic packet, and
  Wake-on-LAN switched on in the server's BIOS.
- **Sign-in:** [Tailscale](https://tailscale.com) on the gateway machine
  (the easiest), or a reverse proxy that signs people in (Authelia,
  Authentik, oauth2-proxy). The panel has no passwords of its own and is
  never opened to the internet.
- **To build it:** Go (the version in `go.mod`), Node.js 26 and make, on
  any machine. The binaries are static, so build once and copy them over;
  for another kind of CPU, e.g. a Raspberry Pi gateway, build with
  `GOARCH=arm64 make build`.

## Get CirrusUX

Until releases come with ready-made binaries, build it from source:

```bash
git clone https://github.com/asafelobotomy/CirrusUX.git
cd CirrusUX
make build        # bin/cirrusux-{gateway,agent,helper,dockerfilter,idle}
```

Copy the whole folder (or at least `bin/` and `deploy/`) to each machine,
e.g. to `/opt/cirrusux`.

## Install: the single setup

On the server, in the CirrusUX folder:

```bash
sudo install -D -m 644 deploy/host/host.env.example /etc/cirrusux/host.env
sudo nano /etc/cirrusux/host.env
```

Set at least:

```sh
HOST_MODULES="base cirrusux network backup security apps"
HOST_TZ=Europe/London                 # your time zone
CIRRUS_ROLE=single
CIRRUS_ALLOWED_USERS=you@example.com  # your Tailscale login
BACKUP_AGE_RECIPIENT=age1...          # see "backup" below, or leave the module out
```

Then look before you leap, and apply:

```bash
sudo deploy/host/cirrusux-setup check   # what it would change, nothing more
sudo deploy/host/cirrusux-setup apply
```

Publish the panel on your tailnet (see "Sign-in" below for a reverse proxy):

```bash
sudo tailscale serve --bg --https=443 http://127.0.0.1:8064
```

Open `https://<server>.<tailnet>.ts.net/` from a device on your tailnet.
When everything works, add `firewall` to `HOST_MODULES` and apply again
(see "The firewall" below), from a session that will still be allowed in.

## Install: the split setup

**On the server,** in `/etc/cirrusux/host.env`:

```sh
HOST_MODULES="base cirrusux network backup security apps sleep"
CIRRUS_ROLE=server
NET_WOL_IFACE=enp1s0                  # the wired card; `ip link` lists them
```

**On the gateway machine,** in its own `/etc/cirrusux/host.env`:

```sh
HOST_MODULES="base cirrusux"
CIRRUS_ROLE=gateway
CIRRUS_SERVER_NAME=homeserver         # the server's Tailscale name
CIRRUS_ALLOWED_USERS=you@example.com
```

Run `sudo deploy/host/cirrusux-setup check`, then `apply`, on both. The two
sides need a shared token, which setup can't make for two machines, so
their services wait for you. Make one:

```bash
head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'; echo
```

Put it in `CIRRUS_AGENT_TOKEN` in both `/etc/cirrusux/agent.env` (server)
and `/etc/cirrusux/gateway.env` (gateway). Then:

- in **agent.env**, set `CIRRUS_GATEWAY_URL` to
  `https://<gateway>.<tailnet>.ts.net/agent/snapshot`;
- in **gateway.env**, set `CIRRUS_WAKE=wol` and `CIRRUS_WAKE_MAC` to the
  server's MAC address (or another wake driver, below).

Publish the gateway with `sudo tailscale serve --bg --https=443
http://127.0.0.1:8064` on the gateway machine, and run `apply` again on
both: the services start once no `<…>` placeholder is left. Open
`https://<gateway>.<tailnet>.ts.net/`.

## What cirrusux-setup does

`deploy/host/cirrusux-setup` changes only what differs from your settings,
one module at a time, and can be re-run whenever you change `host.env`.

Every change goes to `/var/lib/cirrusux/setup.log` with its undo, and replaced
files keep a dated `.bak-` copy. Config files are created once (with a shared
token in the single setup) and then left to you; services start once nothing
in them is still a placeholder. `cirrusux-setup list` shows the modules.

The modules:

- **base:** the `cirrusux` user, the time zone, and apt's automatic
  upgrades off (updates run when you ask, from the panel).
- **cirrusux:** the binaries, units, sudoers rule and config files.
- **network:** Wake-on-LAN that survives reboots, and a check that
  Tailscale starts at boot.
- **backup:** age-encrypted system backups on a timer that wakes a sleeping
  server. Make the key pair on another machine (`age-keygen -o key.txt`)
  and put only the public key (`age1…`) in `BACKUP_AGE_RECIPIENT`: the
  server can write backups but never read them. Keep the private key safe
  and off the server, or the backups can't be restored.
- **security:** fail2ban for SSH, and a weekly virus scan.
- **apps:** the app store: Podman (or Docker), the apps' folders and user,
  and, with Podman, the Docker filter pointed at Podman's API socket.
- **sleep** (split setup, server only): CirrusUX's own sleep manager.
- **firewall:** below.

Each module switches its page on in the panel by itself. Every setting is
in `CONFIG.md`.

### The firewall

The **firewall** module (add it to `HOST_MODULES` when ready) writes an
nftables table of its own: LAN and tailnet only, and containers' published
ports closed to everything else. A new ruleset rolls back after two minutes
unless you run `sudo cirrusux-setup firewall-confirm`, and setup refuses a
ruleset that would cut off the SSH session running it.

## By hand

The steps below do what the `cirrusux` module does, for a system where
`cirrusux-setup` doesn't fit. Every step has its undo.

### The server

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

### The gateway

On the server itself (single) or the always-on machine (split).

| Step | Command | Undo |
|---|---|---|
| User | as above, if this is another machine | `sudo userdel cirrusux` |
| Binary | `sudo install -m 755 cirrusux-gateway /usr/local/bin/` | `sudo rm /usr/local/bin/cirrusux-gateway` |
| Config | from `gateway.env.example`, mode 600, owner cirrusux | `sudo rm /etc/cirrusux/gateway.env` |
| Unit | copy `deploy/gateway/cirrusux-gateway.service`, daemon-reload, `sudo systemctl enable --now cirrusux-gateway` | disable, remove, daemon-reload |

## Sign-in

Put a front end before the gateway, which signs people in:

- **Tailscale** (`CIRRUS_AUTH=tailscale`):
  `sudo tailscale serve --bg --https=443 http://127.0.0.1:8064`, then open
  `https://<machine>.<tailnet>.ts.net/`.
- **A reverse proxy with sign-in** (`CIRRUS_AUTH=header`): Authelia,
  Authentik or oauth2-proxy in front, passing the user in
  `CIRRUS_AUTH_HEADER`. Keep the gateway on 127.0.0.1 so nothing can reach it
  around the proxy.

Logins not in `CIRRUS_ALLOWED_USERS` get 403. Over HTTPS the browser offers
to install CirrusUX as an app.

## Waking (split setup)

- `CIRRUS_WAKE=wol` sends a magic packet from the gateway. The gateway must
  be on the server's LAN, and Wake-on-LAN must be on in the server's network
  card (and its BIOS).
- `CIRRUS_WAKE=command` runs a command of your own, e.g. a script that asks
  the router to wake it. Run anything that needs a secret through a small
  root wrapper and one sudoers rule, so the gateway never sees the secret.

## The router check (split setup, optional)

`deploy/gateway/cirrusux-router-check` asks a TR-064 router (a FRITZ!Box,
for one) whether the server is on the network. Install it like this:

1. `sudo install -m 755 cirrusux-router-check /usr/local/sbin/`
2. Put `ROUTER_URL`, `ROUTER_USER`, `ROUTER_PASSWORD` and `SERVER_MAC` in
   `/etc/cirrusux/router.env` yourself (root:root 600). The panel never
   handles the password.
3. Add its sudoers rule, `sudoers-cirrusux-router-check`, and set
   `CIRRUS_POWER_CHECK_CMD=sudo -n /usr/local/sbin/cirrusux-router-check`.

## The assistant and the daily digest

The assistant is off until `ASSISTANT_MODEL` is set (plus
`ASSISTANT_API_KEY` for hosted providers), and then it's only called when you
ask it something. The 08:00 digest is built from cached data and sends
nothing out, unless `CIRRUS_DIGEST_ASSISTANT=1`.

## Upgrading

Install the new binaries and unit files, `sudo systemctl daemon-reload`,
then restart: `cirrusux-agent` and `cirrusux-dockerfilter.socket` on the
server (and `sudo systemctl stop cirrusux-dockerfilter.service`, so the socket
starts the new filter), and `cirrusux-gateway`. Version-specific steps are in
CHANGELOG.md.
