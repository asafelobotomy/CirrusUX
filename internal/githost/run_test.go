package githost

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// newHost is a Host in a temporary folder. Tests that need git skip
// without it.
func newHost(t *testing.T) *Host {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	d := t.TempDir()
	h, err := New(filepath.Join(d, "repos"), filepath.Join(d, "home"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestRunTimeoutAndLimit(t *testing.T) {
	h := newHost(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := h.run(ctx, "", 1<<20, "version"); !isKind(err, Timeout) {
		t.Errorf("expired context: %v, want Timeout", err)
	}
	_, err := h.run(context.Background(), "", 10, "version")
	if !isKind(err, Failed) || err.Error() != "git's answer was too large" {
		t.Errorf("limit: %v", err)
	}
	out, err := h.run(context.Background(), "", 1<<20, "version")
	if err != nil || len(out) == 0 {
		t.Errorf("git version: %q %v", out, err)
	}
}

func isKind(err error, k Kind) bool {
	got, ok := kindOf(err)
	return ok && got == k
}

func TestLockRefusesSecondChange(t *testing.T) {
	h := &Host{}
	unlock, err := h.lock("a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.lock("a")
	if !isKind(err, Busy) || err.Error() != "Another change to a is still running" {
		t.Errorf("second lock: %v", err)
	}
	unlockB, err := h.lock("b")
	if err != nil {
		t.Errorf("other repo: %v", err)
	}
	unlockB()
	unlock()
	if u, err := h.lock("a"); err != nil {
		t.Errorf("after unlock: %v", err)
	} else {
		u()
	}
}
