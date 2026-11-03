package appjob

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"cirrusux/internal/fsutil"
)

func (e *Env) record(id string) (Record, error) {
	var r Record
	b, err := os.ReadFile(filepath.Join(e.RecordsDir, id+".json"))
	if err != nil {
		return r, fmt.Errorf("no record of %s: %v", id, err)
	}
	return r, json.Unmarshal(b, &r)
}

func (e *Env) writeStatus(st *Status) error {
	return writeJSON(filepath.Join(e.StatusDir, st.Job+".json"), st, 0o644)
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

func writeJSON(path string, v any, mode os.FileMode) error { return fsutil.WriteJSON(path, v, mode) }
