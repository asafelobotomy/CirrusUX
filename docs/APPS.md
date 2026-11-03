# App definitions

Every built-in app in CirrusStore is one JSON file in `internal/catalogue/apps/`,
named after the app's id (`syncthing.json` for `syncthing`). The files are
built into the binaries, so adding or changing one means a rebuild and a
new release. Nothing is downloaded from a catalogue server.

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

1. Write `internal/catalogue/apps/<id>.json`, with the newest stable tag
   and its digest.
2. `go test ./internal/catalogue` checks every definition, and shows the
   first problem in plain words.
3. Install it on a test machine from CirrusStore, open it, then update or
   remove it, and check its folders are where this page says.
4. Send it as a pull request, saying which tag you tested.

Apps that need several containers (Immich, for one) can't be described yet.

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
