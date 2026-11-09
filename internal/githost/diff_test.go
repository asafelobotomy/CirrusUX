package githost

import (
	"fmt"
	"strings"
	"testing"
)

func TestCommitDetail(t *testing.T) {
	h := newHost(t)
	mustCreate(t, h, "notes")
	first := commitFiles(t, h, "notes", map[string]string{
		"a.txt": "one\ntwo\n",
		"c.txt": "gone\n",
		"d.txt": "a file that moves somewhere else\nwith enough lines\nto be found as a rename\n",
	}, "First")
	sha := commitFiles(t, h, "notes", map[string]string{
		"a.txt": "one\n2\nthree\n",
		"b.txt": "new\n",
		"c.txt": deleteFile,
		"d.txt": deleteFile,
		"e.txt": "a file that moves somewhere else\nwith enough lines\nto be found as a rename\n",
	}, "Second\n\nLonger message.")

	d, err := h.CommitDetail(ctx, "notes", sha)
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA != sha || d.Subject != "Second" || !strings.Contains(d.Message, "Longer message.") || len(d.Parents) != 1 || d.Truncated {
		t.Errorf("commit: %+v", d.Commit)
	}
	byPath := map[string]FileDiff{}
	for _, f := range d.Files {
		byPath[f.Path] = f
	}
	if f := byPath["a.txt"]; f.Change != "modified" || f.Added != 2 || f.Removed != 1 || !strings.Contains(f.Patch, "@@") {
		t.Errorf("a.txt: %+v", f)
	}
	if f := byPath["b.txt"]; f.Change != "added" || f.Added != 1 {
		t.Errorf("b.txt: %+v", f)
	}
	if f := byPath["c.txt"]; f.Change != "deleted" || f.Removed != 1 {
		t.Errorf("c.txt: %+v", f)
	}
	if f := byPath["e.txt"]; f.Change != "renamed" || f.OldPath != "d.txt" {
		t.Errorf("e.txt: %+v", f)
	}
	if len(d.Files) != 4 {
		t.Errorf("files: %+v", d.Files)
	}

	root, err := h.CommitDetail(ctx, "notes", first[:12])
	if err != nil || len(root.Files) != 3 || root.Files[0].Change != "added" || len(root.Parents) != 0 {
		t.Errorf("root commit: %+v %v", root, err)
	}
	if _, err := h.CommitDetail(ctx, "notes", strings.Repeat("0", 40)); !isKind(err, NotFound) {
		t.Errorf("missing commit: %v", err)
	}
}

func filePatch(i, lines int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/f%d b/f%d\nindex 1..2 100644\n--- a/f%d\n+++ b/f%d\n@@ -1 +1,%d @@\n-x\n", i, i, i, i, lines)
	for j := 0; j < lines; j++ {
		b.WriteString("+" + strings.Repeat("y", 1000) + "\n")
	}
	return b.String()
}

func TestParsePatchTruncates(t *testing.T) {
	var many strings.Builder
	for i := 0; i < 301; i++ {
		many.WriteString(filePatch(i, 1))
	}
	files, truncated := parsePatch([]byte(many.String()), 300, 512<<10)
	if len(files) != 300 || !truncated {
		t.Errorf("301 files: %d %v", len(files), truncated)
	}
	if files[0].Path != "f0" || files[0].Added != 1 || files[0].Removed != 1 || files[0].Change != "modified" {
		t.Errorf("first: %+v", files[0])
	}
	big := filePatch(0, 600)
	files, truncated = parsePatch([]byte(big), 300, 512<<10)
	if !truncated || len(files) != 1 || len(files[0].Patch) > 512<<10 || files[0].Added != 600 {
		t.Errorf("600 KiB: %d files, %v, patch %d", len(files), truncated, len(files[0].Patch))
	}
	bin := "diff --git a/p.png b/p.png\nnew file mode 100644\nindex 0..1\nBinary files /dev/null and b/p.png differ\n"
	if files, _ := parsePatch([]byte(bin), 300, 512<<10); len(files) != 1 || !files[0].Binary || files[0].Change != "added" {
		t.Errorf("binary: %+v", files)
	}
}
