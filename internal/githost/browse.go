package githost

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	blobLimit = 1 << 20
	pageSize  = 50
)

type Entry struct {
	Name string `json:"name"`
	Type string `json:"type"` // file, folder, link, submodule
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

type Tree struct {
	Commit  string  `json:"commit"`
	Empty   bool    `json:"empty"`
	Entries []Entry `json:"entries"`
}

type Blob struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
	Text     string `json:"text"`
}

type Commit struct {
	SHA     string    `json:"sha"`
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Time    time.Time `json:"time"`
	Subject string    `json:"subject"`
}

// resolve turns a branch, tag or commit id ("" for the default branch)
// into a commit id. An empty repository resolves to "" with empty set.
func (h *Host) resolve(ctx context.Context, name, ref string) (sha string, empty bool, err error) {
	if err := h.need(name); err != nil {
		return "", false, err
	}
	p := h.path(name)
	if ref == "" {
		out, _ := h.run(ctx, p, 4096, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads")
		if len(bytes.TrimSpace(out)) == 0 {
			return "", true, nil
		}
		ref = "HEAD"
		if _, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
			// HEAD names a branch nobody pushed: show the newest one.
			out, err := h.run(ctx, p, 4096, "for-each-ref", "--sort=-committerdate", "--count=1", "--format=%(refname)", "refs/heads")
			if err != nil {
				return "", false, err
			}
			ref = strings.TrimSpace(string(out))
		}
	} else if validBranch(ref) != nil && validCommit(ref) != nil {
		return "", false, errf(Invalid, "%q isn't a usable branch, tag or commit", ref)
	}
	out, err := h.run(ctx, p, 4096, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", false, errf(NotFound, "There's no branch, tag or commit called %s in %s", ref, name)
	}
	return strings.TrimSpace(string(out)), false, nil
}

// object finds path at commit sha and returns its object spec and type
// (blob, tree or commit).
func (h *Host) object(ctx context.Context, name, sha, path string) (spec, typ string, err error) {
	spec = sha + ":" + path
	out, err := h.run(ctx, h.path(name), 4096, "cat-file", "-t", spec)
	if err != nil {
		return "", "", errf(NotFound, "There's no %s in %s", path, name)
	}
	return spec, strings.TrimSpace(string(out)), nil
}

// Tree lists a folder at a ref, folders first.
func (h *Host) Tree(ctx context.Context, name, ref, path string) (Tree, error) {
	path, err := cleanPath(path)
	if err != nil {
		return Tree{}, err
	}
	sha, empty, err := h.resolve(ctx, name, ref)
	if err != nil || empty {
		return Tree{Empty: empty, Entries: []Entry{}}, err
	}
	spec := sha
	if path != "" {
		s, typ, err := h.object(ctx, name, sha, path)
		if err != nil {
			return Tree{}, err
		}
		if typ != "tree" {
			return Tree{}, errf(Invalid, "%s is a file", path)
		}
		spec = s
	}
	out, err := h.run(ctx, h.path(name), 16<<20, "ls-tree", "-l", "-z", spec)
	if err != nil {
		return Tree{}, err
	}
	t := Tree{Commit: sha, Entries: []Entry{}}
	for _, rec := range strings.Split(string(out), "\x00") {
		meta, fname, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			continue
		}
		e := Entry{Name: fname, Mode: f[0]}
		e.Size, _ = strconv.ParseInt(f[3], 10, 64)
		switch {
		case f[1] == "tree":
			e.Type = "folder"
		case f[1] == "commit":
			e.Type = "submodule"
		case f[0] == "120000":
			e.Type = "link"
		default:
			e.Type = "file"
		}
		t.Entries = append(t.Entries, e)
	}
	sort.SliceStable(t.Entries, func(i, j int) bool {
		a, b := t.Entries[i].Type == "folder", t.Entries[j].Type == "folder"
		if a != b {
			return a
		}
		return t.Entries[i].Name < t.Entries[j].Name
	})
	return t, nil
}

func (h *Host) file(ctx context.Context, name, ref, path string) (spec string, err error) {
	path, err = cleanPath(path)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errf(Invalid, "Pick a file")
	}
	sha, empty, err := h.resolve(ctx, name, ref)
	if err != nil {
		return "", err
	}
	if empty {
		return "", errf(NotFound, "There's no %s in %s", path, name)
	}
	spec, typ, err := h.object(ctx, name, sha, path)
	if err != nil {
		return "", err
	}
	if typ != "blob" {
		return "", errf(Invalid, "%s is a folder", path)
	}
	return spec, nil
}

// Blob is a file's text, if it is text and up to 1 MiB.
func (h *Host) Blob(ctx context.Context, name, ref, path string) (Blob, error) {
	spec, err := h.file(ctx, name, ref, path)
	if err != nil {
		return Blob{}, err
	}
	p := h.path(name)
	out, err := h.run(ctx, p, 4096, "cat-file", "-s", spec)
	if err != nil {
		return Blob{}, err
	}
	b := Blob{Path: strings.SplitN(spec, ":", 2)[1]}
	b.Size, _ = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if b.Size > blobLimit {
		b.TooLarge = true
		return b, nil
	}
	data, err := h.run(ctx, p, blobLimit, "cat-file", "blob", spec)
	if err != nil {
		return Blob{}, err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 || !utf8.Valid(data) {
		b.Binary = true
		return b, nil
	}
	b.Text = string(data)
	return b, nil
}

// Raw writes a file's bytes, whatever their size.
func (h *Host) Raw(ctx context.Context, name, ref, path string, w io.Writer) error {
	spec, err := h.file(ctx, name, ref, path)
	if err != nil {
		return err
	}
	return h.runIn(ctx, h.path(name), w, "cat-file", "blob", spec)
}

// Commits is one page of 50 commits at a ref, newest first, optionally
// only those that touch path.
func (h *Host) Commits(ctx context.Context, name, ref, path string, page int) ([]Commit, error) {
	if page < 1 {
		return nil, errf(Invalid, "Pages start at 1")
	}
	path, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	sha, empty, err := h.resolve(ctx, name, ref)
	if err != nil || empty {
		return []Commit{}, err
	}
	args := []string{"log", "--format=" + commitFormat, "--skip=" + strconv.Itoa((page-1)*pageSize),
		"--max-count=" + strconv.Itoa(pageSize), sha}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := h.run(ctx, h.path(name), 4<<20, args...)
	if err != nil {
		return nil, err
	}
	list := []Commit{}
	for _, f := range records(out, 6) {
		list = append(list, commitFrom(f))
	}
	return list, nil
}

const commitFormat = "%H%x00%P%x00%an%x00%ae%x00%ct%x00%s"

func commitFrom(f []string) Commit {
	t, _ := unixTime(f[4])
	c := Commit{SHA: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3], Time: t, Subject: f[5]}
	if c.Parents == nil {
		c.Parents = []string{}
	}
	return c
}

// Archive writes the files at a ref as tar.gz or zip.
func (h *Host) Archive(ctx context.Context, name, ref, format string, w io.Writer) error {
	if format != "tar.gz" && format != "zip" {
		return errf(Invalid, "Downloads are .tar.gz or .zip")
	}
	sha, empty, err := h.resolve(ctx, name, ref)
	if err != nil {
		return err
	}
	if empty {
		return errf(Exists, "There's nothing to download yet")
	}
	return h.runIn(ctx, h.path(name), w, "archive", "--format="+format, sha)
}
