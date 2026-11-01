package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"cirrusux/internal/snapshot"
)

// Store keeps the gateway's state as small JSON files in DataDir, written
// atomically with mode 600. Enough for one household; no database needed.
type Store struct {
	dir string
	mu  sync.Mutex

	snap *snapshot.Snapshot
	ui   UIDoc
}

// UIDoc is the browser's settings and boards (PLAN §4.5), versioned so two
// devices editing at once don't overwrite each other silently.
type UIDoc struct {
	Version int             `json:"version"`
	SavedAt time.Time       `json:"savedAt"`
	Data    json.RawMessage `json:"data"`
}

type AuditEntry struct {
	At     time.Time `json:"at"`
	Who    string    `json:"who"`
	What   string    `json:"what"`
	Undo   string    `json:"undo,omitempty"`
	Result string    `json:"result,omitempty"`
}

const uiHistory = 20

var ErrConflict = errors.New("version conflict")

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "ui-history"), 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	var snap snapshot.Snapshot
	if err := readFileJSON(filepath.Join(dir, "snapshot.json"), &snap); err == nil {
		s.snap = &snap
	}
	_ = readFileJSON(filepath.Join(dir, "ui.json"), &s.ui)
	return s, nil
}

func (s *Store) Snapshot() *snapshot.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *Store) SaveSnapshot(snap snapshot.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileJSON(filepath.Join(s.dir, "snapshot.json"), snap); err != nil {
		return err
	}
	s.snap = &snap
	return nil
}

func (s *Store) UI() UIDoc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ui
}

// SaveUI stores data if base matches the current version, keeping the
// previous versions in ui-history/ for rollback.
func (s *Store) SaveUI(base int, data json.RawMessage) (UIDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if base != s.ui.Version {
		return s.ui, ErrConflict
	}
	if s.ui.Version > 0 {
		_ = writeFileJSON(filepath.Join(s.dir, "ui-history", fmt.Sprintf("%06d.json", s.ui.Version)), s.ui)
		s.pruneHistory()
	}
	doc := UIDoc{Version: s.ui.Version + 1, SavedAt: time.Now(), Data: data}
	if err := writeFileJSON(filepath.Join(s.dir, "ui.json"), doc); err != nil {
		return s.ui, err
	}
	s.ui = doc
	return doc, nil
}

func (s *Store) pruneHistory() {
	files, _ := filepath.Glob(filepath.Join(s.dir, "ui-history", "*.json"))
	sort.Strings(files)
	for len(files) > uiHistory {
		_ = os.Remove(files[0])
		files = files[1:]
	}
}

func (s *Store) Audit(e AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(e)
}

// RecentAudit returns up to n entries, newest first.
func (s *Store) RecentAudit(n int) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(filepath.Join(s.dir, "audit.jsonl"))
	if err != nil {
		return []AuditEntry{}
	}
	defer f.Close()
	var all []AuditEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e AuditEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	out := make([]AuditEntry, 0, n)
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out
}

// GrokUsage adds tokens to today's count and reports the new total.
func (s *Store) GrokUsage(add int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u struct {
		Day    string `json:"day"`
		Tokens int    `json:"tokens"`
	}
	path := filepath.Join(s.dir, "grok-usage.json")
	_ = readFileJSON(path, &u)
	today := time.Now().In(london).Format("2006-01-02")
	if u.Day != today {
		u.Day, u.Tokens = today, 0
	}
	if add > 0 {
		u.Tokens += add
		_ = writeFileJSON(path, u)
	}
	return u.Tokens
}

// appendChangelog adds one line to the admin box's CHANGELOG.md, locking it
// so other writers (the admin bot) don't interleave.
func appendChangelog(path string, e AuditEntry) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	line := fmt.Sprintf("- %s — %s (CirrusUX): %s", ukStamp(e.At), e.Who, e.What)
	if e.Undo != "" {
		line += fmt.Sprintf(" — undo: `%s`", e.Undo)
	}
	_, err = f.WriteString(line + "\n")
	return err
}

func readFileJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeFileJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
