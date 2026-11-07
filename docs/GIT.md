# Git hosting

CirrusUX can keep your Git repositories, so your own server is the remote
you push to instead of GitHub. You create, import, rename and delete
repositories on the panel's **Git** page and browse their files, commits,
branches, tags and diffs there. From any computer that can reach the panel,
you clone, push and pull with ordinary `git` over HTTPS.

It is off until you turn it on.

Not there yet: SSH access, issues and pull requests, editing files in the
browser, per-repository permissions, and mirrors that sync by themselves.
Every person on `CIRRUS_ALLOWED_USERS` can do everything with every
repository.

## Turning it on

The gateway needs `git` 2.30 or newer (`apt install git` on Debian). In
`gateway.env`:

```sh
CIRRUS_GIT=1
# Optional:
#CIRRUS_GIT_DIR=/var/lib/cirrusux-gateway/git
#CIRRUS_GIT_URL=https://homeserver.example.ts.net
#CIRRUS_GIT_MAX_PUSH_MB=1024
```

Restart the gateway. If git is missing or too old, the gateway logs why
and leaves Git hosting off; the rest of the panel works as before.

`CIRRUS_GIT_URL` is the address shown in clone URLs. Without it, the panel
uses the address you opened it on.

## Where the repositories are

They are bare repositories in `CIRRUS_GIT_DIR`, by default the `git` folder
in the gateway's data folder, owned by the gateway's own user. Nothing runs
as root.

In the **split** setup the gateway runs on the always-on machine, so the
repositories live there, not on the server that sleeps. You can push and
pull while the server is asleep, and pushing never wakes it.

**Backups:** in the single setup, the backup module's default
`BACKUP_PATHS` already includes the gateway's data folder, so the
repositories and `git-tokens.json` are backed up with no change. If you set
`CIRRUS_GIT_DIR` somewhere else, add that folder to `BACKUP_PATHS`. In the
split setup, back up the gateway machine's data folder from that machine.

## Cloning, pushing and pulling

The clone address is on each repository's page, for example:

```sh
git clone https://homeserver.example.ts.net/git/notes.git
```

To push an existing project:

```sh
cd my-project
git remote add homeserver https://homeserver.example.ts.net/git/notes.git
git push homeserver main
```

### Signing in

- **Through `tailscale serve`** (the default sign-in), git needs no
  password. Tailscale tells the gateway who you are, the same as for the
  panel, and anyone on `CIRRUS_ALLOWED_USERS` may clone and push.
- **Through a reverse proxy with its own login page** (Authelia, Authentik,
  oauth2-proxy), git can't get past the login page. Make an **access token**
  on the Git page and use it as the password when git asks. The user name
  can be anything. Let the reverse proxy pass `/git/` through without its
  login; the gateway checks the token itself. On `/git/` the gateway never
  believes the proxy's identity header (`CIRRUS_AUTH_HEADER`), since a
  client could send it on a route the proxy doesn't log in: only tokens
  count there.

Tokens can **read and write** or **read only**. A token is shown once, when
you make it; only a hash of it is kept. Revoke a token on the Git page when
you no longer need it. A token stops working by itself if its owner is
removed from `CIRRUS_ALLOWED_USERS`.

So you aren't asked every time, let git remember the token:

```sh
git config --global credential.helper store      # in a file in your home folder
# or your system's keychain: osxkeychain on macOS, manager on Windows,
# libsecret on Linux desktops
```

## Importing from GitHub or elsewhere

**Import** on the Git page copies a repository from an `https://` address:
every branch and tag. The gateway fetches it once, when you press the
button, and never again. For a private repository, put a token for that
site in the address (`https://user:token@github.com/you/project.git`). It is
used for the copy and not kept. Afterwards, point your clones at the new
address:

```sh
git remote set-url origin https://homeserver.example.ts.net/git/project.git
```

## Changes and undo

Every change on the Git page is confirmed first and written to the
Changelog, with an **Undo** where one is possible:

| Change | Undo |
|---|---|
| Create or import | Delete it (to Deleted repositories) |
| Rename | Rename it back |
| Delete | Restore it from Deleted repositories |
| Description, default branch | Set the old one again |
| Delete a branch or tag | Put it back at the same commit, for 30 days: a hidden ref keeps its commits that long, then goes on a later delete |
| Delete for good (from Deleted repositories) | None. You type the repository's name to confirm |

Pushes are written to the Changelog too, with who pushed (or which token).

These changes run on the gateway, so they work while the server sleeps.

## Limits

- A push may send up to `CIRRUS_GIT_MAX_PUSH_MB` (1024 MB by default), and
  any one clone, push or import may take up to 30 minutes.
- The panel shows text files up to 1 MB, and up to 300 files or 512 KB of
  changes per commit. Larger ones can be downloaded, or seen in a clone.
- Repository names use lower-case letters, digits, dots, dashes and
  underscores, start with a letter or digit, and are up to 64 characters.
