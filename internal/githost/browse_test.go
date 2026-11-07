package githost

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestTreeAndBlob(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{
		"README.md":   "hello",
		"src/main.go": "package main",
		"logo.bin":    "\x00\x01",
		"big.txt":     strings.Repeat("a", 1<<20+1),
	}, "Files")

	tr, err := h.Tree(ctx, "notes", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range tr.Entries {
		got = append(got, e.Name+":"+e.Type)
	}
	if strings.Join(got, ",") != "src:folder,README.md:file,big.txt:file,logo.bin:file" || len(tr.Commit) != 40 || tr.Empty {
		t.Errorf("root tree: %v %+v", got, tr)
	}
	if tr.Entries[1].Size != 5 {
		t.Errorf("README size: %d", tr.Entries[1].Size)
	}
	if sub, err := h.Tree(ctx, "notes", "main", "src"); err != nil || len(sub.Entries) != 1 || sub.Entries[0].Name != "main.go" {
		t.Errorf("src tree: %+v %v", sub, err)
	}
	if _, err := h.Tree(ctx, "notes", "", "README.md"); !isKind(err, Invalid) {
		t.Errorf("tree of a file: %v", err)
	}
	if _, err := h.Tree(ctx, "notes", "", "nope"); !isKind(err, NotFound) {
		t.Errorf("tree of a missing folder: %v", err)
	}

	if b, err := h.Blob(ctx, "notes", "", "README.md"); err != nil || b.Text != "hello" || b.Binary || b.Size != 5 {
		t.Errorf("README: %+v %v", b, err)
	}
	if b, _ := h.Blob(ctx, "notes", "", "logo.bin"); !b.Binary || b.Text != "" {
		t.Errorf("binary: %+v", b)
	}
	if b, _ := h.Blob(ctx, "notes", "", "big.txt"); !b.TooLarge || b.Text != "" || b.Size != 1<<20+1 {
		t.Errorf("too large: %+v", b.TooLarge)
	}
	if _, err := h.Blob(ctx, "notes", "", "src"); !isKind(err, Invalid) {
		t.Errorf("blob of a folder: %v", err)
	}
	var raw bytes.Buffer
	if err := h.Raw(ctx, "notes", "", "logo.bin", &raw); err != nil || raw.String() != "\x00\x01" {
		t.Errorf("raw: %q %v", raw.String(), err)
	}
}

func TestCommitsPaging(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	work := t.TempDir()
	gitIn(t, work, "-c", "init.defaultBranch=main", "clone", "-q", h.path("notes"), ".")
	for i := 1; i <= 55; i++ {
		f := "a.txt"
		if i%11 == 0 {
			f = "src/main.go"
		}
		if err := os.MkdirAll(work+"/src", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(work+"/"+f, []byte(fmt.Sprint(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, work, "add", "-A")
		gitIn(t, work, "commit", "-q", "-m", fmt.Sprint("Commit ", i))
	}
	gitIn(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")

	p1, err := h.Commits(ctx, "notes", "", "", 1)
	if err != nil || len(p1) != 50 || p1[0].Subject != "Commit 55" || p1[0].Author != "Test" || p1[0].Email != "test@example.com" || len(p1[0].Parents) != 1 {
		t.Fatalf("page 1: %d %+v %v", len(p1), p1[0], err)
	}
	if p2, _ := h.Commits(ctx, "notes", "", "", 2); len(p2) != 5 || p2[4].Subject != "Commit 1" || len(p2[4].Parents) != 0 {
		t.Errorf("page 2: %+v", p2)
	}
	if only, _ := h.Commits(ctx, "notes", "main", "src/main.go", 1); len(only) != 5 {
		t.Errorf("path filter: %d", len(only))
	}
}

func TestArchive(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a.txt": "a"}, "One")
	for format, magic := range map[string]string{"tar.gz": "\x1f\x8b", "zip": "PK"} {
		var b bytes.Buffer
		if err := h.Archive(ctx, "notes", "", format, &b); err != nil || !strings.HasPrefix(b.String(), magic) {
			t.Errorf("%s: %v %q", format, err, b.String()[:min(4, b.Len())])
		}
	}
	if err := h.Archive(ctx, "notes", "", "rar", &bytes.Buffer{}); !isKind(err, Invalid) {
		t.Errorf("rar: %v", err)
	}
}

func TestBrowseRefusesOptionLikeInput(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	commitFiles(t, h, "notes", map[string]string{"a.txt": "a"}, "One")
	out := t.TempDir() + "/x"
	checks := map[string]error{
		"tree ref":    func() error { _, err := h.Tree(ctx, "notes", "--output="+out, ""); return err }(),
		"blob ..":     func() error { _, err := h.Blob(ctx, "notes", "", "../x"); return err }(),
		"blob -x":     func() error { _, err := h.Blob(ctx, "notes", "", "-x"); return err }(),
		"commit -p":   func() error { _, err := h.CommitDetail(ctx, "notes", "-p"); return err }(),
		"commits ref": func() error { _, err := h.Commits(ctx, "notes", "--all", "", 1); return err }(),
		"archive ref": h.Archive(ctx, "notes", "--output="+out, "zip", &bytes.Buffer{}),
	}
	for what, err := range checks {
		if !isKind(err, Invalid) {
			t.Errorf("%s: %v, want Invalid", what, err)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("git wrote %s", out)
	}
	if _, err := h.Tree(ctx, "notes", "nope", ""); !isKind(err, NotFound) || err.Error() != "There's no branch, tag or commit called nope in notes" {
		t.Errorf("missing ref: %v", err)
	}
}

func TestEmptyRepoBrowse(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "empty")
	if tr, err := h.Tree(ctx, "empty", "", ""); err != nil || !tr.Empty || len(tr.Entries) != 0 || tr.Entries == nil {
		t.Errorf("tree: %+v %v", tr, err)
	}
	if cs, err := h.Commits(ctx, "empty", "", "", 1); err != nil || cs == nil || len(cs) != 0 {
		t.Errorf("commits: %#v %v", cs, err)
	}
	if r, err := h.Info(ctx, "empty"); err != nil || len(r.Branches) != 0 || !r.Empty {
		t.Errorf("info: %+v %v", r, err)
	}
	err := h.Archive(ctx, "empty", "", "zip", &bytes.Buffer{})
	if !isKind(err, Exists) || err.Error() != "There's nothing to download yet" {
		t.Errorf("archive: %v", err)
	}
}
