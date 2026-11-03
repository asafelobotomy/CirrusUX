package fsutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Writers at the same time each finish whole, and nothing is left beside.
func TestWriteConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.json")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := WriteJSON(path, map[string]int{"n": i}, 0o640); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left %d files", len(entries))
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("stat = %v, %v", fi, err)
	}
}
