package githost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/fsutil"
)

// Summary is one repository in the list.
type Summary struct {
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	DefaultBranch string     `json:"defaultBranch"`
	Size          int64      `json:"size"`
	LastCommit    *time.Time `json:"lastCommit"`
	Empty         bool       `json:"empty"`
}

type Branch struct {
	Name    string    `json:"name"`
	Commit  string    `json:"commit"`
	Subject string    `json:"subject"`
	Time    time.Time `json:"time"`
}

type Tag struct {
	Name   string    `json:"name"`
	Commit string    `json:"commit"`
	Time   time.Time `json:"time"`
}

// Repo is one repository with its branches and tags.
type Repo struct {
	Summary
	Branches []Branch `json:"branches"`
	Tags     []Tag    `json:"tags"`
}

const refsLimit = 4 << 20

// settings go into every new or imported repository. The 30 days keep
// the commits of a deleted branch or tag, so its undo works that long.
var settings = [][2]string{
	{"core.logAllRefUpdates", "true"},
	{"gc.pruneExpire", "30.days.ago"},
	{"gc.reflogExpireUnreachable", "30.days.ago"},
	{"http.receivepack", "true"},
	// Refs that keep a deleted branch's or tag's commits (refs.go) are
	// never shown to clients.
	{"transfer.hideRefs", "refs/cirrus"},
}

// Exists says whether a repository of that name is there.
func (h *Host) Exists(name string) bool {
	if ValidName(name) != nil {
		return false
	}
	st, err := os.Stat(h.path(name))
	return err == nil && st.IsDir()
}

func (h *Host) need(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if !h.Exists(name) {
		return errf(NotFound, "There's no repository called %s", name)
	}
	return nil
}

func checkDescription(d string) error {
	if len([]rune(d)) > 200 || strings.ContainsAny(d, "\r\n") {
		return errf(Invalid, "A description is one line of up to 200 characters")
	}
	return nil
}

// Create makes an empty repository with main as its default branch.
func (h *Host) Create(ctx context.Context, name, description string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if err := checkDescription(description); err != nil {
		return err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	if h.Exists(name) {
		return errf(Exists, "A repository called %s already exists", name)
	}
	p := h.path(name)
	if _, err := h.run(ctx, h.Dir, 64<<10, "init", "--bare", "-q", "-b", "main", "--", p); err != nil {
		_ = os.RemoveAll(p)
		return err
	}
	if err := h.configure(ctx, p, description); err != nil {
		_ = os.RemoveAll(p)
		return err
	}
	return nil
}

func (h *Host) configure(ctx context.Context, p, description string) error {
	for _, kv := range settings {
		if _, err := h.run(ctx, p, 4096, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return fsutil.WriteFile(filepath.Join(p, "description"), []byte(description), 0o600)
}

// Rename gives a repository a new name.
func (h *Host) Rename(ctx context.Context, from, to string) error {
	if err := ValidName(to); err != nil {
		return err
	}
	if err := h.need(from); err != nil {
		return err
	}
	if from == to {
		return errf(Invalid, "That's already its name")
	}
	unlock, err := h.lock(from)
	if err != nil {
		return err
	}
	defer unlock()
	unlockTo, err := h.lock(to)
	if err != nil {
		return err
	}
	defer unlockTo()
	if h.Exists(to) {
		return errf(Exists, "A repository called %s already exists", to)
	}
	return os.Rename(h.path(from), h.path(to))
}
