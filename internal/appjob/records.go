package appjob

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/store"
)

// Status is a job's progress, rewritten at every step.
type Status struct {
	Job      string     `json:"job"`
	Action   string     `json:"action"`
	App      string     `json:"app"`
	Name     string     `json:"name"`
	State    string     `json:"state"` // running, done, failed
	Step     string     `json:"step"`
	Detail   string     `json:"detail,omitempty"` // e.g. pull progress
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
}

// Record is an installed app, kept in RecordsDir/<id>.json.
type Record struct {
	App    string `json:"app"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Digest string `json:"digest,omitempty"`
	// The version before the last update, for rolling back.
	PreviousTag    string            `json:"previousTag,omitempty"`
	PreviousDigest string            `json:"previousDigest,omitempty"`
	Updated        time.Time         `json:"updated,omitempty"`
	Runtime        string            `json:"runtime"`
	MainPort       int               `json:"mainPort"`
	HTTPS          bool              `json:"https,omitempty"`
	Options        catalogue.Options `json:"options"`
	Installed      time.Time         `json:"installed"`
	// Where the definition came from: Source "" is the built-in catalogue;
	// otherwise Definition is the exact signed envelope installed, which
	// updates, rollbacks and removal use from then on.
	Source     string        `json:"source,omitempty"`
	Serial     int64         `json:"serial,omitempty"`
	Definition *store.Signed `json:"definition,omitempty"`
	// The same for the version before the last update.
	PreviousSource     string        `json:"previousSource,omitempty"`
	PreviousSerial     int64         `json:"previousSerial,omitempty"`
	PreviousDefinition *store.Signed `json:"previousDefinition,omitempty"`
}

func (e *Env) record(id string) (Record, error) {
	var r Record
	b, err := os.ReadFile(filepath.Join(e.RecordsDir, id+".json"))
	if err != nil {
		return r, fmt.Errorf("no record of %s: %v", id, err)
	}
	return r, json.Unmarshal(b, &r)
}

func (e *Env) writeStatus(st *Status) error {
	return fsutil.WriteJSON(filepath.Join(e.StatusDir, st.Job+".json"), st, 0o644)
}

// Records lists the installed apps.
func Records(dir string) []Record {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []Record
	for _, f := range files {
		var r Record
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Statuses lists job statuses, newest first.
func Statuses(dir string) []Status {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []Status
	for _, f := range files {
		var s Status
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &s) == nil {
			out = append(out, s)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Started.After(out[j-1].Started); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// lockApp holds an exclusive lock on the app until unlock is called; it
// waits for another job of the same app to finish. The lock is a file in
// StatusDir, released by the kernel if the job dies.
func (e *Env) lockApp(id string) (unlock func(), err error) {
	if err := os.MkdirAll(e.StatusDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(e.StatusDir, id+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// PruneStatuses removes job status (.json) and lock (.lock) files in dir
// that haven't changed for maxAge, so /run/cirrusux-jobs doesn't grow
// without end. A lock somebody holds is left alone.
func PruneStatuses(dir string, maxAge time.Duration, now time.Time) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.Type().IsRegular() || (ext != ".json" && ext != ".lock") {
			continue
		}
		fi, err := e.Info()
		if err != nil || now.Sub(fi.ModTime()) < maxAge {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if ext == ".lock" {
			f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
			if err != nil {
				continue
			}
			busy := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil
			if !busy {
				_ = os.Remove(path)
			}
			f.Close()
			continue
		}
		_ = os.Remove(path)
	}
}
