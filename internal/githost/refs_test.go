package githost

import "testing"

func TestDeleteRestoreRef(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a": "a"}, "One")
	dev := commitOn(t, h, "notes", "dev", map[string]string{"b": "b"}, "Dev")
	work := t.TempDir()
	gitIn(t, work, "clone", "-q", h.path("notes"), ".")
	gitIn(t, work, "tag", "v1")
	gitIn(t, work, "push", "-q", "origin", "v1")

	sha, err := h.DeleteRef(ctx, "notes", "refs/heads/dev")
	if err != nil || sha != dev {
		t.Fatalf("DeleteRef: %q %v (want %s)", sha, err, dev)
	}
	if r, _ := h.Info(ctx, "notes"); len(r.Branches) != 1 {
		t.Errorf("dev still listed: %+v", r.Branches)
	}
	if err := h.RestoreRef(ctx, "notes", "refs/heads/dev", sha); err != nil {
		t.Fatal(err)
	}
	if r, _ := h.Info(ctx, "notes"); len(r.Branches) != 2 {
		t.Errorf("dev not back: %+v", r.Branches)
	}
	if err := h.RestoreRef(ctx, "notes", "refs/heads/dev", sha); !isKind(err, Exists) || err.Error() != "dev already exists" {
		t.Errorf("restore twice: %v", err)
	}
	if _, err := h.DeleteRef(ctx, "notes", "refs/heads/main"); !isKind(err, Invalid) || err.Error() != "main is the default branch. Pick another default first" {
		t.Errorf("delete default: %v", err)
	}
	if _, err := h.DeleteRef(ctx, "notes", "refs/heads/x"); !isKind(err, NotFound) {
		t.Errorf("delete missing: %v", err)
	}
	for _, bad := range []string{"refs/remotes/x", "main", "refs/heads/-x", "refs/heads/a..b"} {
		if _, err := h.DeleteRef(ctx, "notes", bad); !isKind(err, Invalid) {
			t.Errorf("DeleteRef(%q): %v", bad, err)
		}
	}

	tagSHA, err := h.DeleteRef(ctx, "notes", "refs/tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := h.Info(ctx, "notes"); len(r.Tags) != 0 {
		t.Errorf("tag still there: %+v", r.Tags)
	}
	if err := h.RestoreRef(ctx, "notes", "refs/tags/v1", tagSHA); err != nil {
		t.Fatal(err)
	}
	if err := h.RestoreRef(ctx, "notes", "refs/tags/v2", "0000000000000000000000000000000000000000"); !isKind(err, NotFound) {
		t.Errorf("restore at a missing commit: %v", err)
	}
}
