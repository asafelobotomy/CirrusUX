package gateway

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// newGitServer is the test server with Git hosting on.
func newGitServer(t *testing.T, tweak ...func(*Config)) (*Server, http.Handler) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.DataDir, cfg.Git, cfg.GitDir, cfg.GitMaxPush = dir, true, filepath.Join(dir, "git"), 64<<20
	for _, f := range tweak {
		f(&cfg)
	}
	s := NewServer(cfg, store, testPower(), fstest.MapFS{"index.html": {Data: []byte("x")}})
	if s.git == nil {
		t.Fatal("git hosting didn't start")
	}
	return s, s.Handler()
}

// runGit runs a person's git in dir.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitService(t *testing.T) {
	q := func(s string) url.Values { v, _ := url.ParseQuery(s); return v }
	cases := []struct {
		rest, query, method string
		repo                string
		push, ok            bool
	}{
		{"notes.git/info/refs", "service=git-upload-pack", "GET", "notes", false, true},
		{"notes.git/info/refs", "service=git-receive-pack", "GET", "notes", true, true},
		{"notes.git/git-upload-pack", "", "POST", "notes", false, true},
		{"notes.git/git-receive-pack", "", "POST", "notes", true, true},
		{"notes.git/HEAD", "", "GET", "", false, false},
		{"notes.git/objects/info/packs", "", "GET", "", false, false},
		{"notes.git/info/refs", "", "GET", "", false, false},
		{"notes.git/info/refs", "service=git-upload-archive", "GET", "", false, false},
		{"notes.git/git-upload-pack", "", "GET", "", false, false},
		{"notes.git/info/refs", "service=git-upload-pack", "POST", "", false, false},
		{"notes/info/refs", "service=git-upload-pack", "GET", "", false, false},
	}
	for _, c := range cases {
		repo, push, ok := gitService(c.rest, q(c.query), c.method)
		if repo != c.repo || push != c.push || ok != c.ok {
			t.Errorf("%s %s?%s = %q %v %v", c.method, c.rest, c.query, repo, push, ok)
		}
	}
}

func TestGitHTTPBadPaths(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/git/notes.git/HEAD", "/git/NOPE.git/info/refs?service=git-upload-pack",
		"/git/-x.git/info/refs?service=git-upload-pack", "/git/missing.git/info/refs?service=git-upload-pack"} {
		if w := do(h, "GET", p, "", owner); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	// The mux cleans dot-dot paths with a redirect away from /git/.
	if w := do(h, "GET", "/git/notes.git/../../etc/passwd", "", owner); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/etc/passwd" {
		t.Errorf("dot-dot: %d %v", w.Code, w.Header())
	}
	if w := do(h, "GET", "/git/notes.git/info/refs?service=git-upload-pack", "", owner); w.Code != http.StatusOK ||
		!strings.Contains(w.Header().Get("Content-Type"), "git-upload-pack-advertisement") {
		t.Errorf("signed-in refs: %d %v", w.Code, w.Header())
	}
}

func TestGitHTTPTokenRules(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	refs := "/git/notes.git/info/refs?service="
	basic := func(secret string) map[string]string {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("git", secret)
		return map[string]string{"Authorization": r.Header.Get("Authorization")}
	}
	w := do(h, "GET", refs+"git-upload-pack", "", nil)
	if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") != `Basic realm="CirrusUX Git"` {
		t.Errorf("no credentials: %d %v", w.Code, w.Header())
	}
	if w := do(h, "GET", refs+"git-upload-pack", "", basic("cxg_wrong")); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", w.Code)
	}
	read, _, _ := s.gitTokens.Create("owner@example.com", "reader", "read")
	if w := do(h, "GET", refs+"git-upload-pack", "", basic(read)); w.Code != http.StatusOK {
		t.Errorf("read token fetch: %d", w.Code)
	}
	if w := do(h, "GET", refs+"git-receive-pack", "", basic(read)); w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "This token can only read" {
		t.Errorf("read token push: %d %q", w.Code, w.Body.String())
	}
	rw, _, _ := s.gitTokens.Create("owner@example.com", "ci", "read-write")
	if w := do(h, "GET", refs+"git-receive-pack", "", basic(rw)); w.Code != http.StatusOK {
		t.Errorf("read-write push refs: %d", w.Code)
	}
	s.cfg.AllowedUsers = []string{"someone@example.com"}
	if w := do(h, "GET", refs+"git-upload-pack", "", basic(rw)); w.Code != http.StatusUnauthorized {
		t.Errorf("owner no longer allowed: %d", w.Code)
	}
	// The identity header only counts from a trusted proxy and for an
	// allowed user.
	if w := do(h, "GET", refs+"git-upload-pack", "", owner); w.Code != http.StatusUnauthorized {
		t.Errorf("header of a user who isn't allowed: %d", w.Code)
	}
}

func TestGitCloneAndPush(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h) // from 127.0.0.1, which isn't a trusted proxy: the token signs in
	defer srv.Close()
	secret, _, _ := s.gitTokens.Create("owner@example.com", "ci", "read-write")
	u, _ := url.Parse(srv.URL + "/git/notes.git")
	u.User = url.UserPassword("git", secret)

	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=main", "clone", "-q", u.String(), ".")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Over 1 MiB, so git sends the push chunked.
	big := make([]byte, 3<<20)
	_, _ = rand.Read(big)
	if err := os.WriteFile(filepath.Join(work, "big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-q", "-m", "First")
	runGit(t, work, "push", "-q", "origin", "HEAD:main")
	sha := runGit(t, work, "rev-parse", "HEAD")

	r, err := s.git.Info(t.Context(), "notes")
	if err != nil || len(r.Branches) != 1 || r.Branches[0].Commit != sha {
		t.Fatalf("after push: %+v %v", r.Branches, err)
	}
	found := false
	for _, e := range s.store.RecentAudit(5) {
		found = found || e.What == "Pushed to notes" && e.Who == `token "ci" of owner@example.com`
	}
	if !found {
		t.Errorf("no push in the audit log: %+v", s.store.RecentAudit(5))
	}
	// A second clone fetches it back.
	again := t.TempDir()
	runGit(t, again, "clone", "-q", u.String(), ".")
	if got := runGit(t, again, "rev-parse", "HEAD"); got != sha {
		t.Errorf("clone got %s, want %s", got, sha)
	}
}
