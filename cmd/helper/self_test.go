package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The job runs cirrusux-setup as the owner would, always with -y: there is
// nobody at a terminal to answer.
func TestSelfArgs(t *testing.T) {
	if got := selfArgs("upgrade", "v0.9.0"); !slices.Equal(got, []string{"upgrade", "v0.9.0", "-y"}) {
		t.Errorf("upgrade: %v", got)
	}
	if got := selfArgs("rollback", "-"); !slices.Equal(got, []string{"rollback", "-y"}) {
		t.Errorf("rollback: %v", got)
	}
}

// A copy built from source has no releases to move between: nothing starts.
func TestSelfJobNeedsARelease(t *testing.T) {
	old := setupLink
	setupLink = filepath.Join(t.TempDir(), "cirrusux-setup")
	defer func() { setupLink = old }()
	for _, what := range []string{"upgrade", "rollback"} {
		_, err := selfJob(context.Background(), what, "v0.9.0")
		if err == nil || !strings.Contains(err.Error(), "wasn't installed from a release") {
			t.Errorf("%s: err = %v", what, err)
		}
	}
}

// The helper itself refuses to go to the same or an older release, whatever
// the gateway or the agent asked for.
func TestSelfUpgradeOnlyGoesForwards(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"opt/releases/v0.9.0/deploy/host", "opt/releases/v0.8.1"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink("releases/v0.9.0", filepath.Join(dir, "opt", "current"))
	_ = os.Symlink("releases/v0.8.1", filepath.Join(dir, "opt", "previous"))
	old := setupLink
	setupLink = filepath.Join(dir, "cirrusux-setup")
	defer func() { setupLink = old }()
	_ = os.Symlink(filepath.Join(dir, "opt", "releases", "v0.9.0", "deploy", "host", "cirrusux-setup"), setupLink)
	for _, v := range []string{"v0.9.0", "v0.8.1", "v0.1.0"} {
		_, err := selfJob(context.Background(), "upgrade", v)
		if err == nil || !strings.Contains(err.Error(), "only goes to a newer release") {
			t.Errorf("%s: err = %v", v, err)
		}
	}
	for _, tc := range []struct {
		have, want string
		older      bool
	}{{"v0.8.1", "v0.9.0", true}, {"v0.9.9", "v0.10.0", true}, {"v0.9.0", "v0.9.0", false}, {"v1.0.0", "v0.9.9", false}, {"", "v0.9.0", false}} {
		if got := olderThan(tc.have, tc.want); got != tc.older {
			t.Errorf("olderThan(%q, %q) = %v", tc.have, tc.want, got)
		}
	}
}

func TestLastLines(t *testing.T) {
	got := lastLines("a\n\n  b  \nc\n", 2)
	if !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("got %v", got)
	}
	if got := lastLines("", 3); len(got) != 0 {
		t.Errorf("empty input: %v", got)
	}
}
