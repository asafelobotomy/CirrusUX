package githost

import (
	"strings"
	"testing"
	"time"
)

// A deleted branch can be restored after git cleans up, even when its
// commits are old: a hidden ref keeps them.
func TestRestoreRefAfterGC(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a": "a"}, "One")
	sha := commitOn(t, h, "notes", "dev", map[string]string{"b": "b"}, "Dev")
	if _, err := h.DeleteRef(ctx, "notes", "refs/heads/dev"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h.path("notes"), "gc", "-q", "--prune=now")
	if err := h.RestoreRef(ctx, "notes", "refs/heads/dev", sha); err != nil {
		t.Fatalf("restore after gc: %v", err)
	}
	// The keeping ref isn't offered to clients.
	if out := gitIn(t, t.TempDir(), "ls-remote", h.path("notes")); strings.Contains(out, "refs/cirrus") {
		t.Errorf("hidden ref advertised:\n%s", out)
	}
}

// Keeping refs older than 30 days are dropped on the next delete.
func TestOldKeepRefsDropped(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a": "a"}, "One")
	commitOn(t, h, "notes", "a", nil, "A")
	commitOn(t, h, "notes", "b", nil, "B")
	h.now = func() time.Time { return time.Now().Add(-31 * 24 * time.Hour) }
	if _, err := h.DeleteRef(ctx, "notes", "refs/heads/a"); err != nil {
		t.Fatal(err)
	}
	h.now = time.Now
	if _, err := h.DeleteRef(ctx, "notes", "refs/heads/b"); err != nil {
		t.Fatal(err)
	}
	out := gitIn(t, h.path("notes"), "for-each-ref", "--format=%(refname)", "refs/cirrus")
	if strings.Contains(out, "/heads/a") || !strings.Contains(out, "/heads/b") {
		t.Errorf("keep refs: %q", out)
	}
}

func TestRedactArgs(t *testing.T) {
	got := redactArgs([]string{"clone", "--", "https://bob:s3cret@192.0.2.1/x.git", "dir"})
	if strings.Contains(strings.Join(got, " "), "s3cret") {
		t.Errorf("redactArgs: %v", got)
	}
}

// A repository whose HEAD names a missing branch, with other branches,
// shows the newest one rather than an error.
func TestResolveFallsBackWhenHeadUnborn(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitOn(t, h, "notes", "master", map[string]string{"a": "a"}, "One")
	tr, err := h.Tree(ctx, "notes", "", "")
	if err != nil || tr.Empty || len(tr.Entries) != 1 {
		t.Errorf("tree: %+v %v", tr, err)
	}
}
