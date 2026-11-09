// Package fsutil writes files so that a reader never sees half of one: the
// content goes to a temporary file of its own beside it, then replaces the
// old file in one rename. Two writers at once can't share a temporary
// file; the last rename wins.
package fsutil

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteFile replaces path with b, mode applied, creating its folder.
func WriteFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone already once renamed
	if err := tmp.Chmod(mode); err != nil {
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

// WriteJSON replaces path with v as indented JSON.
func WriteJSON(path string, v any, mode os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, b, mode)
}
