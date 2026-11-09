package githost

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/fsutil"
)

// List is every repository, by name.
func (h *Host) List(ctx context.Context) ([]Summary, error) {
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".git")
		if ok && e.IsDir() && ValidName(name) == nil {
			names = append(names, name)
		}
	}
	list := make([]Summary, len(names))
	errs := make([]error, len(names))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			list[i], errs[i] = h.summary(ctx, name)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (h *Host) summary(ctx context.Context, name string) (Summary, error) {
	p := h.path(name)
	s := Summary{Name: name, Size: h.size(p)}
	if b, err := os.ReadFile(filepath.Join(p, "description")); err == nil {
		d := strings.TrimSpace(string(b))
		if !strings.HasPrefix(d, "Unnamed repository;") {
			s.Description = d
		}
	}
	out, err := h.run(ctx, p, 4096, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return s, err
	}
	s.DefaultBranch = strings.TrimSpace(string(out))
	out, err = h.run(ctx, p, 4096, "for-each-ref", "--sort=-committerdate", "--count=1", "--format=%(committerdate:unix)", "refs/heads")
	if err != nil {
		return s, err
	}
	if t, ok := unixTime(strings.TrimSpace(string(out))); ok {
		s.LastCommit = &t
	} else {
		s.Empty = true
	}
	return s, nil
}

func unixTime(s string) (time.Time, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0).UTC(), true
}

// sizeTTL is how long a folder's size is kept. A push shows up in the
// list's size after that long at most.
const sizeTTL = 60 * time.Second

type sizeEntry struct {
	n  int64
	at time.Time
}

// size is dirSize, remembered for sizeTTL so a listing doesn't walk
// every repository each time.
func (h *Host) size(p string) int64 {
	h.mu.Lock()
	e, ok := h.sizes[p]
	h.mu.Unlock()
	if ok && h.now().Sub(e.at) < sizeTTL {
		return e.n
	}
	n := dirSize(p)
	h.mu.Lock()
	if h.sizes == nil || len(h.sizes) > 1000 {
		h.sizes = map[string]sizeEntry{}
	}
	h.sizes[p] = sizeEntry{n, h.now()}
	h.mu.Unlock()
	return n
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if i, err := d.Info(); err == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}

// Info is a repository with its branches and tags, newest first.
func (h *Host) Info(ctx context.Context, name string) (Repo, error) {
	if err := h.need(name); err != nil {
		return Repo{}, err
	}
	s, err := h.summary(ctx, name)
	if err != nil {
		return Repo{}, err
	}
	r := Repo{Summary: s, Branches: []Branch{}, Tags: []Tag{}}
	out, err := h.run(ctx, h.path(name), refsLimit, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)%00%(objectname)%00%(committerdate:unix)%00%(subject)", "refs/heads")
	if err != nil {
		return r, err
	}
	for _, f := range records(out, 4) {
		t, _ := unixTime(f[2])
		r.Branches = append(r.Branches, Branch{Name: f[0], Commit: f[1], Time: t, Subject: f[3]})
	}
	out, err = h.run(ctx, h.path(name), refsLimit, "for-each-ref", "--sort=-creatordate",
		"--format=%(refname:short)%00%(objectname)%00%(*objectname)%00%(creatordate:unix)", "refs/tags")
	if err != nil {
		return r, err
	}
	for _, f := range records(out, 4) {
		t, _ := unixTime(f[3])
		commit := f[1]
		if f[2] != "" { // an annotated tag: the commit it points at
			commit = f[2]
		}
		r.Tags = append(r.Tags, Tag{Name: f[0], Commit: commit, Time: t})
	}
	return r, nil
}

// records splits git's output into lines of n NUL-separated fields.
func records(out []byte, n int) [][]string {
	var rs [][]string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.SplitN(line, "\x00", n); len(f) == n {
			rs = append(rs, f)
		}
	}
	return rs
}

// SetDescription changes the description and returns the old one.
func (h *Host) SetDescription(ctx context.Context, name, desc string) (string, error) {
	if err := checkDescription(desc); err != nil {
		return "", err
	}
	if err := h.need(name); err != nil {
		return "", err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return "", err
	}
	defer unlock()
	s, err := h.summary(ctx, name)
	if err != nil {
		return "", err
	}
	return s.Description, fsutil.WriteFile(filepath.Join(h.path(name), "description"), []byte(desc), 0o600)
}

// SetDefaultBranch points HEAD at another branch and returns the old one.
func (h *Host) SetDefaultBranch(ctx context.Context, name, branch string) (string, error) {
	if err := validBranch(branch); err != nil {
		return "", err
	}
	if err := h.need(name); err != nil {
		return "", err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return "", err
	}
	defer unlock()
	p := h.path(name)
	if _, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "--end-of-options", "refs/heads/"+branch); err != nil {
		return "", errf(NotFound, "There's no branch called %s in %s", branch, name)
	}
	out, err := h.run(ctx, p, 4096, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := h.run(ctx, p, 4096, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
