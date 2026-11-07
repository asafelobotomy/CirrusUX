package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getJSON(t *testing.T, h http.Handler, path string, v any) int {
	t.Helper()
	w := do(h, "GET", path, "", owner)
	if w.Code == http.StatusOK && v != nil {
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return w.Code
}

func TestGitReadAPI(t *testing.T) {
	s, h := newGitServer(t, devMode)
	if err := s.git.Create(t.Context(), "notes", "My notes"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=main", "clone", "-q", srv.URL+"/git/notes.git", ".")
	runGit(t, work, "checkout", "-q", "-b", "main")
	for _, f := range [][2]string{{"README.md", "hello"}, {"logo.bin", "\x00\x01"}} {
		writeFile(t, work+"/"+f[0], f[1])
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-q", "-m", "First")
	runGit(t, work, "push", "-q", "origin", "HEAD:main")
	sha := runGit(t, work, "rev-parse", "HEAD")

	var list []map[string]any
	if c := getJSON(t, h, "/api/git/repos", &list); c != 200 || len(list) != 1 || list[0]["name"] != "notes" || list[0]["description"] != "My notes" {
		t.Errorf("list: %d %v", c, list)
	}
	var repo map[string]any
	if c := getJSON(t, h, "/api/git/repos/notes", &repo); c != 200 || repo["cloneUrl"] != "http://example.com/git/notes.git" || len(repo["branches"].([]any)) != 1 {
		t.Errorf("repo: %d %v", c, repo)
	}
	var tree struct {
		Commit  string
		Entries []struct{ Name, Type string }
	}
	if c := getJSON(t, h, "/api/git/repos/notes/tree?ref=main", &tree); c != 200 || tree.Commit != sha || len(tree.Entries) != 2 {
		t.Errorf("tree: %d %+v", c, tree)
	}
	var blob struct{ Text string }
	if c := getJSON(t, h, "/api/git/repos/notes/blob?path=README.md", &blob); c != 200 || blob.Text != "hello" {
		t.Errorf("blob: %d %+v", c, blob)
	}
	w := do(h, "GET", "/api/git/repos/notes/blob?path=logo.bin&raw=1", "", owner)
	if w.Code != 200 || w.Body.String() != "\x00\x01" || w.Header().Get("Content-Type") != "application/octet-stream" ||
		w.Header().Get("Content-Disposition") != `attachment; filename=logo.bin` || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("raw: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	var commits []map[string]any
	if c := getJSON(t, h, "/api/git/repos/notes/commits?page=1", &commits); c != 200 || len(commits) != 1 || commits[0]["sha"] != sha || commits[0]["subject"] != "First" {
		t.Errorf("commits: %d %v", c, commits)
	}
	var detail map[string]any
	if c := getJSON(t, h, "/api/git/repos/notes/commit/"+sha, &detail); c != 200 || len(detail["files"].([]any)) != 2 {
		t.Errorf("commit: %d %v", c, detail)
	}
	w = do(h, "GET", "/api/git/repos/notes/archive?format=zip", "", owner)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "PK") || w.Header().Get("Content-Disposition") != `attachment; filename=notes-main.zip` {
		t.Errorf("archive: %d %v", w.Code, w.Header())
	}
	if w := do(h, "GET", "/api/git/repos/notes/blob?path=nope&raw=1", "", owner); w.Code != 404 || w.Header().Get("Content-Disposition") != "" {
		t.Errorf("raw missing file: %d %v", w.Code, w.Header())
	}
	if w := do(h, "GET", "/api/git/repos/notes/archive?format=rar", "", owner); w.Code != 400 {
		t.Errorf("bad format: %d", w.Code)
	}
	if c := getJSON(t, h, "/api/git/repos/notes/commit/zzz", nil); c != 400 {
		t.Errorf("bad sha: %d", c)
	}
	w = do(h, "GET", "/api/git/repos/notes/tree?ref=nope", "", owner)
	if w.Code != 404 || strings.TrimSpace(w.Body.String()) != "There's no branch, tag or commit called nope in notes" {
		t.Errorf("missing ref: %d %q", w.Code, w.Body.String())
	}
	if c := getJSON(t, h, "/api/git/repos/nope", nil); c != 404 {
		t.Errorf("missing repo: %d", c)
	}
	if c := getJSON(t, h, "/api/git/repos/notes/commits?page=x", nil); c != 400 {
		t.Errorf("bad page: %d", c)
	}
	var trash []any
	if c := getJSON(t, h, "/api/git/trash", &trash); c != 200 || trash == nil {
		t.Errorf("trash: %d %v", c, trash)
	}
}

func TestGitCloneURLSetting(t *testing.T) {
	s, h := newGitServer(t, func(c *Config) { c.GitURL = "https://homeserver.example.ts.net" })
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	var repo map[string]any
	if getJSON(t, h, "/api/git/repos/notes", &repo); repo["cloneUrl"] != "https://homeserver.example.ts.net/git/notes.git" {
		t.Errorf("clone URL: %v", repo["cloneUrl"])
	}
	// Without the setting, the address the panel was opened on, as https
	// outside dev mode.
	s2, h2 := newGitServer(t)
	if err := s2.git.Create(t.Context(), "x", ""); err != nil {
		t.Fatal(err)
	}
	if getJSON(t, h2, "/api/git/repos/x", &repo); repo["cloneUrl"] != "https://example.com/git/x.git" {
		t.Errorf("clone URL from the request: %v", repo["cloneUrl"])
	}
}

func TestGitTokensAPI(t *testing.T) {
	s, h := newGitServer(t, func(c *Config) { c.AllowedUsers = append(c.AllowedUsers, "second@example.com") })
	body := `{"name":"laptop","scope":"read-write"}`
	if w := do(h, "POST", "/api/git/tokens", body, owner); w.Code != http.StatusForbidden {
		t.Errorf("without the CSRF header: %d", w.Code)
	}
	w := do(h, "POST", "/api/git/tokens", body, actor)
	var made struct {
		Token  GitToken
		Secret string
	}
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	if w.Code != http.StatusCreated || !strings.HasPrefix(made.Secret, "cxg_") || made.Token.Name != "laptop" {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/git/tokens", `{"name":"x","scope":"admin"}`, actor); w.Code != http.StatusBadRequest {
		t.Errorf("bad scope: %d", w.Code)
	}
	w = do(h, "GET", "/api/git/tokens", "", owner)
	if w.Code != 200 || strings.Contains(w.Body.String(), "hash") || !strings.Contains(w.Body.String(), "laptop") {
		t.Errorf("list: %d %s", w.Code, w.Body)
	}
	second := map[string]string{"Tailscale-User-Login": "second@example.com", "X-Cirrus-Request": "1"}
	if w := do(h, "GET", "/api/git/tokens", "", second); w.Body.String() != "[]\n" {
		t.Errorf("second user sees: %s", w.Body)
	}
	if w := do(h, "DELETE", "/api/git/tokens/"+made.Token.ID, "", second); w.Code != http.StatusNotFound {
		t.Errorf("second user revokes: %d", w.Code)
	}
	if w := do(h, "DELETE", "/api/git/tokens/"+made.Token.ID, "", actor); w.Code != http.StatusNoContent {
		t.Errorf("revoke: %d", w.Code)
	}
	a := s.store.RecentAudit(5)
	if len(a) < 2 || a[1].What != `Made Git access token "laptop" (read-write)` || a[0].What != `Revoked Git access token "laptop"` {
		t.Errorf("audit: %+v", a)
	}
}

func TestMeReportsGit(t *testing.T) {
	_, h := newTestServer(t)
	var me map[string]any
	if getJSON(t, h, "/api/me", &me); me["git"] != false {
		t.Errorf("off: %v", me["git"])
	}
	_, h = newGitServer(t)
	if getJSON(t, h, "/api/me", &me); me["git"] != true {
		t.Errorf("on: %v", me["git"])
	}
}

func TestGitAPIOff(t *testing.T) {
	_, h := newTestServer(t)
	w := do(h, "GET", "/api/git/repos", "", owner)
	if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != "Git hosting is off" {
		t.Errorf("off: %d %q", w.Code, w.Body.String())
	}
}
