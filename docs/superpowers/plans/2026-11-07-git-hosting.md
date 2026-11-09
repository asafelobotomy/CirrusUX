# Git hosting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Status:** Done. All tasks are implemented (2026-11-07); see docs/GIT.md for the result.

**Goal:** Self-hosted Git repositories in the CirrusUX gateway: create, import, rename, delete, browse in the panel, and clone, push and pull over smart HTTP.

**Architecture:** A new `internal/githost` package does all repository work by running the system `git` with fixed argument lists. The gateway adds config, a `/git/` smart-HTTP route (`git http-backend` through `net/http/cgi`) with header-or-token sign-in, git actions that run on the gateway through the existing actions flow (audit, Undo), and a read API under `/api/git/`. A Svelte **Git** page browses and changes repositories.

**Tech Stack:** Go standard library only (`os/exec`, `net/http/cgi`, `crypto/sha256`); system `git` ≥ 2.30; Svelte 5 + TypeScript, no new npm packages; `node --test` for UI helpers.

**Spec:** `docs/superpowers/specs/2026-11-07-git-hosting-design.md` (read it with this plan; values below come from it).

## Global Constraints

- Go: standard library only, no new modules. `gofmt` clean, `go vet` clean.
- No source file over 400 lines, comments and blanks included (`scripts/check-file-length.sh`).
- git is never run through a shell; every call has a timeout and an output limit; user-given refs and paths come after `--end-of-options` or `--`.
- Repo name: `^[a-z0-9][a-z0-9._-]{0,63}$`, not ending in `.git`, not `.trash`.
- New and imported repos: `core.logAllRefUpdates=true`, `gc.pruneExpire=30.days.ago`, `gc.reflogExpireUnreachable=30.days.ago`, `http.receivepack=true`; new repos' default branch `main`.
- Settings: `CIRRUS_GIT` (off; `1` on), `CIRRUS_GIT_DIR` (`$CIRRUS_DATA_DIR/git`), `CIRRUS_GIT_URL` (none), `CIRRUS_GIT_MAX_PUSH_MB` (`1024`).
- Tokens: secret `cxg_` + base32 (no padding) of 32 random bytes; stored as SHA-256 hex in `$CIRRUS_DATA_DIR/git-tokens.json` mode 600; scopes `read`, `read-write`.
- Limits: blob text 1 MiB; commits page 50; diff 300 files / 512 KiB patch; push body `CIRRUS_GIT_MAX_PUSH_MB`; git HTTP and import 30 min.
- Plain English in every message the panel shows; git's stderr goes only to the log (except import's last line, with URL credentials removed).
- Nothing phones out by itself: only `git_import`, when pressed, fetches from outside, and only `https://`.
- Every change is confirmed in the UI, logged, and undoable where the spec says.
- No personal details in tests or docs: `homeserver`, `192.0.2.x`, `owner@example.com`.
- Tests needing git call `t.Skip` when `exec.LookPath("git")` fails.
- Subagents: run builds and tests in the foreground and leave nothing running in the background when reporting; don't wait on CI, just report what was pushed.

## Review Focus

- **Repo names that look like paths or options** (`../x`, `-x`, `x.git`, `X`, `.trash`, 65 chars): refused with a plain message before any git or filesystem call. Test: `TestValidName` table (Task 1) and `TestGitHTTPBadPaths` (Task 4).
- **A ref or path from the panel that starts with `-` or contains `..`** (`--output=/tmp/x`, `../../etc/passwd`): refused, never reaches git as an option. Test: `TestBrowseRefusesOptionLikeInput` (Task 3).
- **An empty repository** (just created, nothing pushed): list, info, tree, commits and archive all answer without error (`empty: true`, no entries, empty list; archive is a 409 "nothing to download yet"). Test: `TestEmptyRepoBrowse` (Task 3).
- **A token whose owner was removed from `CIRRUS_ALLOWED_USERS`**, and a read token used to push: refused (401 and 403). Test: `TestGitHTTPTokenRules` (Task 4).
- **Two changes to one repo at once** (rename while a delete runs): the second gets "Another change to <name> is still running", no half-done state. Test: `TestLockRefusesSecondChange` (Task 2).

---

## File map

| File | What it does |
|---|---|
| `internal/githost/errors.go` | `Error` with `Kind`, constructors |
| `internal/githost/validate.go` | name, branch/tag, commit id, path checks |
| `internal/githost/run.go` | `Host`, `New`, running git, version check, per-repo lock |
| `internal/githost/repos.go` | create, import, rename, list, info, description, default branch |
| `internal/githost/trash.go` | delete to trash, restore, purge, list trash |
| `internal/githost/refs.go` | delete and restore branches and tags |
| `internal/githost/browse.go` | tree, blob, raw, commits, archive |
| `internal/githost/diff.go` | commit detail and diff parsing |
| `internal/githost/*_test.go` | tests per file above; `helpers_test.go` builds repos with commits |
| `internal/gateway/config.go` | the four settings |
| `internal/gateway/git_tokens.go` | token store |
| `internal/gateway/git_http.go` | `/git/` route, sign-in, http-backend, push audit |
| `internal/gateway/git_actions.go` | git action specs and local runners |
| `internal/gateway/git_api.go` | `/api/git/...` read API and tokens API |
| `internal/gateway/actions.go`, `queue.go`, `server.go` | local actions, wiring, `/api/me` |
| `web/src/lib/git/api.ts` | types and fetchers, demo fallback |
| `web/src/lib/git/diff.ts` | `parsePatch`, `pathTrail`, `parseGitRoute` |
| `web/src/lib/demo/git.ts` | sample repos for demo mode |
| `web/src/pages/Git.svelte` | page shell: picks list or repo view from the route |
| `web/src/lib/git/*.svelte` | `RepoList`, `NewRepoDialog`, `Tokens`, `Trash`, `RepoView`, `Files`, `FileView`, `Commits`, `CommitView`, `Refs`, `RepoSettings`, `CloneBox` |
| `docs/GIT.md`, `docs/CONFIG.md`, `CHANGELOG.md` | docs |

---

### Task 1: githost foundations — errors, validation, running git

**Files:**
- Create: `internal/githost/errors.go`, `internal/githost/validate.go`, `internal/githost/run.go`
- Test: `internal/githost/validate_test.go`, `internal/githost/run_test.go`

**Interfaces:**
- Produces:
  - `type Kind int` with `Invalid, NotFound, Exists, Busy, Timeout, Failed`
  - `type Error struct{ Kind Kind; Msg string }`; `func (e *Error) Error() string` returns `Msg`; `func errf(k Kind, format string, a ...any) *Error`
  - `func ValidName(name string) error`
  - `func validBranch(name string) error` (shape only: not empty, no leading `-`, ≤ 255, no `..`, no control chars, no space, no `~^:?*[\`, not ending `/` or `.lock`; `check-ref-format` is run by callers that write refs)
  - `func validCommit(sha string) error` (4–64 hex)
  - `func cleanPath(p string) (string, error)` → `""` for root; refuses absolute, `..`, leading `-`
  - `type Host struct{ Dir string; git string; home string; mu sync.Mutex; busy map[string]bool }`
  - `func New(dir, home string) (*Host, error)` — `exec.LookPath("git")`, version ≥ 2.30 (parse `git version X.Y`), `MkdirAll(dir/.trash, 0o700)`, `MkdirAll(home, 0o700)`
  - `func (h *Host) run(ctx context.Context, dir string, limit int64, args ...string) ([]byte, error)` — env exactly: `PATH` from os, `HOME=h.home`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`; stdout capped at `limit` (over → `errf(Failed, "git's answer was too large")`); context deadline → `errf(Timeout, "git took too long and was stopped")`; non-zero exit → logs stderr with `slog.Warn("git", ...)`, returns `errf(Failed, "git failed")`
  - `func (h *Host) runIn(ctx, dir string, stdout io.Writer, args ...string) error` — streaming variant for raw files and archives
  - `func (h *Host) lock(name string) (unlock func(), err error)` — `errf(Busy, "Another change to %s is still running", name)` when held
  - `func (h *Host) path(name string) string` → `filepath.Join(h.Dir, name+".git")`

- [x] **Step 1: Write the failing tests**

`TestValidName`: accepts `notes`, `my-app`, `a.b_c`, `x` and a 64-char name; refuses `""`, `-x`, `.x`, `X`, `a/b`, `../x`, `x.git`, `.trash`, a 65-char name, `a b`; each refusal is `*Error` with `Kind == Invalid`.
`TestCleanPath`: `""`→`""`, `"/"`→ error, `"src/main.go"`→same, `"src//a/./b"`→`"src/a/b"`, `"../x"`, `"a/../../x"`, `"-x"` → errors.
`TestValidBranch` / `TestValidCommit`: `main`, `feature/x` ok; `-x`, `a..b`, `x.lock`, `a b` refused; `abcd`, 40-hex ok; `abc`, `xyz1`, 65-hex refused.
`TestRunTimeoutAndLimit` (needs git): `run` with a 1 ns context returns `Kind == Timeout`; `run(ctx, "", 10, "version")` (git prints more than 10 bytes) is `Kind == Failed` with message `git's answer was too large`.
`TestLockRefusesSecondChange`: `unlock, _ := h.lock("a")`; second `h.lock("a")` is `Busy` with message `Another change to a is still running`; `h.lock("b")` ok; after `unlock()`, `h.lock("a")` ok.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/githost/`
Expected: FAIL (package doesn't build: undefined `ValidName`, ...)

- [x] **Step 3: Implement the files listed in Interfaces**

- [x] **Step 4: Run them and see them pass**

Run: `go test ./internal/githost/ && gofmt -l internal/githost && go vet ./internal/githost/`
Expected: `ok`, no gofmt output, no vet output.

- [x] **Step 5: Commit**

```bash
git add internal/githost
git commit -m "githost: names, paths and running git"
```

### Task 2: githost repositories and trash

**Files:**
- Create: `internal/githost/repos.go`, `internal/githost/trash.go`, `internal/githost/helpers_test.go`
- Test: `internal/githost/repos_test.go`, `internal/githost/trash_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `type Summary struct{ Name, Description, DefaultBranch string; Size int64; LastCommit *time.Time; Empty bool }` (JSON: `name, description, defaultBranch, size, lastCommit, empty`)
  - `type Branch struct{ Name, Commit, Subject string; Time time.Time }`; `type Tag struct{ Name, Commit string; Time time.Time }` (JSON lower camel)
  - `type Repo struct{ Summary; Branches []Branch; Tags []Tag }` (JSON embeds Summary fields, plus `branches`, `tags`)
  - `func (h *Host) Exists(name string) bool`
  - `func (h *Host) Create(ctx, name, description string) error` — `init --bare -b main`, set the config above, write `description` (empty → git's default text is replaced by `""`)
  - `func (h *Host) Import(ctx, name, rawURL string) error` — `rawURL` must parse with scheme `https` and a host, else `Invalid` "Only https:// addresses can be imported"; clones into `.tmp-<name>-<rand>` inside `h.Dir` with extra env `GIT_ALLOW_PROTOCOL=https`, then renames into place; on failure removes the temp folder and returns `Failed` with git's last stderr line with `user:pass@` removed (`redactURL`)
  - `func (h *Host) Rename(ctx, from, to string) error` — `Exists` errors: `NotFound` "There's no repository called %s", `Exists` "A repository called %s already exists"
  - `func (h *Host) List(ctx) ([]Summary, error)` — sorted by name; skips `.trash`, `.tmp-*`, anything not `*.git`
  - `func (h *Host) Info(ctx, name) (Repo, error)` — branches/tags from `for-each-ref --format` with `%(refname:short)%00%(objectname)%00%(committerdate:unix)%00%(subject)`, sorted newest first
  - `func (h *Host) SetDescription(ctx, name, desc string) (old string, err error)` — ≤ 200 chars, no newline, else `Invalid`
  - `func (h *Host) SetDefaultBranch(ctx, name, branch string) (old string, err error)` — branch must exist (`NotFound` "There's no branch called %s in %s")
  - `type Trashed struct{ ID, Name string; DeletedAt time.Time; Size int64 }`
  - `func (h *Host) Delete(ctx, name string) (trashID string, err error)` — ID `<name>-<YYYYMMDD-HHMMSS>` from `h.now()` (add `now func() time.Time` to Host, default `time.Now`), with `-2`, `-3` suffix if taken
  - `func (h *Host) Restore(ctx, trashID string) (name string, err error)`
  - `func (h *Host) Purge(ctx, trashID, confirm string) error` — `confirm` must equal the trashed repo's name, else `Invalid` "Type the repository's name to delete it for good"
  - `func (h *Host) ListTrash() ([]Trashed, error)` — newest first
  - Every mutating method takes `h.lock(name)` (rename: both names; trash ops: the trash ID).
  - Test helper `newHost(t) *Host` and `commitFiles(t, h, repo string, files map[string]string, msg string) string` (clones to a temp dir, writes files, commits with a fixed author `Test <test@example.com>`, pushes with `git push` on the local path, returns the commit sha).

- [x] **Step 1: Write the failing tests**

`TestCreateListRename`: create `notes` with description `My notes`; `List` has one `Summary{Name:"notes", Description:"My notes", DefaultBranch:"main", Empty:true}`; `git config` in the repo shows `gc.pruneExpire=30.days.ago` and `http.receivepack=true`; creating `notes` again is `Exists`; `Rename(notes, diary)` then `List` names `diary`; renaming a missing repo is `NotFound`.
`TestInfoAfterCommit`: `commitFiles` two commits on `main`, push a `dev` branch and a tag `v1`; `Info` has branches `main`, `dev` with subjects, tag `v1`; `Empty == false`; `LastCommit` set.
`TestDescriptionAndDefaultBranch`: `SetDescription` returns the old one; 201 chars or `"a\nb"` is `Invalid`; `SetDefaultBranch(dev)` returns `main`; `SetDefaultBranch(nope)` is `NotFound`.
`TestTrashRoundTrip`: with a fixed `now`, `Delete(notes)` returns `notes-20261107-120000`; `List` is empty; `ListTrash` has it; deleting a new `notes` at the same time gives `notes-20261107-120000-2`; `Restore` brings back `notes`; restore while `notes` exists is `Exists`; `Purge(id, "wrong")` is `Invalid`; `Purge(id, "notes")` removes the folder.
`TestImportRefusesNonHTTPS`: `file:///x`, `http://192.0.2.1/x.git`, `ssh://192.0.2.1/x`, `/tmp/x`, `ext::sh -c x` are `Invalid`; nothing is left in `h.Dir` but `.trash`.
`TestRedactURL`: `"fatal: unable to access 'https://bob:secret@192.0.2.1/x.git/'"` → no `secret`, no `bob:`.
`TestLockRefusesSecondChange` extended: while `h.lock("notes")` is held, `Rename(ctx,"notes","x")` is `Busy`.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/githost/`
Expected: FAIL (undefined `Create`, `List`, ...)

- [x] **Step 3: Implement `repos.go` and `trash.go` per Interfaces**

Size is the sum of file sizes under the repo folder (`filepath.WalkDir`). `LastCommit` is the newest `committerdate` among `refs/heads`. A successful import is tested in Task 5 (needs a TLS http-backend).

- [x] **Step 4: Run them and see them pass**

Run: `go test ./internal/githost/`
Expected: `ok`

- [x] **Step 5: Commit**

```bash
git add internal/githost
git commit -m "githost: create, import, rename, list and trash"
```

### Task 3: githost browsing, diffs and refs

**Files:**
- Create: `internal/githost/browse.go`, `internal/githost/diff.go`, `internal/githost/refs.go`
- Test: `internal/githost/browse_test.go`, `internal/githost/diff_test.go`, `internal/githost/refs_test.go`

**Interfaces:**
- Consumes: Tasks 1–2 (`commitFiles` helper).
- Produces:
  - `func (h *Host) resolve(ctx, name, ref string) (sha string, empty bool, err error)` — `ref == ""` → default branch; empty repo → `("", true, nil)`; ref starting `-` → `Invalid`; `rev-parse --verify --end-of-options <ref>^{commit}` failing → `NotFound` "There's no branch, tag or commit called %s in %s"
  - `type Entry struct{ Name, Type string; Size int64; Mode string }` (Type: `file`, `folder`, `link`, `submodule`)
  - `type Tree struct{ Commit string; Empty bool; Entries []Entry }` — folders first, then by name
  - `func (h *Host) Tree(ctx, name, ref, path string) (Tree, error)` — `ls-tree -l --end-of-options <sha> -- <path>/` (root: no path); a path that's a file → `Invalid` "%s is a file"
  - `type Blob struct{ Path string; Size int64; Binary, TooLarge bool; Text string }`
  - `func (h *Host) Blob(ctx, name, ref, path string) (Blob, error)` — size from `cat-file -s`; > 1 MiB → `TooLarge`, no text; binary = a NUL byte in the first 8000 bytes or not valid UTF-8
  - `func (h *Host) Raw(ctx, name, ref, path string, w io.Writer) error`
  - `type Commit struct{ SHA string; Parents []string; Author, Email string; Time time.Time; Subject string }` (JSON `sha, parents, author, email, time, subject`)
  - `func (h *Host) Commits(ctx, name, ref, path string, page int) ([]Commit, error)` — page ≥ 1, 50 per page, `log --format=%H%x00%P%x00%an%x00%ae%x00%ct%x00%s --skip --max-count`; empty repo → `[]`
  - `type FileDiff struct{ Path, OldPath, Change string; Added, Removed int; Binary bool; Patch string }` (Change: `added`, `deleted`, `modified`, `renamed`)
  - `type Detail struct{ Commit; Message string; Files []FileDiff; Truncated bool }`
  - `func (h *Host) CommitDetail(ctx, name, sha string) (Detail, error)` — `validCommit`; `show --format=... --patch --find-renames --no-color --no-ext-diff`; root commits diff against the empty tree
  - `func parsePatch(out []byte, maxFiles, maxBytes int) (files []FileDiff, truncated bool)` — splits on `diff --git `; reads `new file mode`, `deleted file mode`, `rename from/to`, `Binary files`; counts `+`/`-` lines after the first `@@`
  - `func (h *Host) Archive(ctx, name, ref, format string, w io.Writer) error` — format `tar.gz` or `zip` else `Invalid`; empty → `errf(Exists, "There's nothing to download yet")` (gateway maps Exists → 409)
  - `func (h *Host) DeleteRef(ctx, name, ref string) (sha string, err error)` — `ref` is `refs/heads/<b>` or `refs/tags/<t>` (else `Invalid`); the default branch → `Invalid` "%s is the default branch. Pick another default first"; missing → `NotFound`; `update-ref -d <ref> <sha>`
  - `func (h *Host) RestoreRef(ctx, name, ref, sha string) error` — ref free (else `Exists` "%s already exists"), commit present (`cat-file -e <sha>^{commit}`, else `NotFound` "The commit %s is gone, so %s can't be restored"); `update-ref <ref> <sha> ""`

- [x] **Step 1: Write the failing tests**

`TestTreeAndBlob`: repo with `README.md` ("hello"), `src/main.go`, `logo.bin` (bytes `0x00 0x01`), and a 1 MiB + 1 file `big.txt`. Root tree: `src` (folder) first, then `README.md`, `big.txt`, `logo.bin` as files; `Tree(…,"src")` lists `main.go`; `Blob(README.md).Text == "hello"`; `logo.bin` `Binary`; `big.txt` `TooLarge` with empty `Text`; `Tree` of `README.md` is `Invalid`; `Raw(logo.bin)` writes the 2 bytes.
`TestCommitsPaging`: 55 commits; page 1 has 50 newest first, page 2 has 5; `Commits(…, path="src/main.go", 1)` only commits touching it.
`TestCommitDetail`: a commit that modifies `a.txt` (+2 −1), adds `b.txt`, deletes `c.txt`, renames `d.txt`→`e.txt`: four `FileDiff`s with those `Change`s, counts and `OldPath:"d.txt"`; the first commit's detail shows its files as `added`.
`TestParsePatchTruncates`: 301 small file diffs → 300 files, `truncated`; one 600 KiB diff with `maxBytes` 512 KiB → `truncated`.
`TestArchive`: `tar.gz` output starts with `0x1f 0x8b`; `zip` with `PK`; `rar` is `Invalid`.
`TestBrowseRefusesOptionLikeInput`: `Tree(ref="--output=/tmp/x")`, `Blob(path="../x")`, `Blob(path="-x")`, `CommitDetail(sha="-p")`, `Commits(ref="--all")` all `Invalid`; no file `/tmp/x` created.
`TestEmptyRepoBrowse`: fresh repo: `Tree` → `Empty:true`, no entries; `Commits` → empty slice, no error; `Info` → no branches; `Archive` → `Kind == Exists`.
`TestDeleteRestoreRef`: delete `refs/heads/dev` returns its sha and `Info` loses it; `RestoreRef` brings it back; restoring again is `Exists`; deleting `refs/heads/main` (default) is `Invalid`; deleting `refs/heads/x` missing is `NotFound`; tag delete and restore the same; `refs/remotes/x` is `Invalid`.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/githost/`
Expected: FAIL (undefined `Tree`, ...)

- [x] **Step 3: Implement `browse.go`, `diff.go`, `refs.go` per Interfaces**

- [x] **Step 4: Run them and see them pass**

Run: `go test -race ./internal/githost/ && bash scripts/check-file-length.sh`
Expected: `ok`; the length check prints nothing over 400.

- [x] **Step 5: Commit**

```bash
git add internal/githost
git commit -m "githost: browse files, commits, diffs; delete and restore refs"
```

### Task 4: Gateway config, tokens and the `/git/` route

**Files:**
- Modify: `internal/gateway/config.go` (fields, env parsing), `internal/gateway/server.go` (`Server.git`, `Server.gitTokens`, `NewServer`, `Handler`)
- Create: `internal/gateway/git_tokens.go`, `internal/gateway/git_http.go`
- Test: `internal/gateway/git_tokens_test.go`, `internal/gateway/git_http_test.go`, `internal/gateway/config_test.go` (extend)

**Interfaces:**
- Consumes: `githost.New`, `(*Host).Exists`, `Create` (tests), `ValidName`.
- Produces:
  - `Config` fields `Git bool; GitDir, GitURL string; GitMaxPush int64` (bytes). `ConfigFromEnv`: `CIRRUS_GIT=1`; `CIRRUS_GIT_DIR` default `filepath.Join(DataDir, "git")`; `CIRRUS_GIT_URL` must be `https://` or `http://` with a host and no path beyond `/` if set, trailing `/` dropped; `CIRRUS_GIT_MAX_PUSH_MB` positive int, default 1024.
  - `Server` fields `git *githost.Host` (nil = off) and `gitTokens *gitTokenStore`. `NewServer`: if `cfg.Git`, `githost.New(cfg.GitDir, filepath.Join(cfg.DataDir, "git-home"))`; on error `slog.Error("git hosting is off", "err", err)` and leave nil. Tokens store always opened at `filepath.Join(cfg.DataDir, "git-tokens.json")` when git is on.
  - `type GitToken struct{ ID, Name, Owner, Scope string; Hash string; Created time.Time; LastUsed *time.Time }` (JSON `id,name,owner,scope,hash,created,lastUsed`)
  - `type gitTokenStore struct{ path string; mu sync.Mutex; list []GitToken; now func() time.Time }`
  - `func openGitTokens(path string) *gitTokenStore`
  - `func (t *gitTokenStore) Create(owner, name, scope string) (secret string, tok GitToken, err error)` — name 1–60 chars, one line; scope `read` or `read-write`; ID 8 random bytes hex; saved with `writeFileJSON`
  - `func (t *gitTokenStore) List(owner string) []GitToken` (Hash cleared)
  - `func (t *gitTokenStore) Revoke(owner, id string) (GitToken, error)` — other owners' tokens are "not found"
  - `func (t *gitTokenStore) Check(secret string) (GitToken, bool)` — `subtle.ConstantTimeCompare` on hex hashes over every token; updates `LastUsed` and saves only if the last save of it was ≥ 1 minute ago
  - `func gitService(rest string, query url.Values, method string) (repo string, push, ok bool)` — `rest` is the path after `/git/`: `<name>.git/info/refs` (GET, `service` = `git-upload-pack` or `git-receive-pack`; push when receive-pack), `<name>.git/git-upload-pack` (POST), `<name>.git/git-receive-pack` (POST, push). Anything else → `ok=false`.
  - `type gitCaller struct{ User, Label string; CanPush bool }` (Label: the user, or `token "<name>" of <owner>`)
  - `func (s *Server) gitCaller(r *http.Request) (gitCaller, bool)` — header from trusted proxy in allow-list → full; else Basic password via `Check` with owner in allow-list → scope decides `CanPush`; dev mode with neither → `dev`, full
  - `func (s *Server) gitHTTP(w http.ResponseWriter, r *http.Request)` — mounted `mux.Handle("/git/", http.HandlerFunc(s.gitHTTP))` only when `s.git != nil`, **outside** `s.auth`; order: `gitService` (404 "not found"), `ValidName` and `Exists` (404), caller (401 + `WWW-Authenticate: Basic realm="CirrusUX Git"`), push without `CanPush` (403 "This token can only read"); `http.MaxBytesReader(GitMaxPush)`; `context.WithTimeout` 30 min; `cgi.Handler{Path: git, Args: []string{"http-backend"}, Dir: s.cfg.GitDir, Env: [...], InheritEnv: []string{"PATH"}}` with env `GIT_PROJECT_ROOT=<GitDir>`, `GIT_HTTP_EXPORT_ALL=1`, `REMOTE_USER=<caller.User>`, `GIT_CONFIG_NOSYSTEM=1`, `HOME=<DataDir>/git-home`; for a push POST, wrap the writer to catch the status and on 200 `s.record(AuditEntry{Who: caller.Label, What: "Pushed to " + repo})`
  - `githost` needs `func (h *Host) GitPath() string` for the cgi `Path` — add it in this task.

- [x] **Step 1: Write the failing tests**

`TestGitConfig` (config_test.go): defaults off; with `CIRRUS_GIT=1` and `CIRRUS_DATA_DIR=/d`, `GitDir == "/d/git"`, `GitMaxPush == 1024<<20`; `CIRRUS_GIT_URL=ftp://x` and `CIRRUS_GIT_MAX_PUSH_MB=0` are errors; `https://homeserver.example.ts.net/` becomes `https://homeserver.example.ts.net`.
`TestGitTokens`: `Create("owner@example.com","laptop","read")` returns a secret starting `cxg_`; the JSON file holds no secret, only the hash, mode 600; `Check(secret)` true, `Check(secret+"x")` false; `List` hides the hash; `Revoke` by another owner fails; after `Revoke` `Check` false; scope `admin` and name `""` refused.
`TestGitService`: table — `notes.git/info/refs?service=git-upload-pack` GET → (`notes`, false, true); `…?service=git-receive-pack` → push; `notes.git/git-receive-pack` POST → push; `notes.git/HEAD`, `notes.git/objects/info/packs`, `notes.git/info/refs` without service, `notes.git/git-upload-pack` GET, `../x.git/info/refs` → `ok=false`.
`TestGitHTTPBadPaths`: through `s.Handler()` with the owner header: `/git/notes.git/HEAD` 404, `/git/NOPE.git/info/refs?service=git-upload-pack` 404, `/git/-x.git/info/refs?...` 404.
`TestGitHTTPTokenRules` (needs git): repo `notes`; no credentials → 401 with the `WWW-Authenticate` header; wrong token → 401; read token on `info/refs?service=git-receive-pack` → 403 with body `This token can only read`; read token on upload-pack refs → 200; a read-write token whose owner is then removed from `s.cfg.AllowedUsers` → 401.
`TestGitCloneAndPush` (needs git): start `httptest.NewServer(s.Handler())` (its requests come from 127.0.0.1, which `testConfig` doesn't trust, so the identity header is ignored and the token is what signs in); make a read-write token; in a temp dir `git clone http://x:<secret>@127.0.0.1:<port>/git/notes.git`, commit a file, `git push origin HEAD:main`; then `s.git.Info` shows branch `main`; `s.store.RecentAudit(5)` contains `Pushed to notes` with Who `token "ci" of owner@example.com`.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/gateway/ -run 'Git'`
Expected: FAIL (undefined fields and functions)

- [x] **Step 3: Implement config, `git_tokens.go`, `git_http.go`, server wiring per Interfaces**

- [x] **Step 4: Run them and see them pass**

Run: `go test -race ./internal/gateway/ && go vet ./...`
Expected: `ok`, no vet output.

- [x] **Step 5: Commit**

```bash
git add internal/gateway internal/githost
git commit -m "gateway: smart HTTP git with header or token sign-in"
```

### Task 5: Local git actions with audit and Undo

**Files:**
- Modify: `internal/gateway/actions.go` (`actionSpec.local`, `Validate` arg keys, `postAction`), `internal/gateway/queue.go` (`AddLocal`, `expire`, `Next` skip)
- Create: `internal/gateway/git_actions.go`
- Test: `internal/gateway/git_actions_test.go`

**Interfaces:**
- Consumes: Tasks 2–4 (`Host` methods, `s.git`, `s.record`).
- Produces:
  - `actionSpec` field `local func(s *Server, ctx context.Context, args map[string]string) (undo *ActionReq, out []string, err error)`; `localTimeout time.Duration` (default 2 min; `git_import` 30 min)
  - `Command` field `local bool` (unexported, not JSON). `func (q *Queue) AddLocal(c *Command)` — like `Add` but `State = "sent"`, `local = true`, no notify. `Next` never returns local commands; `expire` skips them.
  - `func (s *Server) runLocal(c *Command, spec actionSpec)` — goroutine body: `context.WithTimeout(context.Background(), spec.localTimeout)`; calls `spec.local`; `q.Complete(c.ID, err == nil, out, msg)` where `msg` is `*githost.Error`'s `Msg`, or "Git hosting is off" / "git failed" otherwise; then records the audit entry exactly as `agentResults` does (extract that block into `func (s *Server) recordCommand(c Command)` and call it from both)
  - `postAction`: after `Validate`, if `spec.local != nil`: if `s.git == nil` → 404 "Git hosting is off"; skip the power check; `c.Undo` is filled by the runner, so set it on the stored command inside `Complete` → add `func (q *Queue) SetUndo(id string, u *ActionReq)` called before `Complete`; `s.queue.AddLocal(c)`; `go s.runLocal(c, spec)`; 202 `{"id"}`.
  - `Validate` accepted keys add: `to`, `description`, `url`, `branch`, `tag`, `ref`, `sha`, `trash`.
  - `func init()` in `git_actions.go` adds to `actionSpecs`: `git_create`, `git_import`, `git_rename`, `git_delete`, `git_restore`, `git_purge`, `git_describe`, `git_default_branch`, `git_branch_delete`, `git_tag_delete`, `git_ref_restore`, with the args and undos of the spec's table. `check` funcs do shape checks only (`ValidName` on `name`/`to`, non-empty `url`, `branch`/`tag` non-empty and not starting `-`). Labels:
    - `Created repository <name>`; `Imported <url without credentials> as repository <name>`; `Renamed repository <name> to <to>`; `Deleted repository <name> (kept in Deleted repositories)`; `Restored repository from <trash>`; `Deleted <trash> for good`; `Changed the description of <name>`; `Made <branch> the default branch of <name>`; `Deleted branch <branch> from <name>`; `Deleted tag <tag> from <name>`; `Restored <ref short> in <name>`
  - Undo args: `git_create`/`git_import` → `git_delete{name}`; `git_rename` → `git_rename{name: to, to: name}`; `git_delete` → `git_restore{trash: id}`; `git_restore` → `git_delete{name: restored}`; `git_describe` → `git_describe{name, description: old}`; `git_default_branch` → same with old; `git_branch_delete` → `git_ref_restore{name, ref: "refs/heads/"+branch, sha}`; `git_tag_delete` → with `refs/tags/`; `git_ref_restore` → `git_branch_delete` or `git_tag_delete` from the ref's prefix; `git_purge` none.

- [x] **Step 1: Write the failing tests**

Helper `waitAction(t, h, id) Command` polls `GET /api/actions/{id}` until not `sent` (2 s cap).
`TestGitActionsAuditAndUndo` (needs git): with the owner headers (`actor`): POST `git_create{name:notes}` → 202; done; audit has `Created repository notes` with `UndoAction == {git_delete, {name:notes}}` and `Undo == "Deleted repository notes (kept in Deleted repositories)"`. Then POST that undo action → done; `s.git.List` empty; audit `UndoAction.Action == "git_restore"`; POST it → `notes` is back.
`TestGitActionsNoWakeNeeded`: the test power view is asleep (split, no snapshot), and `git_create` still returns 202 and completes; `container_start` still gets 409.
`TestGitActionFailures`: `git_rename` of a missing repo → `failed` with error `There's no repository called nope`, audit `… (failed)` with no undo; `git_create{name:"../x"}` → 400 at `Validate`; with `s.git = nil` → 404 `Git hosting is off`.
`TestGitBranchDeleteUndo`: repo with `main`, `dev`; `git_branch_delete{name, branch:dev}` → audit undo `git_ref_restore{name, ref: refs/heads/dev, sha: <dev sha>}`; running it restores `dev`.
`TestGitImport` (needs git): `httptest.NewTLSServer` serving a second gateway's `/git/` (dev mode, its own repo with one commit); `t.Setenv("GIT_SSL_NO_VERIFY", "1")` — and make `githost.run` pass through `GIT_SSL_NO_VERIFY` **only** when set in the process env (document it as test-only in a comment); `git_import{name: copy, url: <tls url>/git/src.git}` → done, `Info(copy)` has `main`. `url: http://192.0.2.1/x.git` → failed `Only https:// addresses can be imported`.
`TestLocalCommandsNotSentToAgent`: after a local action, `GET /agent/commands` (agent token) returns 204 within its poll — use a context with 100 ms deadline on the request.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/gateway/ -run 'GitAction|GitBranch|GitImport|LocalCommands'`
Expected: FAIL

- [x] **Step 3: Implement queue changes, `recordCommand`, `postAction` branch, `git_actions.go`**

- [x] **Step 4: Run them and see them pass**

Run: `go test -race ./internal/gateway/ && bash scripts/check-file-length.sh`
Expected: `ok`; nothing over 400 lines (split `actions.go` if it passes 400).

- [x] **Step 5: Commit**

```bash
git add internal/gateway internal/githost
git commit -m "gateway: git actions run on the gateway, logged and undoable"
```

### Task 6: Read API, tokens API and `/api/me`

**Files:**
- Create: `internal/gateway/git_api.go`
- Modify: `internal/gateway/server.go` (routes, `me` adds `"git": s.git != nil`, `"gitUrl"`)
- Test: `internal/gateway/git_api_test.go`

**Interfaces:**
- Consumes: Tasks 2–4.
- Produces (all on the `api` mux, so behind `s.auth`; each answers 404 "Git hosting is off" when `s.git == nil`):
  - `GET /api/git/repos` → `[]githost.Summary`
  - `GET /api/git/repos/{name}` → `{...Repo, "cloneUrl": "<base>/git/<name>.git"}`; base is `s.cfg.GitURL` or `https://<r.Host>` (`http://` when `s.cfg.Dev`)
  - `GET /api/git/repos/{name}/tree?ref&path`, `/blob?ref&path[&raw=1]`, `/commits?ref&path&page`, `/commit/{sha}`, `/archive?ref&format`
  - raw: `Content-Type: application/octet-stream`, `Content-Disposition: attachment; filename="<base name>"` via `mime.FormatMediaType`, `X-Content-Type-Options: nosniff`; archive: `<name>-<ref or short sha>.tar.gz|zip` filename, ref sanitised to `[A-Za-z0-9._-]`
  - `GET /api/git/trash` → `[]githost.Trashed`
  - `GET /api/git/tokens` → `[]GitToken` of `userOf(r)`; `POST /api/git/tokens` body `{"name","scope"}` → 201 `{"token": GitToken, "secret": "cxg_…"}`, audit `Made Git access token "<name>" (<scope>)`; `DELETE /api/git/tokens/{id}` → 204, audit `Revoked Git access token "<name>"`
  - `func gitStatus(err error) int` — `*githost.Error` Kind: Invalid 400, NotFound 404, Exists 409, Busy 409, Timeout 504, Failed 502; others 500 with "git failed". The body is the `Msg`.
  - `/api/me` adds `"git": bool`.

- [x] **Step 1: Write the failing tests**

`TestGitReadAPI` (needs git): repo with commits; `/api/git/repos` lists it; `/api/git/repos/notes` has `cloneUrl == "https://example.com/git/notes.git"` (httptest's Host) and with `GitURL = "https://homeserver.example.ts.net"` uses that; `tree`, `blob`, `commits?page=1`, `commit/<sha>` answer 200 with the expected JSON fields; `blob?raw=1` has the attachment header and the bytes; `archive?format=zip` has `PK` and filename `notes-main.zip`; `commit/zzz` → 400; `tree?ref=nope` → 404 with body `There's no branch, tag or commit called nope in notes`.
`TestGitTokensAPI`: POST without `X-Cirrus-Request` → 403 (existing CSRF guard); with `actor` → 201 and a `cxg_` secret; GET lists it without `hash`; another user (add `second@example.com` to the allow-list) sees none and can't DELETE it (404); DELETE by the owner → 204; audit has both entries.
`TestMeReportsGit`: `/api/me` has `"git": false` by default and `true` with a host.
`TestGitAPIOff`: with `s.git == nil`, `/api/git/repos` → 404 `Git hosting is off`.

- [x] **Step 2: Run them and see them fail**

Run: `go test ./internal/gateway/ -run 'GitReadAPI|GitTokensAPI|MeReportsGit|GitAPIOff'`
Expected: FAIL

- [x] **Step 3: Implement `git_api.go` and the route and `/api/me` changes**

- [x] **Step 4: Run them and see them pass**

Run: `go test -race ./... && go vet ./... && gofmt -l .`
Expected: all `ok`; no vet or gofmt output.

- [x] **Step 5: Commit**

```bash
git add internal/gateway
git commit -m "gateway: Git read API, access tokens API, git in /api/me"
```

### Task 7: UI foundations — client, helpers, demo data, routing, nav

**Files:**
- Create: `web/src/lib/git/api.ts`, `web/src/lib/git/diff.ts`, `web/src/lib/demo/git.ts`, `web/tests/git.test.ts`
- Modify: `web/src/lib/api.svelte.ts` (`Me.git`, `mode.git`, demo `git: true`), `web/src/lib/state/ui.svelte.ts` (`Route` adds `'git'`; `routeFromHash` takes the first segment; `ui.sub` = the rest; `go(route, sub?)`), `web/src/lib/shell/nav.ts` (Git after Storage, icon `I.GitBranch`, `available: () => mode.git`), `web/src/lib/icons.ts` (export `GitBranch`, `GitCommit`, `Tag`, `FolderGit2`, `Copy`, `Download` from `@lucide/svelte` if missing), `web/src/lib/state/actions.ts` (`sendAction` options `{ where?: string; waitMs?: number }`: the "sent to …" toast says `where` — `the gateway` for git — and the deadline is `waitMs`, default 5 min; git import passes 31 min)

**Interfaces:**
- Produces:
  - Types mirroring the Go JSON: `RepoSummary`, `Repo` (+ `branches`, `tags`, `cloneUrl`), `TreeEntry`, `Tree`, `Blob`, `Commit`, `FileDiff`, `CommitDetail`, `Trashed`, `GitToken`
  - Fetchers: `listRepos()`, `getRepo(name)`, `getTree(name, ref, path)`, `getBlob(name, ref, path)`, `getCommits(name, ref, path, page)`, `getCommit(name, sha)`, `listTrash()`, `listTokens()`, `createToken(name, scope)` → `{token, secret}`, `revokeToken(id)`; `rawUrl(name, ref, path)`, `archiveUrl(name, ref, format)`. When `!mode.live`, each returns data from `demo/git.ts`.
  - `gitAction(action: string, args: Record<string,string>, label: string): Promise<boolean>` — asks with the existing `ask(...)` confirmation, then `sendAction(..., { where: 'the gateway' })`; demo mode toasts "(demo)" like other actions.
  - `parsePatch(patch: string): DiffLine[]` — `DiffLine = { kind: 'hunk' | 'add' | 'del' | 'ctx'; text: string; old?: number; new?: number }`; numbers from `@@ -a,b +c,d @@`; the `diff --git`, `index`, `---`, `+++` header lines are dropped
  - `pathTrail(path: string): { name: string; path: string }[]` — `''` → `[]`; `'a/b'` → `[{name:'a',path:'a'},{name:'b',path:'a/b'}]`
  - `parseGitRoute(sub: string): GitRoute` — `GitRoute = { repo?: string; tab: 'files' | 'commits' | 'commit' | 'branches' | 'tags' | 'settings'; ref?: string; path?: string; sha?: string }`; forms: `''`; `notes`; `notes/files/<ref>/<path…>`; `notes/commits/<ref>`; `notes/commit/<sha>`; `notes/branches`; `notes/tags`; `notes/settings`; segments are `decodeURIComponent`ed. And `gitHref(r: GitRoute): string`, its inverse (`#/git/...`, segments `encodeURIComponent`ed, path slashes kept).

- [x] **Step 1: Write the failing tests** (`web/tests/git.test.ts`)

`parsePatch`: a two-hunk patch gives `hunk` lines and `add`/`del`/`ctx` with right `old`/`new` numbers (e.g. after `@@ -3,2 +3,3 @@`, first ctx is old 3 new 3, an add is new 4 with no old); header lines are dropped; `\ No newline at end of file` is kept as `ctx` with no numbers.
`pathTrail`: the two cases above.
`parseGitRoute` / `gitHref` round-trip: `notes/files/main/src/a%20b.go` → `{repo:'notes', tab:'files', ref:'main', path:'src/a b.go'}` and back; `''` → `{tab:'files'}` with no repo; `notes` → `{repo:'notes', tab:'files'}`; `notes/commit/abc123` → `sha`; a branch with a slash, `feature/x`, survives as one encoded segment.

- [x] **Step 2: Run them and see them fail**

Run: `cd web && npm test`
Expected: FAIL (cannot find `../src/lib/git/diff.ts`)

- [x] **Step 3: Implement the files per Interfaces**

The demo data has three repos (`notes` with a few commits, a folder, a README and one diff; `website` with branches `main`, `redesign` and tag `v1.0`; `empty-project` empty), one trashed repo and one token. Clone URLs use `https://homeserver.example.ts.net/git/<name>.git`.

- [x] **Step 4: Run them and see them pass**

Run: `cd web && npm test && npm run check`
Expected: tests pass; `svelte-check found 0 errors`.

- [x] **Step 5: Commit**

```bash
git add web/src web/tests
git commit -m "web: Git client, diff and route helpers, demo data, nav"
```

### Task 8: UI pages — list, repository view, files, commits, refs, settings

**Files:**
- Create: `web/src/pages/Git.svelte`, `web/src/lib/git/RepoList.svelte`, `NewRepoDialog.svelte`, `Tokens.svelte`, `Trash.svelte`, `RepoView.svelte`, `CloneBox.svelte`, `Files.svelte`, `FileView.svelte`, `Commits.svelte`, `CommitView.svelte`, `Refs.svelte`, `RepoSettings.svelte` (all in `web/src/lib/git/` except the page)
- Modify: `web/src/App.svelte` (`PAGES.git = Git`)

**Interfaces:**
- Consumes: Task 7 (fetchers, `gitAction`, `parsePatch`, `pathTrail`, `parseGitRoute`, `gitHref`, `ui.sub`), existing `Card`, `Button`, `Dialog`, `Badge`, `Segmented`, `toast`, `format.ts`.
- Produces: the page. Behaviour, per spec "UI":
  - `Git.svelte`: `parseGitRoute(ui.sub)`; no repo → `RepoList` + `Tokens` + `Trash`; repo → `RepoView`.
  - `RepoList`: rows link with `gitHref`; buttons **New repository** and **Import** open `NewRepoDialog` (`mode: 'create' | 'import'`; fields name, description or URL; name checked client-side with the same regex as Go; Import's help text says it fetches from that address once, now).
  - `RepoView`: header with name, description, `CloneBox` (URL + Copy via `navigator.clipboard`), a branch `<select>` driving `ref` in the route; tabs with `Segmented` linking `gitHref`. Empty repo: a block with `git remote add homeserver <cloneUrl>` and `git push homeserver main`, using the server name from `mode.server` as the remote name.
  - `Files`: `pathTrail` breadcrumbs; folder rows; a file row opens `FileView` (line-numbered `<pre>`, Binary/TooLarge notes with a Download link to `rawUrl`); README.md text under the root listing; Download `.tar.gz` / `.zip` links to `archiveUrl`.
  - `Commits`: paged list (Older/Newer); a row links to the commit; `CommitView`: message, author, time, per-file sections with change badge, `+n −m`, lines from `parsePatch` coloured with theme tokens (`--ok`, `--danger` backgrounds at low alpha — use the existing token names in `web/src/lib/styles`), `Truncated` note.
  - `Refs` (`kind: 'branches' | 'tags'`): list with Delete → `gitAction('git_branch_delete' | 'git_tag_delete', …)`; no Delete on the default branch.
  - `RepoSettings`: description (save → `git_describe`), default branch select (→ `git_default_branch`), rename (→ `git_rename`, then `go('git', newName)`), delete (→ `git_delete`, then back to the list).
  - `Tokens`: list with scope, created, last used, Revoke (confirm); a form to make one; the secret is shown once in a dialog with Copy and a sentence: "Use it as the password when git asks. It won't be shown again."
  - `Trash`: deleted repos with **Restore** (`git_restore`) and **Delete for good** (`git_purge`; a Dialog with a text field that must equal the name before the button enables).
  - After a successful `gitAction`, the view reloads its data.
  - Each component under 400 lines.

- [x] **Step 1: Build in demo mode and check by hand**

Run: `cd web && npm run check && npm run build`
Expected: `0 errors`; build succeeds.

- [x] **Step 2: Check it in the browser**

Start the UI dev server from `.claude/launch.json` (add a `web` config: `npm run dev` in `web`, port 5174) with `preview_start`. Open `#/git`: the three demo repos show; open `notes`, browse a folder and a file, open a commit and see coloured diff lines; open Settings, Branches on `website`; check Trash and Tokens; resize to mobile and check nothing overflows. Read console messages: no errors.

- [x] **Step 3: Check it against a live gateway**

Run (two terminals, foreground-safe: stop both before reporting): `make web`, then `CIRRUS_GIT=1 make dev-gateway`. In the panel at http://127.0.0.1:8064: create `demo-repo`; from a temp dir `git clone http://127.0.0.1:8064/git/demo-repo.git` (dev mode signs in as `dev`), commit, push; the commit shows in the panel; delete it and Undo from the Changelog. Stop the gateway.

- [x] **Step 4: Commit**

```bash
git add web/src .claude/launch.json
git commit -m "web: Git page — repositories, files, commits, diffs, refs, tokens, trash"
```
(`.claude/` is git-ignored; if `git add` refuses it, leave it out.)

### Task 9: Docs, changelog, full check

**Files:**
- Create: `docs/GIT.md`
- Modify: `docs/CONFIG.md` (the four settings in the gateway.env table), `CHANGELOG.md` (`## [Unreleased]` → `### Added`), `AGENTS.md` (`internal/githost` in Layout), `deploy/gateway/gateway.env.example` (commented `CIRRUS_GIT` lines)

**Interfaces:**
- Consumes: everything above.

- [x] **Step 1: Write `docs/GIT.md`**

Sections: what it is (and isn't yet: SSH, issues, PRs, editing in the browser); turning it on (`CIRRUS_GIT=1`, `git` 2.30+); where repositories live and what to back up (`CIRRUS_GIT_DIR`, `git-tokens.json`), including the split-setup note that they're on the gateway machine; cloning over `tailscale serve` (no password); access tokens and a credential helper (`git config --global credential.helper store` or the OS keychain); importing from GitHub; undo and Deleted repositories; limits (push size, 30 minutes).

- [x] **Step 2: Update CONFIG, CHANGELOG, AGENTS, the env example**

CHANGELOG `### Added` line: "Git hosting (off by default, `CIRRUS_GIT=1`): repositories on your own server, with clone, push and pull over HTTPS, browsing in the panel, access tokens, and undoable changes. See docs/GIT.md."

- [x] **Step 3: Run the full check**

Run: `make test`
Expected: ends without errors (file length, gofmt, vet, go test, svelte-check, UI tests all pass).

Run: `go test -race ./...`
Expected: all `ok`.

- [x] **Step 4: Commit**

```bash
git add docs CHANGELOG.md AGENTS.md deploy/gateway/gateway.env.example
git commit -m "docs: Git hosting"
```
