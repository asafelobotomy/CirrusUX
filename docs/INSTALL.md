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
- **Only to build it from source:** Go (the version in `go.mod`), Node.js
  26 and make, on any machine. The binaries are static, so build once and copy them over;
  for another kind of CPU, e.g. a Raspberry Pi gateway, build with
  `GOARCH=arm64 make build`.

## Quick install

On each machine (the server, and in a split setup the gateway machine too),
download the installer, read it, and run it:

```bash
curl -fsSLO https://github.com/asafelobotomy/CirrusUX/releases/latest/download/install.sh
less install.sh
sudo bash install.sh
```

It picks the release for this CPU (amd64 or arm64) and checks it against the
release's `SHA256SUMS`. It unpacks the release to
`/opt/cirrusux/releases/vX.Y.Z`, which belongs to root, and points
`/opt/cirrusux/current` at it. Then it starts `cirrusux-setup init`, which
asks a few questions. Its answers come from the machine where it can: the
time zone, your Tailscale login, the wired network card. It writes
`/etc/cirrusux/host.env`, shows what it would change, and applies it once
you agree. Nothing changes before that.

- **Single setup:** answer `1`. With Tailscale installed and logged in,
  setup publishes the panel on your tailnet (`CIRRUS_TAILSCALE_SERVE=yes`).
  Open the address it prints.
- **Split setup:** answer `3` on the gateway machine and `2` on the server.
  Then connect the two:
  1. On the gateway, run `sudo cirrusux-setup pair`. It makes the shared
     token, wakes the server by magic packet unless you've set another way,
     and prints a join code.
  2. On the server, run `sudo cirrusux-setup join` and paste the code.

  The server reports its MAC address, so the gateway can wake it with
  nothing more to copy over. The code holds the token, so don't post it
  anywhere.

No internet on the machine? Download the `.tar.gz` for its CPU and
`SHA256SUMS` from the release page into one folder, then run
`sudo bash install.sh --from that-folder`. To check where a bundle was
built, run
`gh attestation verify cirrusux-vX.Y.Z-linux-amd64.tar.gz --repo asafelobotomy/CirrusUX`.

Add `firewall` to `HOST_MODULES` once everything works (see "The firewall"
below). Everything else is in the rest of this guide: every module, every
setting, and the manual steps.

## From source

Build it yourself to run an unreleased version, or for a CPU there's no
release for:

```bash
git clone https://github.com/asafelobotomy/CirrusUX.git
cd CirrusUX
make build        # bin/cirrusux-{gateway,agent,helper,dockerfilter,idle}
```

`make build` also produces `cirrusstore`, which is for catalogue publishers
only; you don't need to install it. `scripts/bundle.sh VERSION ARCH OUT`
makes the same bundle a release has.

Copy the whole folder (or at least `bin/` and `deploy/`) to each machine,
at a root-owned path such as `/opt/cirrusux`. Then run
`sudo deploy/host/cirrusux-setup init`, or write `host.env` yourself as
below.

## host.env by hand: the single setup

What `init` does, step by step. On the server, in the CirrusUX folder:

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

Publish the panel on your tailnet: set `CIRRUS_TAILSCALE_SERVE=yes` in
`host.env` before you apply, or run it yourself (see "Sign-in" below for a
reverse proxy):

```bash
sudo tailscale serve --bg --https=443 http://127.0.0.1:8064
```

Open `https://<server>.<tailnet>.ts.net/` from a device on your tailnet.
When everything works, add `firewall` to `HOST_MODULES` and apply again
(see "The firewall" below), from a session that will still be allowed in.

## host.env by hand: the split setup

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
sides need a shared token, so their services wait for it.
`cirrusux-setup pair` and `join` (see "Quick install") handle it for you. By
hand, make one:

```bash
head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'; echo
```

Put it in `CIRRUS_AGENT_TOKEN` in both `/etc/cirrusux/agent.env` (server)
and `/etc/cirrusux/gateway.env` (gateway). Then:

- in **agent.env**, set `CIRRUS_GATEWAY_URL` to
  `https://<gateway>.<tailnet>.ts.net/agent/snapshot`;
- in **gateway.env**, set `CIRRUS_WAKE=wol` (or another wake driver,
  below). The gateway wakes the server at the MAC address the agent
  reports; set `CIRRUS_WAKE_MAC` only to use another.

Publish the gateway with `sudo tailscale serve --bg --https=443
http://127.0.0.1:8064` on the gateway machine, and run `apply` again on
both: the services start once no `<…>` placeholder is left. Open
`https://<gateway>.<tailnet>.ts.net/`.

### A gateway machine without systemd (runit)

The gateway also runs on a Debian machine without systemd, under
[runit](http://smarden.org/runit/): for one, a container whose PID 1 is
`tini` running `runsvdir -P /etc/service`. Install `runit` (and `sudo`, for
the router check), then use `cirrusux-setup` as above with
`HOST_MODULES="base cirrusux"` and `CIRRUS_ROLE=gateway`. Setup notices
there's no systemd (or set `CIRRUS_INIT=runit`) and:

- installs the service in `/etc/sv/cirrusux-gateway` (`run` and `log/run`)
  and links it into `/etc/service` once `gateway.env` is filled in; the
  gateway runs as `cirrusux`, reads `gateway.env` itself (`-env`), and logs
  to `/var/log/cirrusux-gateway/current` through svlogd;
- sets the time zone through `/etc/localtime` and `/etc/timezone`;
- never starts, restarts or duplicates `tailscaled`: the machine's own
  Tailscale carries `tailscale serve`;
- refuses the other roles and modules, which need systemd's timers,
  sockets and sleep hooks.

Undo: `sudo rm /etc/service/cirrusux-gateway` stops it (runsvdir notices),
then remove `/etc/sv/cirrusux-gateway`.

Tailscale in userspace mode (`tailscaled --tun=userspace-networking`) works
too: `tailscale serve --bg --https=443 http://127.0.0.1:8064` proxies inside
tailscaled itself, so it needs no tun device. If that machine reaches the
server's router only through a proxy (Tailscale's own
`--socks5-server=localhost:1055`, or a wireproxy tunnel), set
`ROUTER_PROXY` for the router check and wake (below).

## What cirrusux-setup does

`deploy/host/cirrusux-setup` changes only what differs from your settings,
one module at a time, and can be re-run whenever you change `host.env`.
After the first run it's also on the PATH, as `sudo cirrusux-setup`.

Every change goes to `/var/lib/cirrusux/setup.log` with its undo, and replaced
files keep a dated `.bak-` copy; programs keep just the version before the
last update, in `/var/lib/cirrusux/previous/`. Config files are created once (with a shared
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
- **apps:** CirrusStore, the app store: Podman (or Docker), the apps' folders and user,
  and, with Podman, the Docker filter pointed at Podman's API socket.
  Media folders you give apps must exist first, inside `APPS_MEDIA_ROOTS`;
  one an app writes to (a sync or download folder) must belong to the
  apps' user: `sudo install -d -o cirrusux-apps -g cirrusux-apps
  /srv/media/sync`.
- **sleep** (split setup, server only): CirrusUX's own sleep manager, with
  optional fixes after each resume and devices kept from waking it.
- **firewall:** below.
- **ssh** (opt-in): who may log in over SSH, and from where. Like the
  firewall, a change rolls back unless you run `sudo cirrusux-setup
  ssh-confirm`, and setup refuses one that would shut out your own session.
  Password logins are left as they are.
- **ftps** (opt-in): pure-ftpd with TLS required and one virtual user kept
  inside `FTPS_HOME`. Set its password with `sudo cirrusux-setup
  ftps-passwd`, which asks for it; it's never written to a file of ours.

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
- `CIRRUS_WAKE=command` runs a command, with no shell. Run anything that
  needs a secret through a small root wrapper and one sudoers rule, so the
  gateway never sees the secret. For a TR-064 router, use
  `cirrusux-router-wake` (below): the router sends the magic packet, so the
  gateway doesn't need to be on the server's LAN.

## The router check and wake (split setup, optional)

`deploy/gateway/cirrusux-router-check` asks a TR-064 router (a FRITZ!Box,
for one) whether the server is on the network; under the name
`cirrusux-router-wake` it asks the router to wake the server. Install them
like this:

1. `sudo install -m 755 cirrusux-router-check /usr/local/sbin/` and
   `sudo ln -s cirrusux-router-check /usr/local/sbin/cirrusux-router-wake`
2. Put `ROUTER_URL`, `ROUTER_USER`, `ROUTER_PASSWORD` and `SERVER_MAC` in
   `/etc/cirrusux/router.env` yourself (root:root 600). The panel never
   handles the password. If the gateway reaches the router only through a
   proxy (a userspace-mode Tailscale's SOCKS5 server, or a wireproxy
   tunnel), add `ROUTER_PROXY=socks5h://localhost:1055`.
3. Add the sudoers rules, `sudoers-cirrusux-router-check` and
   `sudoers-cirrusux-router-wake` (check each with `sudo visudo -cf`), and
   set `CIRRUS_POWER_CHECK_CMD=sudo -n /usr/local/sbin/cirrusux-router-check`,
   `CIRRUS_WAKE=command` and
   `CIRRUS_WAKE_CMD=sudo -n /usr/local/sbin/cirrusux-router-wake`.
4. Test the wake while the server sleeps:
   `sudo /usr/local/sbin/cirrusux-router-wake` prints `wake sent`.

## The assistant and the daily digest

The assistant is off until `ASSISTANT_MODEL` is set (plus
`ASSISTANT_API_KEY` for hosted providers), and then it's only called when you
ask it something. The 08:00 digest is built from cached data and sends
nothing out, unless `CIRRUS_DIGEST_ASSISTANT=1`.

## Upgrading

Installed with `install.sh`:

```bash
sudo cirrusux-setup upgrade           # the latest release; or: upgrade v0.8.0
sudo cirrusux-setup rollback          # back to the release before
```

`upgrade` downloads and checks the release the same way `install.sh` does,
and points `current` at it. It then shows what applying it would change,
asks, and applies it: new binaries, units and sudoers rules, with the
services restarted. It goes online only when you run it. `rollback` swaps
`current` with `previous` (the one release kept before it) and applies that
one; running it again undoes it. Add `-y` to either to skip the question.
Version-specific steps are in CHANGELOG.md.

Built from source: update the checkout, `make build`, then
`sudo cirrusux-setup apply`. By hand, without setup: install the new
binaries and unit files and run `sudo systemctl daemon-reload`. On the
server, restart `cirrusux-agent` and `cirrusux-dockerfilter.socket`, and
`sudo systemctl stop cirrusux-dockerfilter.service` so the socket starts
the new filter. Wherever the gateway runs, restart `cirrusux-gateway`.

In a split setup the gateway and the server are upgraded separately. The server
tells the gateway which version of CirrusUX it runs and which actions it knows,
so if the server's side is older the panel says so at the top of the page. Any
action the older side can't do stays switched off (with a note on the button)
until you run `sudo cirrusux-setup upgrade` on that machine; reading the status
keeps working.
