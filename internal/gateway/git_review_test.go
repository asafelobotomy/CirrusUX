package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Behind a reverse proxy (header mode), /git/ is often passed through
// without the proxy's login, so a client could send the identity header
// itself. There only tokens count.
func TestGitHeaderModeIgnoresIdentityHeader(t *testing.T) {
	s, h := newGitServer(t, func(c *Config) { c.Auth = "header" })
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	forged := map[string]string{"Remote-User": "owner@example.com"}
	if w := do(h, "GET", "/git/notes.git/info/refs?service=git-receive-pack", "", forged); w.Code != http.StatusUnauthorized {
		t.Errorf("forged header on /git/: %d", w.Code)
	}
	secret, _, _ := s.gitTokens.Create("owner@example.com", "ci", "read-write")
	r := httptest.NewRequest("GET", "/git/notes.git/info/refs?service=git-receive-pack", nil)
	r.SetBasicAuth("git", secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("token in header mode: %d", w.Code)
	}
}

// An import address's password stays out of what the API shows.
func TestGitImportURLPasswordNotShown(t *testing.T) {
	_, h := newGitServer(t)
	_, c := postGit(t, h, ActionReq{Action: "git_import", Args: map[string]string{"name": "x", "url": "https://bob:s3cret@127.0.0.1:1/x.git"}})
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "s3cret") || strings.Contains(string(b), "bob:") {
		t.Errorf("command shows the password: %s", b)
	}
	if c.Args["url"] != "https://127.0.0.1:1/x.git" {
		t.Errorf("url kept as %q", c.Args["url"])
	}
}

// A first push of master (not main) makes master the default branch.
func TestGitFirstPushSetsDefaultBranch(t *testing.T) {
	s, h := newGitServer(t, devMode)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=master", "clone", "-q", srv.URL+"/git/notes.git", ".")
	runGit(t, work, "checkout", "-q", "-b", "master")
	runGit(t, work, "commit", "-q", "--allow-empty", "-m", "One")
	runGit(t, work, "push", "-q", "origin", "master")
	r, err := s.git.Info(t.Context(), "notes")
	if err != nil || r.DefaultBranch != "master" {
		t.Fatalf("default branch: %+v %v", r.Summary, err)
	}
	if tr, err := s.git.Tree(t.Context(), "notes", "", ""); err != nil || tr.Empty {
		t.Errorf("tree at the default branch: %+v %v", tr, err)
	}
}
