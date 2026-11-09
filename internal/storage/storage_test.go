package storage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func realDu(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "du", args...).Output()
	return string(out), err
}

func TestParseDu(t *testing.T) {
	out := "4096\t/srv/media/empty\n900\t/srv/media/Music\n5000\t/srv/media/Films\n9996\t/srv/media\n"
	r := parseDu(out, RootSpec{Path: "/srv/media/", Label: "Media"})
	if r.Bytes != 9996 || len(r.Children) != 3 || r.Children[0] != (Child{"Films", 5000}) || r.Label != "Media" {
		t.Errorf("root = %+v", r)
	}
	var many strings.Builder
	for i := range 20 {
		fmt.Fprintf(&many, "%d\t/r/d%02d\n", 100+i, i)
	}
	many.WriteString("5000\t/r\n")
	r = parseDu(many.String(), RootSpec{Path: "/r"})
	if len(r.Children) != childrenKept+1 || r.Children[childrenKept].Name != "8 more" {
		t.Errorf("children = %+v", r.Children)
	}
}

func places(t *testing.T) Places {
	root := t.TempDir()
	p := Places{
		Roots:        []RootSpec{{Path: filepath.Join(root, "media"), Label: "Media"}},
		ArchiveDir:   filepath.Join(root, "archive"),
		BackupDir:    filepath.Join(root, "backups"),
		BackupPrefix: "atlas-system-",
	}
	for _, d := range []string{"media/Films", "media/Music", "archive/navidrome-20261101-120000/data", "archive/jellyfin-before-12.3-20261102-090000/config", "archive/stray-folder", "backups"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "media/Films/film.mkv"), make([]byte, 64<<10), 0o644)
	os.WriteFile(filepath.Join(root, "archive/navidrome-20261101-120000/data/navidrome.db"), make([]byte, 8<<10), 0o644)
	os.WriteFile(filepath.Join(root, "backups/atlas-system-2026-11-01_033053.tar.zst.age"), make([]byte, 2000), 0o600)
	os.WriteFile(filepath.Join(root, "backups/atlas-system-2026-11-02_033053.tar.zst.age.partial"), nil, 0o600)
	os.WriteFile(filepath.Join(root, "backups/notes.txt"), nil, 0o600)
	return p
}

func TestScan(t *testing.T) {
	p := places(t)
	res, err := Scan(context.Background(), p, realDu, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Roots) != 1 || res.Roots[0].Children[0].Name != "Films" || res.Roots[0].Bytes < 64<<10 || res.Roots[0].DiskSize == 0 {
		t.Errorf("roots = %+v", res.Roots)
	}
	kinds := map[string]Entry{}
	for _, e := range res.Archive {
		kinds[e.Kind] = e
	}
	if len(res.Archive) != 3 || kinds["removed-app"].App != "navidrome" || kinds["pre-update"].Tag != "12.3" || kinds["backup"].Bytes != 2000 {
		t.Errorf("archive = %+v", res.Archive)
	}
	if kinds["removed-app"].Bytes < 8<<10 {
		t.Errorf("removed app's size = %d", kinds["removed-app"].Bytes)
	}
}

// Delete takes only exact names of what the archive holds.
func TestDelete(t *testing.T) {
	p := places(t)
	outside := filepath.Join(filepath.Dir(p.ArchiveDir), "media")
	os.Symlink(outside, filepath.Join(p.ArchiveDir, "media-20261101-000000"))
	for _, bad := range []string{"", ".", "..", "../media", "stray-folder", "navidrome-20261101-120000/data", "atlas-system-2026-11-02_033053.tar.zst.age.partial", "notes.txt", "jellyfin-20991231-000000"} {
		if _, err := Delete(p, bad); err == nil {
			t.Errorf("deleted %q", bad)
		}
	}
	if _, err := Delete(p, "media-20261101-000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "Films", "film.mkv")); err != nil {
		t.Error("deleting a link removed what it points at")
	}
	for _, name := range []string{"navidrome-20261101-120000", "jellyfin-before-12.3-20261102-090000", "atlas-system-2026-11-01_033053.tar.zst.age"} {
		path, err := Delete(p, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Lstat(path); err == nil {
			t.Errorf("%s still there", path)
		}
	}
	if _, err := os.Stat(filepath.Join(p.ArchiveDir, "stray-folder")); err != nil {
		t.Error("something not named was removed")
	}
}

func TestKeepAndForget(t *testing.T) {
	f := filepath.Join(t.TempDir(), "storage.json")
	Write(f, Result{Archive: []Entry{{Name: "a-20261101-000000"}, {Name: "b-20261101-000000"}}})
	Forget(f, "a-20261101-000000")
	if r := Read(f); r == nil || len(r.Archive) != 1 || r.Archive[0].Name != "b-20261101-000000" {
		t.Errorf("after forget: %+v", r)
	}
}

// Scan lists archive entries as folders and backups as files; Delete
// refuses a file where a folder belongs, and the other way round.
func TestDeleteOnlyWhatScanLists(t *testing.T) {
	p := places(t)
	os.WriteFile(filepath.Join(p.ArchiveDir, "ghost-20261101-000000"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(p.BackupDir, "atlas-system-2026-11-03_033053.tar.zst.age"), 0o755)
	for _, name := range []string{"ghost-20261101-000000", "atlas-system-2026-11-03_033053.tar.zst.age"} {
		if _, err := Delete(p, name); err == nil {
			t.Errorf("deleted %s", name)
		}
	}
}

// Forgetting while a scan writes must not lose either update, or bring
// back a deleted entry.
func TestForgetAndWriteDoNotRace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "storage.json")
	res := Result{Archive: []Entry{{Name: "a-20261101-000000"}, {Name: "b-20261101-000000"}}}
	Write(f, res)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); Forget(f, "a-20261101-000000") }()
		go func() { defer wg.Done(); Forget(f, "zz-20261101-000000") }()
	}
	wg.Wait()
	if r := Read(f); r == nil || len(r.Archive) != 1 || r.Archive[0].Name != "b-20261101-000000" {
		t.Errorf("after forgetting: %+v", r)
	}
}
