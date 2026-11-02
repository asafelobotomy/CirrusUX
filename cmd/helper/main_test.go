package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/helperapi"
)

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	var b strings.Builder
	for i := range 10 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	os.WriteFile(path, []byte(b.String()), 0o600)
	got, err := tailFile(path, 3)
	if err != nil || strings.Join(got.Lines, ",") != "line 7,line 8,line 9" {
		t.Errorf("tail = %v, %v", got.Lines, err)
	}
	if got, _ := tailFile(path, 50); len(got.Lines) != 10 {
		t.Errorf("short file: %d lines", len(got.Lines))
	}
	if _, err := tailFile(filepath.Join(t.TempDir(), "missing"), 3); err == nil {
		t.Error("missing file should fail")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abcdef", 3) != "abc" || truncate("ab", 3) != "ab" {
		t.Error("truncate")
	}
}

// Only finished system archives are listed: not .partial files, other
// names or directories.
func TestBackupList(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"cloud64-system-20261101.tar.zst.age", "cloud64-system-20261102.tar.zst.age.partial", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("12345"), 0o600)
	}
	os.Mkdir(filepath.Join(dir, "cloud64-system-dir"), 0o700)
	l, err := backupList(dir)
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "cloud64-system-20261101.tar.zst.age" || l.Files[0].Bytes != 5 || l.Files[0].MTime == 0 {
		t.Errorf("list = %+v, %v", l.Files, err)
	}
}

func TestTimeoutFor(t *testing.T) {
	if timeoutFor(helperapi.CmdEgressReport) != 150*time.Second || timeoutFor(helperapi.CmdIdleLog) != 30*time.Second {
		t.Error("timeouts")
	}
}
