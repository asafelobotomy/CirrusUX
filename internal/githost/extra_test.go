package githost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergeCommitShowsChanges(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"base.txt": "base\n"}, "Base")
	commitOn(t, h, "notes", "dev", map[string]string{"dev.txt": "dev\n"}, "On dev")
	commitFiles(t, h, "notes", map[string]string{"main.txt": "main\n"}, "On main")

	work := t.TempDir()
	gitIn(t, work, "clone", "-q", h.path("notes"), ".")
	gitIn(t, work, "merge", "-q", "--no-ff", "-m", "Merge dev", "origin/dev")
	gitIn(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")
	sha := gitIn(t, work, "rev-parse", "HEAD")

	d, err := h.CommitDetail(ctx, "notes", sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Parents) != 2 || len(d.Files) != 1 || d.Files[0].Path != "dev.txt" || d.Files[0].Change != "added" {
		t.Errorf("merge: parents %v, files %+v", d.Parents, d.Files)
	}
}

func TestNonASCIIPathIsNotQuoted(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	sha := commitFiles(t, h, "notes", map[string]string{"café/naïve.txt": "x\n"}, "Accents")
	d, err := h.CommitDetail(ctx, "notes", sha)
	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "café/naïve.txt" {
		t.Errorf("files: %+v %v", d.Files, err)
	}
}

func TestDashPathAndLiteralPathspec(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"-x": "dash\n", "a.txt": "a\n"}, "One")
	b, err := h.Blob(ctx, "notes", "", "-x")
	if err != nil || b.Text != "dash\n" {
		t.Errorf("blob -x: %+v %v", b, err)
	}
	// "*.txt" is a file name, not a glob.
	list, err := h.Commits(ctx, "notes", "", "*.txt", 1)
	if err != nil || len(list) != 0 {
		t.Errorf("glob path matched: %v %v", list, err)
	}
	list, err = h.Commits(ctx, "notes", "", "a.txt", 1)
	if err != nil || len(list) != 1 {
		t.Errorf("a.txt: %v %v", list, err)
	}
	if _, err := h.Commits(ctx, "notes", "", "", maxPage+1); !isKind(err, Invalid) {
		t.Errorf("huge page: %v", err)
	}
}

func TestSizeIsCached(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	p := h.path("notes")
	first := h.size(p)
	if err := os.WriteFile(filepath.Join(p, "big"), make([]byte, 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := h.size(p); got != first {
		t.Errorf("size changed within the TTL: %d, then %d", first, got)
	}
	c := h.now
	h.now = func() time.Time { return c().Add(2 * sizeTTL) }
	if got := h.size(p); got != first+5000 {
		t.Errorf("size after the TTL: %d, want %d", got, first+5000)
	}
}

func TestPushLockAndAdminChanges(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	a, err := h.LockPush("notes")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.LockPush("notes")
	if err != nil {
		t.Fatalf("two pushes at once: %v", err)
	}
	if err := h.Rename(ctx, "notes", "x"); !isKind(err, Busy) {
		t.Errorf("rename during push: %v", err)
	}
	if _, err := h.Delete(ctx, "notes"); !isKind(err, Busy) {
		t.Errorf("delete during push: %v", err)
	}
	if _, err := h.SetDefaultBranch(ctx, "notes", "dev"); !isKind(err, Busy) {
		t.Errorf("default branch during push: %v", err)
	}
	a()
	b()
	unlock, _ := h.lock("notes")
	if _, err := h.LockPush("notes"); !isKind(err, Busy) {
		t.Errorf("push during change: %v", err)
	}
	unlock()
}

func TestBackendSlots(t *testing.T) {
	h := newHost(t)
	var rel []func()
	for range maxBackends {
		r, err := h.AcquireBackend(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rel = append(rel, r)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.AcquireBackend(cctx); !isKind(err, Timeout) {
		t.Errorf("full: %v", err)
	}
	rel[0]()
	if r, err := h.AcquireBackend(ctx); err != nil {
		t.Errorf("after release: %v", err)
	} else {
		r()
	}
}

func TestNewSweepsTmpDirs(t *testing.T) {
	h := newHost(t)
	stale := filepath.Join(h.Dir, ".tmp-copy-abc")
	if err := os.MkdirAll(filepath.Join(stale, "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(h.Dir, h.home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale import folder kept: %v", err)
	}
}

func TestImportRefusesPrivateAddresses(t *testing.T) {
	h := newHost(t)
	for _, u := range []string{"https://127.0.0.1/x.git", "https://10.1.2.3/x.git", "https://192.168.0.5/x.git", "https://169.254.169.254/x.git",
		"https://[::1]/x.git", "https://100.100.1.1/x.git", "https://localhost/x.git"} {
		err := h.Import(ctx, "copy", u)
		if !isKind(err, Invalid) || !strings.Contains(err.Error(), "CIRRUS_GIT_IMPORT_PRIVATE") {
			t.Errorf("Import(%q): %v", u, err)
		}
	}
}

func TestConfigParams(t *testing.T) {
	got := configParams("http.extraHeader", "Authorization: Basic abc", "a.b", "it's")
	want := `'http.extraHeader=Authorization: Basic abc' 'a.b=it'\''s'`
	if got != want {
		t.Errorf("configParams = %q, want %q", got, want)
	}
}

func TestDescriptionWrittenAtomically(t *testing.T) {
	h := newHost(t)
	if err := h.Create(ctx, "notes", "Hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SetDescription(ctx, "notes", "Changed"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(h.path("notes"), "description"))
	if string(b) != "Changed" {
		t.Errorf("description: %q", b)
	}
	entries, _ := os.ReadDir(h.path("notes"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}
