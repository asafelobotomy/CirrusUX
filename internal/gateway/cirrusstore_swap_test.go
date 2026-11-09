package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapDir(t *testing.T) {
	root := t.TempDir()
	cache, fresh := filepath.Join(root, "src"), filepath.Join(root, "src.new")
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	// No old cache yet.
	os.MkdirAll(fresh, 0o700)
	os.WriteFile(filepath.Join(fresh, "f"), []byte("one"), 0o600)
	if err := swapDir(fresh, cache); err != nil || read(filepath.Join(cache, "f")) != "one" {
		t.Fatalf("first swap: %v", err)
	}

	// Replace an existing cache; nothing is left aside.
	os.MkdirAll(fresh, 0o700)
	os.WriteFile(filepath.Join(fresh, "f"), []byte("two"), 0o600)
	if err := swapDir(fresh, cache); err != nil || read(filepath.Join(cache, "f")) != "two" {
		t.Fatalf("second swap: %v", err)
	}
	if _, err := os.Stat(cache + ".old"); !os.IsNotExist(err) {
		t.Error("old cache left behind")
	}

	// The new directory is missing: the old cache must survive.
	if err := swapDir(filepath.Join(root, "missing"), cache); err == nil {
		t.Fatal("swap of a missing directory succeeded")
	}
	if read(filepath.Join(cache, "f")) != "two" {
		t.Error("old cache lost when the swap failed")
	}
}
