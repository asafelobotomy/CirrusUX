package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHTTPAuthBeforeLookup(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	// Real and missing repositories, and nonsense paths, all look alike
	// to someone who hasn't signed in.
	for _, p := range []string{"/git/notes.git/info/refs?service=git-upload-pack", "/git/missing.git/info/refs?service=git-upload-pack",
		"/git/notes.git/HEAD", "/git/NOPE.git/x"} {
		w := do(h, "GET", p, "", nil)
		if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d %v", p, w.Code, w.Header())
		}
	}
	if w := do(h, "GET", "/git/missing.git/info/refs?service=git-upload-pack", "", owner); w.Code != http.StatusNotFound {
		t.Errorf("signed in, missing: %d", w.Code)
	}
}

func TestGitTailscaleIdentityBeatsBasic(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	hdr := map[string]string{"Tailscale-User-Login": "owner@example.com"}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetBasicAuth("git", "cxg_added_by_a_proxy")
	hdr["Authorization"] = r.Header.Get("Authorization")
	if w := do(h, "GET", "/git/notes.git/info/refs?service=git-upload-pack", "", hdr); w.Code != http.StatusOK {
		t.Errorf("identity with a stray Basic header: %d", w.Code)
	}
}

func TestGitPushTooBigRefusedUpFront(t *testing.T) {
	s, h := newGitServer(t, func(c *Config) { c.GitMaxPush = 1024 })
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	w := do(h, "POST", "/git/notes.git/git-receive-pack", strings.Repeat("x", 2048), owner)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over the limit: %d %s", w.Code, w.Body)
	}
}

func TestGitPushBlockedWhileRepoChanges(t *testing.T) {
	s, h := newGitServer(t)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	unlock, err := s.git.LockPush("notes")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, c := postGit(t, h, ActionReq{Action: "git_rename", Args: map[string]string{"name": "notes", "to": "other"}})
	if c.State != "failed" {
		t.Errorf("rename during a push: %+v", c)
	}
}

func TestCleanURL(t *testing.T) {
	got := cleanURL("https://bob:secret@example.com/a/b.git?token=abc&x=1#frag")
	if got != "https://example.com/a/b.git" {
		t.Errorf("cleanURL = %q", got)
	}
}

func TestQueueNeverTrimsRunningCommands(t *testing.T) {
	q := NewQueue()
	first := &Command{}
	q.AddLocal(first) // a long import, still running
	var pending []*Command
	for range keepHistory + 20 {
		c := &Command{}
		q.AddLocal(c)
		q.CompleteLocal(c.ID, true, nil, "", nil)
		p := &Command{}
		q.Add(p)
		pending = append(pending, p)
	}
	if _, ok := q.CompleteLocal(first.ID, true, nil, "", nil); !ok {
		t.Error("the running command was trimmed")
	}
	if _, ok := q.Get(pending[0].ID); !ok {
		t.Error("a pending command was trimmed")
	}
	q.mu.Lock()
	finished := 0
	for _, c := range q.cmds {
		if c.State == "done" {
			finished++
		}
	}
	q.mu.Unlock()
	if finished > keepHistory {
		t.Errorf("%d finished commands kept", finished)
	}
}

func TestRunLocalRecordsWhenCommandIsGone(t *testing.T) {
	s, _ := newGitServer(t)
	c := Command{ID: "gone", Label: "Created repository notes", Who: "owner@example.com", ReadOnly: false}
	spec := actionSpecs["git_create"]
	s.runLocal(c, gitArgs{"name": "notes"}, spec)
	e := lastAudit(s)
	if e.What != "Created repository notes" || e.UndoAction == nil || time.Since(e.At) > time.Minute {
		t.Errorf("audit: %+v", e)
	}
}

func TestRunLocalStopsWithServer(t *testing.T) {
	s, _ := newGitServer(t)
	s.Close()
	c := Command{ID: "x", Label: "Created repository late", Who: "owner@example.com"}
	s.runLocal(c, gitArgs{"name": "late"}, actionSpecs["git_create"])
	if e := lastAudit(s); !strings.HasSuffix(e.What, "(failed)") {
		t.Errorf("after Close: %+v", e)
	}
}
