package appjob

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPruneStatuses(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	mk := func(name string, mtime time.Time) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("{}"), 0o644)
		os.Chtimes(p, mtime, mtime)
		return p
	}
	oldJob, newJob := mk("aaaaaaaa.json", old), mk("bbbbbbbb.json", now.Add(-time.Hour))
	oldLock, heldLock := mk("jellyfin.lock", old), mk("navidrome.lock", old)
	other := mk("notes.txt", old)

	f, _ := os.Open(heldLock)
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	PruneStatuses(dir, 7*24*time.Hour, now)

	gone := func(p string) bool { _, err := os.Stat(p); return os.IsNotExist(err) }
	if !gone(oldJob) || !gone(oldLock) {
		t.Error("old status or lock files were kept")
	}
	if gone(newJob) || gone(heldLock) || gone(other) {
		t.Error("a recent, held or unrelated file was removed")
	}
}
