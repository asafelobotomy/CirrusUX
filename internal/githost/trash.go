package githost

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Deleted repositories wait in .trash until restored or deleted for good.
// A trash id is <name>-<YYYYMMDD-HHMMSS> in UTC, with -2, -3... if two
// deletes of one name land in the same second.

// Trashed is one deleted repository.
type Trashed struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	DeletedAt time.Time `json:"deletedAt"`
	Size      int64     `json:"size"`
}

const stampLayout = "20060102-150405"

var trashRe = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]{0,63})-(\d{8}-\d{6})(-\d+)?$`)

func (h *Host) trashPath(id string) string { return filepath.Join(h.Dir, ".trash", id+".git") }

// parseTrash checks a trash id and returns the repository's name and when
// it was deleted.
func parseTrash(id string) (string, time.Time, error) {
	m := trashRe.FindStringSubmatch(id)
	if m == nil || ValidName(m[1]) != nil {
		return "", time.Time{}, errf(Invalid, "%q isn't a deleted repository", id)
	}
	t, err := time.Parse(stampLayout, m[2])
	if err != nil {
		return "", time.Time{}, errf(Invalid, "%q isn't a deleted repository", id)
	}
	return m[1], t, nil
}

// Delete moves a repository to the trash and returns its trash id.
func (h *Host) Delete(ctx context.Context, name string) (string, error) {
	if err := h.need(name); err != nil {
		return "", err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return "", err
	}
	defer unlock()
	base := name + "-" + h.now().UTC().Format(stampLayout)
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(h.trashPath(id)); os.IsNotExist(err) {
			break
		}
		id = base + "-" + strconv.Itoa(i)
	}
	return id, os.Rename(h.path(name), h.trashPath(id))
}

func (h *Host) needTrash(id string) (string, error) {
	name, _, err := parseTrash(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(h.trashPath(id)); err != nil {
		return "", errf(NotFound, "There's no deleted repository %s", id)
	}
	return name, nil
}

// Restore moves a deleted repository back and returns its name.
func (h *Host) Restore(ctx context.Context, id string) (string, error) {
	name, err := h.needTrash(id)
	if err != nil {
		return "", err
	}
	unlockTrash, err := h.lock("trash:" + id)
	if err != nil {
		return "", err
	}
	defer unlockTrash()
	unlock, err := h.lock(name)
	if err != nil {
		return "", err
	}
	defer unlock()
	if h.Exists(name) {
		return "", errf(Exists, "A repository called %s already exists. Rename it first", name)
	}
	return name, os.Rename(h.trashPath(id), h.path(name))
}

// Purge deletes a repository from the trash for good. confirm must be the
// repository's name, typed by the person.
func (h *Host) Purge(ctx context.Context, id, confirm string) error {
	name, err := h.needTrash(id)
	if err != nil {
		return err
	}
	if confirm != name {
		return errf(Invalid, "Type the repository's name to delete it for good")
	}
	unlock, err := h.lock("trash:" + id)
	if err != nil {
		return err
	}
	defer unlock()
	return os.RemoveAll(h.trashPath(id))
}

// ListTrash is every deleted repository, newest first.
func (h *Host) ListTrash() ([]Trashed, error) {
	entries, err := os.ReadDir(filepath.Join(h.Dir, ".trash"))
	if err != nil {
		return nil, err
	}
	list := []Trashed{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".git")
		if !ok || !e.IsDir() {
			continue
		}
		name, at, err := parseTrash(id)
		if err != nil {
			continue
		}
		list = append(list, Trashed{ID: id, Name: name, DeletedAt: at, Size: dirSize(h.trashPath(id))})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].DeletedAt.Equal(list[j].DeletedAt) {
			return list[i].DeletedAt.After(list[j].DeletedAt)
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}
