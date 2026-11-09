package githost

import (
	"os"
	"strings"
	"testing"
)

func TestCreateListRename(t *testing.T) {
	h := newHost(t)
	if err := h.Create(ctx, "notes", "My notes"); err != nil {
		t.Fatal(err)
	}
	list, err := h.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v %v", list, err)
	}
	s := list[0]
	if s.Name != "notes" || s.Description != "My notes" || s.DefaultBranch != "main" || !s.Empty || s.LastCommit != nil {
		t.Errorf("summary: %+v", s)
	}
	for k, want := range map[string]string{"gc.pruneExpire": "30.days.ago", "gc.reflogExpireUnreachable": "30.days.ago", "http.receivepack": "true", "core.logAllRefUpdates": "true"} {
		if got := gitIn(t, h.path("notes"), "config", k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if err := h.Create(ctx, "notes", ""); !isKind(err, Exists) {
		t.Errorf("create twice: %v", err)
	}
	if err := h.Create(ctx, "../x", ""); !isKind(err, Invalid) {
		t.Errorf("bad name: %v", err)
	}
	if err := h.Rename(ctx, "notes", "diary"); err != nil {
		t.Fatal(err)
	}
	if list, _ := h.List(ctx); len(list) != 1 || list[0].Name != "diary" {
		t.Errorf("after rename: %+v", list)
	}
	if err := h.Rename(ctx, "notes", "x"); !isKind(err, NotFound) || err.Error() != "There's no repository called notes" {
		t.Errorf("rename missing: %v", err)
	}
	mustCreate(t, h, "other")
	if err := h.Rename(ctx, "other", "diary"); !isKind(err, Exists) || err.Error() != "A repository called diary already exists" {
		t.Errorf("rename onto taken: %v", err)
	}
}

func TestInfoAfterCommit(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a.txt": "a"}, "First")
	commitFiles(t, h, "notes", map[string]string{"a.txt": "b"}, "Second")
	commitOn(t, h, "notes", "dev", map[string]string{"d.txt": "d"}, "On dev")
	work := t.TempDir()
	gitIn(t, work, "clone", "-q", h.path("notes"), ".")
	gitIn(t, work, "tag", "-a", "v1", "-m", "Version 1")
	gitIn(t, work, "push", "-q", "origin", "v1")

	r, err := h.Info(ctx, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if r.Empty || r.LastCommit == nil || r.DefaultBranch != "main" {
		t.Errorf("summary: %+v", r.Summary)
	}
	subjects := map[string]string{}
	for _, b := range r.Branches {
		subjects[b.Name] = b.Subject
		if len(b.Commit) != 40 || b.Time.IsZero() {
			t.Errorf("branch %+v", b)
		}
	}
	if subjects["main"] != "Second" || subjects["dev"] != "On dev" || len(r.Branches) != 2 {
		t.Errorf("branches: %+v", r.Branches)
	}
	main := gitIn(t, h.path("notes"), "rev-parse", "main")
	if len(r.Tags) != 1 || r.Tags[0].Name != "v1" || r.Tags[0].Commit != main {
		t.Errorf("tags: %+v (main %s)", r.Tags, main)
	}
	if _, err := h.Info(ctx, "nope"); !isKind(err, NotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestDescriptionAndDefaultBranch(t *testing.T) {
	h := newHost(t)
	if err := h.Create(ctx, "notes", "Old"); err != nil {
		t.Fatal(err)
	}
	if old, err := h.SetDescription(ctx, "notes", "New"); err != nil || old != "Old" {
		t.Errorf("SetDescription: %q %v", old, err)
	}
	for _, bad := range []string{strings.Repeat("a", 201), "a\nb"} {
		if _, err := h.SetDescription(ctx, "notes", bad); !isKind(err, Invalid) {
			t.Errorf("description %q: %v", bad, err)
		}
	}
	commitFiles(t, h, "notes", map[string]string{"a": "a"}, "One")
	commitOn(t, h, "notes", "dev", nil, "Dev")
	if old, err := h.SetDefaultBranch(ctx, "notes", "dev"); err != nil || old != "main" {
		t.Errorf("SetDefaultBranch: %q %v", old, err)
	}
	if r, _ := h.Info(ctx, "notes"); r.DefaultBranch != "dev" || r.Description != "New" {
		t.Errorf("after: %+v", r.Summary)
	}
	_, err := h.SetDefaultBranch(ctx, "notes", "nope")
	if !isKind(err, NotFound) || err.Error() != "There's no branch called nope in notes" {
		t.Errorf("missing branch: %v", err)
	}
	if _, err := h.SetDefaultBranch(ctx, "notes", "-x"); !isKind(err, Invalid) {
		t.Errorf("option-like branch: %v", err)
	}
}

func TestImportRefusesNonHTTPS(t *testing.T) {
	h := newHost(t)
	for _, u := range []string{"file:///x", "http://192.0.2.1/x.git", "ssh://192.0.2.1/x", "/tmp/x", "ext::sh -c x", "https://", "https:///x"} {
		err := h.Import(ctx, "copy", u)
		if !isKind(err, Invalid) || err.Error() != "Only https:// addresses can be imported" {
			t.Errorf("Import(%q): %v", u, err)
		}
	}
	entries, _ := os.ReadDir(h.Dir)
	if len(entries) != 1 || entries[0].Name() != ".trash" {
		t.Errorf("left behind: %v", entries)
	}
}

func TestRedactURL(t *testing.T) {
	got := redactURL("fatal: unable to access 'https://bob:secret@192.0.2.1/x.git/': 403")
	if strings.Contains(got, "secret") || strings.Contains(got, "bob") || !strings.Contains(got, "https://192.0.2.1/x.git/") {
		t.Errorf("redactURL: %q", got)
	}
}

func TestLockBlocksRename(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	unlock, _ := h.lock("notes")
	defer unlock()
	if err := h.Rename(ctx, "notes", "x"); !isKind(err, Busy) {
		t.Errorf("rename while busy: %v", err)
	}
}
