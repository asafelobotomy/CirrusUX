# Git hosting — design

Date: 2026-11-07. Status: approved in brainstorming, before the plan.

## Purpose

The owner's home server becomes their Git remote instead of GitHub. From the
panel they create, import, list, rename and delete repositories and browse them
(files, commits, branches, tags, diffs). From any machine on the tailnet (or
behind their reverse proxy) they clone, push and pull with plain `git`.

Out of scope for this version: issues, pull requests, SSH, editing files in the
browser, per-repository permissions, mirrors that sync by themselves, syntax
highlighting.

## Decisions

| Question | Decision |
|---|---|
| Built in or catalogue app | Built into CirrusUX |
| Where it runs | Inside the gateway process. In the split setup the repositories live on the always-on gateway machine and work while the server sleeps |
| Transport | Smart HTTP only, through `git http-backend` |
| Sign-in for git | The trusted identity header (as the panel), or HTTP Basic with a personal access token |
| Who may do what | Every allowed panel user may do everything; one flat namespace |
| v1 actions | Create, import from an `https://` URL, rename, delete (to trash), restore, delete for good, description, default branch, delete branch, delete tag, download a ref as `.tar.gz` or `.zip` |

## Architecture

- `internal/githost` (new): the repository work. It runs the system `git`
  binary with `exec.CommandContext` and fixed argument lists, never a shell,
  with a timeout and an output limit on every call. It knows nothing about
  HTTP sign-in or the audit log.
- `internal/gateway`: config, routes, sign-in, actions and audit. Split into
  files by what they do (`git_http.go`, `git_api.go`, `git_actions.go`,
  `git_tokens.go`), each under 400 lines.
- Nothing runs as root. The gateway's own user owns `CIRRUS_GIT_DIR`. No
  helper, helperapi or host-module changes.

### Settings (gateway.env)

| Setting | Default | What it does |
|---|---|---|
| `CIRRUS_GIT` | off | `1` turns Git hosting on |
| `CIRRUS_GIT_DIR` | `$CIRRUS_DATA_DIR/git` | Where bare repositories live |
| `CIRRUS_GIT_URL` | — | The public base for clone URLs shown in the panel, e.g. `https://homeserver.example.ts.net`. Without it the panel uses the address it was opened on |
| `CIRRUS_GIT_MAX_PUSH_MB` | `1024` | The largest request body a push may send |

On start, if `CIRRUS_GIT=1`, the gateway finds `git` on `PATH` and checks
its version is 2.30 or newer. If not, it logs why and leaves Git off. `/api/me`
lists `git` among the features when it's on, and the UI shows the Git page only
then.

### Storage layout

```
$CIRRUS_GIT_DIR/
  <name>.git/            bare repositories
  .trash/<name>-<YYYYMMDD-HHMMSS>.git/   deleted repositories
```

Access tokens are in `$CIRRUS_DATA_DIR/git-tokens.json` (mode 600).

New and imported repositories get `core.logAllRefUpdates=true`,
`gc.pruneExpire=30.days.ago` and `gc.reflogExpireUnreachable=30.days.ago`,
so an undone branch or tag delete finds its commits for at least 30 days.
`http.receivepack=true` is set so `http-backend` accepts pushes from signed-in
users. The description is in the standard `description` file. The default
branch is `HEAD` (`symbolic-ref HEAD refs/heads/<branch>`), `main` for new
repositories.

### Names, refs and paths

- A repository name is `^[a-z0-9][a-z0-9._-]{0,63}$`, doesn't end in `.git`
  and isn't `.trash`. Lower case only, so names never clash on case.
- A ref given by the panel (branch, tag, commit) must not start with `-`,
  must pass `git check-ref-format --allow-onelevel` (branches and tags) or
  be 4–64 hex digits (commits), and is resolved with
  `git rev-parse --verify --end-of-options <ref>^{commit}`.
- A path inside a repository is cleaned (`path.Clean`), must not be
  absolute, contain `..` or start with `-`, and is always passed after `--`.

## Clone, push and pull

Route: `/git/<name>.git/<rest>` on the panel's own address.

- Only the smart paths are served: `GET info/refs?service=git-upload-pack`
  or `git-receive-pack`, `POST git-upload-pack`, `POST git-receive-pack`.
  Everything else is 404, so no "dumb" file serving.
- Run through `net/http/cgi` with `git http-backend`. The environment is
  fixed: `GIT_PROJECT_ROOT=$CIRRUS_GIT_DIR`, `GIT_HTTP_EXPORT_ALL=1`,
  `REMOTE_USER=<user>`, `GIT_CONFIG_NOSYSTEM=1`, `HOME` set to an empty
  folder under the data dir, `PATH` kept. Nothing from the request headers
  reaches git beyond what CGI defines.
- Request bodies are limited (`CIRRUS_GIT_MAX_PUSH_MB`, default 1024) and
  each request has a 30-minute limit.

### Sign-in

This doesn't use `/api`'s CSRF guard (git can't send `X-Cirrus-Request`, and
git's POSTs aren't from a browser). In order:

1. The trusted identity header (`Tailscale-User-Login` or
   `CIRRUS_AUTH_HEADER`) from `CIRRUS_TRUSTED_PROXIES`, matched against
   `CIRRUS_ALLOWED_USERS`: full access. `tailscale serve` sets it on git's
   requests too, so no password is needed there.
2. HTTP Basic. The user name is ignored; the password is a personal access
   token. It must match a token by constant-time comparison of SHA-256
   hashes, and its owner must still be in `CIRRUS_ALLOWED_USERS`. A `read`
   token may fetch only; a push with one is 403 ("This token can only
   read").
3. Otherwise 401 with `WWW-Authenticate: Basic realm="CirrusUX Git"`, so
   git asks for credentials.

In dev mode (`CIRRUS_DEV=1`) a request without either signs in as `dev`,
as the panel does.

Browsers with a panel session reach `/git/` too (same header), which is
harmless: those paths only speak the git protocol.

### Personal access tokens

- Made in the panel: a name, a scope (`read` or `read-write`). The secret is
  `cxg_` plus 32 random bytes in base32, shown once.
- Stored: id, name, owner, scope, SHA-256 of the secret, created, last used
  (updated at most once a minute).
- `GET /api/git/tokens` lists the signed-in user's tokens (never the hash).
  `POST /api/git/tokens` makes one. `DELETE /api/git/tokens/{id}` revokes
  one. Making and revoking are written to the audit log. Revoking has no
  undo.

### Audit of pushes

After a `git-receive-pack` that ends with status 200, the gateway records
"Pushed to <name>" with the user or the token's name. One entry per push, no
undo (the commits are in the repository).

## Panel changes: actions

`actionSpec` gets an optional `local` runner:

```go
local func(s *Server, ctx context.Context, args map[string]string) (undo *ActionReq, out []string, err error)
```

`postAction` checks the spec as it does now. For a local action it skips the
"server awake" check, puts the `Command` in the queue's table as running
without handing it to the agent, and runs the runner in a goroutine with a
time limit. Then it completes the command and records the audit entry the same
way `agentResults` does. The runner returns the undo because some undos need
values found while running (the trash name, the old description, the commit
a branch pointed at). The UI's progress, toasts, Changelog and Undo button
need no changes.

A per-repository lock in `githost` lets only one change run on a repository at
a time. A second one gets "Another change to <name> is still running".

| Action | Args | Does | Undo |
|---|---|---|---|
| `git_create` | name, description | `git init --bare -b main`, settings above | `git_delete` |
| `git_import` | name, url | `git clone --bare` from an `https://` URL, with `GIT_ALLOW_PROTOCOL=https`, `GIT_TERMINAL_PROMPT=0` and a 30-minute limit. Settings above. Phones out only because a person pressed it | `git_delete` |
| `git_rename` | name, to | Renames the folder. Refused if `to` is taken | `git_rename` back |
| `git_delete` | name | Moves to `.trash/<name>-<time>.git` | `git_restore` |
| `git_restore` | trash | Moves back. Refused if the name is taken again | `git_delete` |
| `git_purge` | trash, confirm | Deletes from trash for good. `confirm` must equal the repository's name | none |
| `git_describe` | name, description (≤ 200 chars, one line) | Writes `description` | `git_describe` with the old one |
| `git_default_branch` | name, branch | `symbolic-ref HEAD` to an existing branch | `git_default_branch` with the old one |
| `git_branch_delete` | name, branch | Deletes the branch (not the default branch) | `git_ref_restore` |
| `git_tag_delete` | name, tag | Deletes the tag | `git_ref_restore` |
| `git_ref_restore` | name, ref (full `refs/heads/…` or `refs/tags/…`), sha | Recreates the ref at the commit if it's still there and the ref is free | the matching delete |

`ActionReq.Validate`'s list of accepted arguments gains `to`, `description`,
`url`, `branch`, `tag`, `ref`, `sha` and `trash`.

## Panel changes: read API

All behind the existing `/api` sign-in, all `GET`, all JSON unless noted:

| Path | Returns |
|---|---|
| `/api/git/repos` | Each repo: name, description, default branch, size in bytes, last commit time, empty or not |
| `/api/git/repos/{name}` | The above, plus branches (name, commit, time, subject), tags (name, commit, time) and clone URLs |
| `/api/git/repos/{name}/tree?ref&path` | Entries: name, type (file, folder, link, submodule), size, mode; and the commit the ref is at |
| `/api/git/repos/{name}/blob?ref&path` | Text up to 1 MiB with `binary` and `tooLarge` flags. `&raw=1` sends the file itself as `application/octet-stream` with `Content-Disposition: attachment` |
| `/api/git/repos/{name}/commits?ref&path&page` | 50 per page: sha, parents, author, email, time, subject |
| `/api/git/repos/{name}/commit/{sha}` | The commit with its message and the diff: per file the path, old path, change, added and removed lines, and the patch text. Up to 300 files and 512 KiB of patch; past that, `truncated` |
| `/api/git/repos/{name}/archive?ref&format` | `git archive` as `tar.gz` or `zip`, streamed as a download |
| `/api/git/trash` | Deleted repositories: trash id, name, deleted at, size |

The ref defaults to the default branch. An empty repository answers the tree
with no entries and `empty: true`.

## UI

A **Git** page (`#/git`) in the main nav, after Storage, shown when
`has('git')`. Hash routes inside it: `#/git`, `#/git/<name>`,
`#/git/<name>/commits`, `#/git/<name>/commit/<sha>`, and so on.

- **Repositories:** a list with name, description, last commit and size;
  New repository and Import buttons (dialogs); Access tokens; Deleted
  repositories with Restore and Delete for good (typed name).
- **A repository:** a header with clone URLs (copy buttons) and a branch
  picker; tabs **Files**, **Commits**, **Branches**, **Tags**, **Settings**.
  An empty repository shows how to push an existing project to it.
- **Files:** folder listing with a path trail; a file view with line numbers;
  README.md shown under the listing as plain text in v1; a Download button
  for the ref (`.tar.gz`, `.zip`).
- **Commits:** a paged list; a commit view with the message and a unified
  diff per file (added and removed lines coloured with the theme's tokens).
- **Branches / Tags:** lists with Delete (confirmed). The default branch
  can't be deleted.
- **Settings:** description, default branch, rename, delete.
- Every change uses the existing ConfirmDialog and actions flow.
- Demo mode gets a small set of sample repositories in `lib/demo`.
- Components are split so no file passes 400 lines. No new npm packages.

## Errors

Plain English to the panel ("There's no branch called x in y", "A repository
called x already exists", "git took too long and was stopped"). git's own
stderr goes to the gateway log, not to the panel. The exception is import,
where the panel shows the last line of git's message with the URL's
credentials removed, since import failures are often the remote's fault.

## Testing

- `internal/githost`: with real `git` in temporary folders (the tests skip
  if git is missing). Name, ref and path validation tables; create, rename,
  delete, restore, purge; tree, blob (text, binary, too large), commits,
  commit diff (including truncation), archive; branch and tag delete and
  restore; the lock; the default-branch rule.
- `internal/gateway`: git HTTP sign-in (header, good token, wrong token, a
  read token refused on push, a token whose owner left the allow-list, dumb
  paths refused); end-to-end `git clone` and `git push` through
  `httptest` with a token, then browse the pushed commit through the API;
  local actions with their audit entries and undo actions; import from a
  local `httptest.NewTLSServer` serving http-backend (with `GIT_SSL_NO_VERIFY`
  in the test only); `http://` and `file://` imports refused; tokens API.
- UI: unit tests for the diff-line and path-trail helpers; `svelte-check`.
- `make test` passes. No root-run code, so `make test-host` doesn't change.

## Docs

- `docs/GIT.md`: turning it on, clone URLs, tokens and a credential helper,
  backups (the folder to include), the split-setup note, what isn't there yet.
- `docs/CONFIG.md`: the settings above.
- `CHANGELOG.md`: an `[Unreleased]` entry.
