package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/idle"
)

// quietSys is a server with nothing going on.
type quietSys struct {
	suspends int
	logins   string
}

func (q *quietSys) Run(name string, args ...string) (string, error) {
	switch name {
	case "who":
		return q.logins, nil
	case "ss", "systemctl":
		return "", nil
	}
	return "", idle.ErrNotInstalled // conntrack
}
func (q *quietSys) ReadFile(string) (string, error) { return "", os.ErrNotExist }
func (q *quietSys) Suspend() error                  { q.suspends++; return nil }
func (q *quietSys) BootTime() time.Time             { return time.Now().Add(-10 * time.Hour) }

func do(t *testing.T, sys idle.System, cfg string, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sleep.env")
	if cfg != "" {
		os.WriteFile(cfgFile, []byte(cfg), 0o644)
	}
	var out bytes.Buffer
	err := run(args, sys, &out, cfgFile, filepath.Join(dir, "state"), filepath.Join(dir, "idle.log"))
	return out.String(), dir, err
}

func TestNoArgsIsUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"sleep-hook"}} {
		if _, _, err := do(t, &quietSys{}, "", args...); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestHoldArguments(t *testing.T) {
	for _, bad := range []string{"0", "-5", "x", "10081"} {
		if _, _, err := do(t, &quietSys{}, "", "hold", bad); err == nil {
			t.Errorf("hold %s accepted", bad)
		}
	}
	out, dir, err := do(t, &quietSys{}, "", "hold", "30")
	if err != nil || !strings.Contains(out, "staying awake until") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "hold")); err != nil {
		t.Error("no hold file")
	}
}

func TestStatusChangesNothing(t *testing.T) {
	sys := &quietSys{logins: "alice pts/0 2026-11-02 12:00 (192.0.2.5)\n"}
	out, dir, err := do(t, sys, "", "status")
	if err != nil || !strings.Contains(out, "login session") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Error("status wrote state")
	}
}

func TestCheckDryRunWouldSuspend(t *testing.T) {
	sys := &quietSys{}
	out, _, err := do(t, sys, "IDLE_MIN=1\nGRACE_MIN=0\n", "check", "--dry-run")
	if err != nil || !strings.Contains(out, "would suspend now") || sys.suspends != 0 {
		t.Errorf("%q %v suspends=%d", out, err, sys.suspends)
	}
}

func TestBadConfigIsRefused(t *testing.T) {
	if _, _, err := do(t, &quietSys{}, "NOPE=1\n", "status"); err == nil || !strings.Contains(err.Error(), "sleep.env") {
		t.Errorf("got %v", err)
	}
}

func TestVersion(t *testing.T) {
	if out, _, err := do(t, &quietSys{}, "", "version"); err != nil || !strings.HasPrefix(out, "cirrusux-idle") {
		t.Errorf("%q %v", out, err)
	}
}

// realSys stops a command that hangs, and reports a missing one.
func TestRealSysRun(t *testing.T) {
	if _, err := (realSys{}).Run("cirrusux-no-such-tool"); !errors.Is(err, idle.ErrNotInstalled) {
		t.Errorf("missing tool: %v", err)
	}
	if out, err := (realSys{}).Run("echo", "hi"); err != nil || out != "hi\n" {
		t.Errorf("echo: %q %v", out, err)
	}
}
