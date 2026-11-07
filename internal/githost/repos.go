package githost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
	return os.WriteFile(filepath.Join(p, "description"), []byte(description), 0o600)
}

var credRe = regexp.MustCompile(`(https?://)[^/@\s]+@`)

// redactURL removes user:password@ from URLs in a message.
func redactURL(s string) string { return credRe.ReplaceAllString(s, "$1") }

// Import clones a repository from an https:// address. It is the only
// thing here that fetches from outside, and only when a person asks.
func (h *Host) Import(ctx context.Context, name, rawURL string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" || u.Path == "" || u.Path == "/" {
		return errf(Invalid, "Only https:// addresses can be imported")
	}
	unlock, err := h.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	if h.Exists(name) {
		return errf(Exists, "A repository called %s already exists", name)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	tmp := filepath.Join(h.Dir, ".tmp-"+name+"-"+hex.EncodeToString(b))
	defer os.RemoveAll(tmp)
	err = h.exec(ctx, h.Dir, nil, []string{"GIT_ALLOW_PROTOCOL=https"}, "clone", "--bare", "-q", "--", rawURL, tmp)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Kind == Failed {
			return errf(Failed, "The import failed: %s", lastLine(redactURL(e.stderr)))
		}
		return err
	}
	// The address may hold a password; don't keep it.
	if _, err := h.run(ctx, tmp, 4096, "remote", "remove", "origin"); err != nil {
		return err
	}
	if err := h.configure(ctx, tmp, ""); err != nil {
		return err
	}
	return os.Rename(tmp, h.path(name))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return "git gave no reason"
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

// List is every repository, by name.
func (h *Host) List(ctx context.Context) ([]Summary, error) {
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		return nil, err
	}
	list := []Summary{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".git")
		if !ok || !e.IsDir() || ValidName(name) != nil {
			continue
		}
		s, err := h.summary(ctx, name)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (h *Host) summary(ctx context.Context, name string) (Summary, error) {
	p := h.path(name)
	s := Summary{Name: name, Size: dirSize(p)}
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
	return s.Description, os.WriteFile(filepath.Join(h.path(name), "description"), []byte(desc), 0o600)
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
