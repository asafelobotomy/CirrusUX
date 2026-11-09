// Package storage measures folders and keeps the archive (docs/history/PHASES-4-6 §6c).
// Measuring reads every file under a folder, which keeps a disk busy for a
// while, so it runs only when the owner asks, as a job, and the result is
// kept. The archive is where CirrusUX puts what it would otherwise delete:
// removed apps' folders, backups taken before updates, and the system
// backups. Delete here is the only deletion in CirrusUX, and it takes the
// exact name of what goes.
package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cirrusux/internal/fsutil"
)

// Result is one measurement.
type Result struct {
	Measured time.Time `json:"measured"`
	Roots    []Root    `json:"roots"`
	Archive  []Entry   `json:"archive"`
}

// Root is one measured folder: its size, its biggest sub-folders, and the
// space on the disk it's on.
type Root struct {
	Path     string  `json:"path"`
	Label    string  `json:"label"`
	Bytes    int64   `json:"bytes"`
	Children []Child `json:"children"`
	DiskSize uint64  `json:"diskSize"`
	DiskFree uint64  `json:"diskFree"`
}

type Child struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// Entry is one thing in the archive.
type Entry struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // removed-app, pre-update, backup
	App      string    `json:"app,omitempty"`
	Tag      string    `json:"tag,omitempty"` // pre-update: the version backed up
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// Places says where things are on this server.
type Places struct {
	Roots        []RootSpec
	ArchiveDir   string // removed apps and pre-update backups
	BackupDir    string // system backups
	BackupPrefix string
}

type RootSpec struct{ Path, Label string }

// Du runs `du`; tests replace it.
type Du func(ctx context.Context, args ...string) (string, error)

const childrenKept = 12

var (
	preUpdateRe = regexp.MustCompile(`^([a-z][a-z0-9-]*)-before-([A-Za-z0-9._-]+)-(\d{8}-\d{6})$`)
	removedRe   = regexp.MustCompile(`^([a-z][a-z0-9-]*)-(\d{8}-\d{6})$`)
)

// Scan measures every root and the archive.
func Scan(ctx context.Context, p Places, du Du, now time.Time) (Result, error) {
	res := Result{Measured: now, Roots: []Root{}, Archive: []Entry{}}
	for _, spec := range p.Roots {
		if _, err := os.Stat(spec.Path); err != nil {
			continue
		}
		out, err := du(ctx, "-x", "-B1", "-d1", spec.Path)
		if err != nil && out == "" {
			return res, fmt.Errorf("measuring %s: %w", spec.Path, err)
		}
		r := parseDu(out, spec)
		var st syscall.Statfs_t
		if syscall.Statfs(spec.Path, &st) == nil {
			r.DiskSize, r.DiskFree = st.Blocks*uint64(st.Bsize), st.Bavail*uint64(st.Bsize)
		}
		res.Roots = append(res.Roots, r)
	}

	if p.ArchiveDir != "" {
		entries, _ := os.ReadDir(p.ArchiveDir)
		for _, e := range entries {
			en, ok := classify(e.Name())
			if !ok || !e.IsDir() {
				continue
			}
			full := filepath.Join(p.ArchiveDir, e.Name())
			out, _ := du(ctx, "-x", "-B1", "-s", full)
			if f := strings.Fields(out); len(f) > 0 {
				en.Bytes, _ = strconv.ParseInt(f[0], 10, 64)
			}
			if fi, err := e.Info(); err == nil {
				en.Modified = fi.ModTime()
			}
			res.Archive = append(res.Archive, en)
		}
	}
	if p.BackupDir != "" && p.BackupPrefix != "" {
		entries, _ := os.ReadDir(p.BackupDir)
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), p.BackupPrefix) || strings.HasSuffix(e.Name(), ".partial") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			res.Archive = append(res.Archive, Entry{Name: e.Name(), Kind: "backup", Bytes: fi.Size(), Modified: fi.ModTime()})
		}
	}
	sort.Slice(res.Archive, func(i, j int) bool { return res.Archive[i].Modified.After(res.Archive[j].Modified) })
	return res, nil
}

// parseDu reads `du -B1 -d1 ROOT`: one "bytes<TAB>path" line per folder, the
// root itself last.
func parseDu(out string, spec RootSpec) Root {
	r := Root{Path: spec.Path, Label: spec.Label, Children: []Child{}}
	root := filepath.Clean(spec.Path)
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		size, path, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
		if err != nil {
			continue
		}
		if filepath.Clean(path) == root {
			r.Bytes = n
		} else if filepath.Dir(filepath.Clean(path)) == root {
			r.Children = append(r.Children, Child{Name: filepath.Base(path), Bytes: n})
		}
	}
	sort.Slice(r.Children, func(i, j int) bool { return r.Children[i].Bytes > r.Children[j].Bytes })
	if len(r.Children) > childrenKept {
		var rest int64
		for _, c := range r.Children[childrenKept:] {
			rest += c.Bytes
		}
		r.Children = append(r.Children[:childrenKept], Child{Name: fmt.Sprintf("%d more", len(r.Children)-childrenKept), Bytes: rest})
	}
	return r
}

func classify(name string) (Entry, bool) {
	if m := preUpdateRe.FindStringSubmatch(name); m != nil {
		return Entry{Name: name, Kind: "pre-update", App: m[1], Tag: m[2]}, true
	}
	if m := removedRe.FindStringSubmatch(name); m != nil {
		return Entry{Name: name, Kind: "removed-app", App: m[1]}, true
	}
	return Entry{}, false
}

// ErrNotInArchive: the name isn't something the archive holds.
var ErrNotInArchive = errors.New("not in the archive")

// Delete removes one archive entry by its exact name: a removed app's
// folder or a pre-update backup in ArchiveDir, or a system backup file in
// BackupDir. Nothing outside those two folders can be named, and a link
// is removed as a link, never followed.
func Delete(p Places, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", ErrNotInArchive
	}
	var path string
	wantDir := false // Scan lists archive entries as folders and backups as files
	switch {
	case p.ArchiveDir != "" && (preUpdateRe.MatchString(name) || removedRe.MatchString(name)):
		path, wantDir = filepath.Join(p.ArchiveDir, name), true
	case p.BackupDir != "" && p.BackupPrefix != "" && strings.HasPrefix(name, p.BackupPrefix) && !strings.HasSuffix(name, ".partial"):
		path = filepath.Join(p.BackupDir, name)
	default:
		return "", ErrNotInArchive
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return "", ErrNotInArchive
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return path, os.Remove(path)
	case wantDir != fi.IsDir():
		return "", ErrNotInArchive // not what Scan listed
	}
	return path, os.RemoveAll(path)
}

// Read is the last result, or nil.
func Read(path string) *Result {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

// Write keeps a result, readable by the agent.
func Write(path string, r Result) error {
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	return fsutil.WriteJSON(path, r, 0o644)
}

// Forget drops a deleted entry from a kept result, so the page is right
// before the next measurement. It reads and writes under the same lock as
// Write, so a scan finishing at the same moment can't bring the entry back.
func Forget(path, name string) {
	unlock, err := lock(path)
	if err != nil {
		return
	}
	defer unlock()
	r := Read(path)
	if r == nil {
		return
	}
	kept := r.Archive[:0]
	for _, e := range r.Archive {
		if e.Name != name {
			kept = append(kept, e)
		}
	}
	r.Archive = kept
	_ = fsutil.WriteJSON(path, *r, 0o644)
}

// lock takes an exclusive lock on <path>.lock until unlock is called; the
// kernel drops it if the process dies.
func lock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
