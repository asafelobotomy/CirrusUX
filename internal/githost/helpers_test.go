package githost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

// deleteFile as a value in commitOn's files removes that file.
const deleteFile = "\x00delete"

// gitIn runs git in dir as a person's own git would, for test setup.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitOn commits files to branch in repo (making the branch from the
// default one if it's new) and pushes it. It returns the commit id.
func commitOn(t *testing.T, h *Host, repo, branch string, files map[string]string, msg string) string {
	t.Helper()
	work := t.TempDir()
	gitIn(t, work, "-c", "init.defaultBranch=main", "clone", "-q", h.path(repo), ".")
	if gitIn(t, work, "ls-remote", "--heads", "origin", branch) != "" {
		gitIn(t, work, "checkout", "-q", branch)
	} else {
		gitIn(t, work, "checkout", "-q", "-b", branch)
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		if body == deleteFile {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "--allow-empty", "-m", msg)
	gitIn(t, work, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return gitIn(t, work, "rev-parse", "HEAD")
}

func commitFiles(t *testing.T, h *Host, repo string, files map[string]string, msg string) string {
	t.Helper()
	return commitOn(t, h, repo, "main", files, msg)
}

// fixedClock makes trash names predictable.
func fixedClock(h *Host) {
	h.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
}

func mustCreate(t *testing.T, h *Host, name string) {
	t.Helper()
	if err := h.Create(ctx, name, ""); err != nil {
		t.Fatal(err)
	}
}
