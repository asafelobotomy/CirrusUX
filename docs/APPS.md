# Publishing to CirrusStore

Every app in CirrusStore is one JSON definition. They come from three
places:

- **Built in:** `internal/catalogue/apps/<id>.json`, compiled into the
  binaries. Always there, with no network, and changed only by a release.
- **The official catalogue** (`cirrusstore`), published as static files at
  `https://asafelobotomy.github.io/cirrusstore/` and signed with CirrusUX's
  own key. New apps and newer versions arrive without a CirrusUX release.
- **Third-party sources** that the server's owner chose to trust, each
  with its own key (below, "Hosting your own source").

Nothing is fetched until the owner presses **Check CirrusStore**. The
gateway then fetches each source's index and changed apps, checks every
signature, and caches them. When an app is installed, the root helper on
the server checks the signature again against keys only root can change,
so neither the panel nor the gateway can slip in an app or a source.

The definition format is the same everywhere; this page describes it first,
then how to sign and publish a catalogue.

## What a definition may say

A definition can only say what CirrusUX knows how to run safely. There is
no field for privileged mode, host networking, capabilities or arbitrary
mounts, and a file with a field CirrusUX doesn't know is refused, so none
can be slipped in. The same definition becomes a Podman Quadlet unit or a
Docker compose file, depending on the server's runtime.

## An example

```json
{
  "id": "syncthing",
  "name": "Syncthing",
  "description": "Keep folders in sync between your devices, peer to peer.",
  "website": "https://syncthing.net",
  "icon": "RefreshCw",
  "color": "#0891d1",
  "group": "Files",
  "image": "docker.io/syncthing/syncthing",
  "tag": "2.1.7",
  "digest": "sha256:2b71da7c0d0a91a6ae5285ad079226fecc0a365d08b27720e1f7037b018f7546",
  "ports": [
    { "name": "web", "container": 8384, "host": 8384, "main": true },
    { "name": "sync", "container": 22000, "host": 22000 },
    { "name": "discovery", "container": 21027, "host": 21027, "protocol": "udp" }
  ],
  "volumes": [
    { "name": "data", "path": "/var/syncthing", "kind": "data" },
    { "name": "sync", "path": "/sync", "kind": "media", "writable": true, "label": "Folder to sync" }
  ],
  "env": [
    { "name": "PUID", "value": "{{uid}}" },
    { "name": "PGID", "value": "{{gid}}" },
    { "name": "TZ", "value": "{{tz}}" }
  ],
  "check": { "port": "web", "path": "/rest/noauth/health" },
  "minMemoryMB": 128,
  "notes": "Set a GUI password on the first visit; share folders inside /sync."
}
```

## About the app

| Field | Required | What it is |
|---|---|---|
| `id` | yes | Lowercase letters, digits and `-`, starting with a letter, 2–31 characters. Also the container's name and the app's folder name |
| `name` | yes | Shown in CirrusStore and on the app's tile |
| `description` | yes | One sentence on what it's for |
| `group` | yes | The CirrusStore section and tile group, e.g. `Media`, `Files`, `Home` |
| `website` | no | The project's own site |
| `icon` | no | A [Lucide](https://lucide.dev/icons/) icon name in PascalCase (`RefreshCw`), from those in `web/src/lib/icons.ts`; others fall back to a box |
| `color` | no | The tile's colour, `#rrggbb` in lowercase |
| `notes` | no | Shown on the app's page before installing: the first steps after it starts |
| `minMemoryMB` | no | Shown before installing; not enforced |

## The image

| Field | Required | What it is |
|---|---|---|
| `image` | yes | Registry and name, no tag: `docker.io/syncthing/syncthing`, `ghcr.io/owner/name` |
| `tag` | yes | An exact release, e.g. `2.1.7`. Never `latest` |
| `digest` | yes | `sha256:` and 64 hex digits: the digest of that tag's manifest (for multi-arch images, its image index) |

The image always runs as `image:tag@digest`, so the tag can't move under
you. To find the digest of a tag:

```bash
skopeo inspect --raw docker://docker.io/syncthing/syncthing:2.1.7 | sha256sum
```

Updates (the Updates page) offer newer tags of the same shape as this one:
the same suffix (`-alpine` stays `-alpine`), and never betas, release
candidates or dev builds. So pin a plain release tag.

## Ports

At least one. Each:

| Field | Required | What it is |
|---|---|---|
| `name` | yes | A unique lowercase word, e.g. `web` |
| `container` | yes | The port inside the container |
| `host` | yes | The suggested port on the server, 1024–65535; the owner can change it before installing |
| `protocol` | no | `tcp` (the default) or `udp` |
| `https` | no | `true` if the app serves HTTPS itself on this port, so its tile opens `https://` |
| `main` | no | The port the app's tile opens. At most one; without one, the first port |

Ports are published on every address unless the server sets `APPS_BIND`.
The firewall module keeps them closed to everything but the LAN and the
tailnet.

## Volumes

Each:

| Field | Required | What it is |
|---|---|---|
| `name` | yes | A unique lowercase word |
| `path` | yes | Where it's mounted in the container: a clean, unique absolute path, not `/` |
| `kind` | yes | `config`, `data`, `cache` or `media` (below) |
| `writable` | no | Media only: mount it read-write. Without it, media is read-only |
| `label` | no | Media only: what the owner is asked for, e.g. "Music folder" |

| Kind | Lives in | Backed up before an update | Kept when the app is removed |
|---|---|---|---|
| `config` | `APPS_DIR/<id>/<name>` | yes | yes, in the archive |
| `data` | `APPS_DIR/<id>/<name>` | yes | yes, in the archive |
| `cache` | `APPS_DIR/<id>/<name>` | no | yes, in the archive |
| `media` | a folder the owner picks, inside `APPS_MEDIA_ROOTS` | no | untouched |

Media is read-only unless the app has to write there (a sync folder, a
download folder). Ask for `writable` only then. The owner picks the folder,
which must already exist; a writable one must belong to the apps' user
(`cirrusux-apps`), and the app's `notes` should say so.

## Devices

Optional. Only these paths (or ones under them) are allowed: `/dev/dri`
(graphics, for video transcoding), `/dev/ttyUSB`, `/dev/ttyACM` (USB
radios and serial devices) and `/dev/snd` (sound).

| Field | Required | What it is |
|---|---|---|
| `path` | yes | The device, e.g. `/dev/dri` |
| `why` | yes | Shown to the owner: what the app does with it |
| `optional` | no | The owner can switch it off before installing; it's off unless they switch it on |
| `hostGroups` | no | Host groups the container joins to use it, e.g. `["render", "video"]` |

## Environment

Each variable has either a fixed `value` or a list of `choices`, not both.

| Field | What it is |
|---|---|
| `name` | A unique `UPPER_CASE` name |
| `value` | A fixed value, one line. May use `{{tz}}` (the server's time zone), `{{uid}}` and `{{gid}}` (the apps' own user) |
| `choices` | The values the owner can pick from |
| `default` | With `choices`: which one is picked first; must be one of them |
| `label` | With `choices`: what the owner is asked |

There is no free-text variable, and no field for secrets: an app that
needs an admin password should ask for it on its first visit (say so in
`notes`).

## Running it

| Field | What it is |
|---|---|
| `runAsUser` | `true` runs the container as the apps' own user instead of the image's default. Use it for images that run as any user; images that take `PUID`/`PGID` use those instead |
| `check` | How an install or update knows the app is up: `{"port": "<port name>", "path": "/health"}`, an HTTP GET that must answer. Without it, the container running is enough. An update whose check fails is rolled back by itself, so give one wherever the app has a health or status URL |

Every container restarts unless stopped, and carries the labels
`io.cirrusux.app` and `io.cirrusux.version`.

## Adding an app

1. Write `<id>.json`, with the newest stable tag and its digest.
2. Check it: `cirrusstore sign` refuses a definition with any problem, in
   plain words. For a built-in app, `go test ./internal/catalogue` checks
   every file in `internal/catalogue/apps/`.
3. Install it on a test machine, open it, then update or remove it, and
   check its folders are where this page says.
4. For the official catalogue, send it as a pull request to its repository,
   saying which tag you tested.

Apps that need several containers (Immich, for one) can't be described yet.

## The signed catalogue

A catalogue is a folder of static files, served over HTTPS as it is:

```
index.json        index.json.sig
apps/<id>.json    apps/<id>.json.sig
```

Each `apps/<id>.json` is an **envelope** around one definition:

```json
{
  "source": "cirrusstore",
  "serial": 3,
  "expires": "2027-05-01T00:00:00Z",
  "app": { "id": "syncthing", "name": "Syncthing", "...": "..." }
}
```

`index.json` lists the apps with their serials, names, groups and icons,
under its own `source`, `serial` and `expires`. Each `.sig` file is an
Ed25519 signature of the exact bytes of its file, in base64. A server
accepts a file only when:

- the signature matches a key it trusts for that source;
- the file's `source` is the source it was fetched from, so one
  publisher's signed file can't be passed off as another's;
- it hasn't expired; and
- its serial is no older than the one it has already seen (the index's,
  and an installed app's), so an old file can't be replayed.

An envelope can be at most 8 KB, since it travels to the server with the
install request. `app` is checked exactly like a built-in definition: a
field CirrusUX doesn't know is refused.

### Serials

An app's serial goes up by one each time its definition changes: a new tag,
a port, a variable. An installed app whose source has a higher serial shows
on the Updates page as a **new definition**; updating moves it to the new
definition (backed up first, and rolled back if its check fails), and the
server records which source, serial and definition it runs. Updates,
rollback and removal use that recorded definition, so an app keeps working
when its source drops it or goes away. Tag-only updates from the registry
still appear as before.

The index has its own serial, one up each time it's rebuilt.

### Expiry

Signatures expire (180 days by default), so a catalogue that stops being
maintained stops offering apps instead of offering stale pins forever. An
expired source shows as **expired** in the panel's Sources card, and its
apps can't be installed until the publisher re-signs. Installed apps keep
running. Re-sign well before the date: `cirrusstore sign` on unchanged
definitions keeps their serials and only moves the expiry.

## The publisher tool

`cirrusstore` uses only Go's standard library (`go build ./cmd/cirrusstore`,
or `bin/cirrusstore` in a release):

| Command | What it does |
|---|---|
| `cirrusstore keygen -name NAME` | A new key pair: `NAME.key` (mode 600; keep it offline) and `NAME.pub` |
| `cirrusstore sign -key NAME.key DEF.json…` | Wraps each definition in a signed envelope in `apps/`, keeping the serial if it's unchanged and adding one if not |
| `cirrusstore index -key NAME.key` | Lists `apps/` in a new signed `index.json`, one serial up |
| `cirrusstore verify -pub NAME.pub DIR` | Checks a folder the way a server will, including that the index and the envelopes agree |

`-source` names the source (default: the key's name), `-days` sets the
expiry (default 180), and `-dir` the catalogue folder (default `.`).

A release of a catalogue, from a folder of definitions in `defs/`:

```bash
cirrusstore sign -key ~/offline/friends.key -dir site defs/*.json
cirrusstore index -key ~/offline/friends.key -dir site
cirrusstore verify -pub friends.pub site
```

Then publish `site/` as it is. Only `NAME.pub` is ever shared.

## Hosting your own source

Any static HTTPS host works (GitHub Pages, a bucket, a plain web server).
The source's name is the one in your key and every file (lowercase letters,
digits and `-`; `cirrusstore` and `built-in` are taken).

To trust it, the server's owner adds it to `host.env` and runs the store
module:

```bash
# host.env
STORE_SOURCES="friends|https://apps.example.org/catalogue/|<last field of friends.pub>"
sudo cirrusux-setup apply store
```

That writes `/etc/cirrusux/store.d/friends.source` (root, mode 644). The
panel lists the source with its key's fingerprint (`SHA256:…`): compare it
with the one `cirrusstore keygen` printed for you, over a channel other
than the one the key came by. The panel can't add a source; only root on
the server can.

App ids are unique across sources. If yours has an id that a built-in or
official app already uses, CirrusStore shows yours as unavailable, with the
reason, and the other one wins. Choose distinct ids (`friends-navidrome`
rather than `navidrome`).

## The official key

The official catalogue's public key is built into CirrusUX
(`internal/store/official.pub`); its signing key is kept offline by the
maintainers, and only the public half is ever in the code. With no key in that file, the
official source is listed but never fetched. `STORE_OFFICIAL=no` leaves
it out altogether.

To move the built-in apps into the official catalogue:

```bash
make store-site KEY=/path/to/offline/cirrusstore.key OUT=site
```

which runs `cirrusstore sign` on `internal/catalogue/apps/*.json` and then
`cirrusstore index`. Publish `site/` to the catalogue's repository.

The built-in definitions stay in the binaries as the offline fallback. An
official definition with the same id replaces the built-in one once the
catalogue has been checked.

## Apps that write to the media

tinyMediaManager and Picard rename and tag files in place, so their media
is `writable`. Both images start as root and then drop to `USER_ID` and
`GROUP_ID`, set to the apps' user (`{{uid}}`, `{{gid}}`). With
`APPS_MEDIA_UID` and `APPS_MEDIA_GID` set to the media's owner, that user
is the owner, so nothing needs a `chown`. `UMASK=0002` keeps new files
group-writable, which suits media folders shared through a group with
setgid directories (mode `2775`). Neither has a password of its own (each is
a desktop in the browser), and each has an update check to switch off by
hand; their `notes` say where.

## Checked plugins

| App, tag | Plugin | Result | Checked |
|---|---|---|---|
| Jellyfin `12.4.20261026-211555` (server 12.4.0) | Trakt 33.0.0.0 (target ABI 12.1.0.0), from Jellyfin's stable repository | Loads: the log shows `Loaded plugin: Trakt 33.0.0.0`, its status is `Active`, no warnings or errors, and the server reports `Healthy` | 3 Nov 2026, rootless Podman, a fresh config with the plugin's zip unpacked in `plugins/Trakt_33.0.0.0` |

CirrusUX never installs or changes Jellyfin plugins: install Trakt from
Jellyfin's own Dashboard → Plugins → Catalog. After an update to a new
Jellyfin tag, its log says whether each plugin loaded.
