package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/store"
)

func TestPublishRoundTrip(t *testing.T) {
	pub, sec, err := store.GenerateKey("example", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Now()
	exp := now.Add(48 * time.Hour)
	defs, _ := filepath.Glob("../../internal/catalogue/apps/*.json")
	if len(defs) < 2 {
		t.Fatal("no built-in definitions to sign")
	}
	if _, err := signApps(dir, sec, "example", exp, defs); err != nil {
		t.Fatal(err)
	}
	if _, err := buildIndex(dir, sec, "example", exp, now); err != nil {
		t.Fatal(err)
	}
	src := store.Source{Name: "example", Keys: []store.Key{pub}}
	ix, err := verifyDir(dir, src, now)
	if err != nil || ix.Serial != 1 || len(ix.Apps) != len(defs) {
		t.Fatalf("verifyDir = %+v, %v", ix, err)
	}

	// Re-signing an unchanged definition keeps its serial; a changed one
	// moves up, and the index has to follow.
	if _, err := signApps(dir, sec, "example", exp, defs[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyDir(dir, src, now); err != nil {
		t.Fatalf("re-signing the same definition broke the folder: %v", err)
	}
	raw, _ := os.ReadFile(defs[0])
	changed := filepath.Join(t.TempDir(), filepath.Base(defs[0]))
	os.WriteFile(changed, []byte(strings.Replace(string(raw), `"description": "`, `"description": "New: `, 1)), 0o644)
	if _, err := signApps(dir, sec, "example", exp, []string{changed}); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyDir(dir, src, now); err == nil || !strings.Contains(err.Error(), "run index again") {
		t.Fatalf("a stale index passed: %v", err)
	}
	ix, err = buildIndex(dir, sec, "example", exp, now)
	if err != nil || ix.Serial != 2 {
		t.Fatalf("second index = %+v, %v", ix, err)
	}
	for _, e := range ix.Apps {
		if filepath.Base(defs[0]) == e.ID+".json" && e.Serial != 2 {
			t.Fatalf("changed app kept serial %d", e.Serial)
		}
	}

	_, other, _ := store.GenerateKey("other", nil)
	if _, err := verifyDir(dir, store.Source{Name: "example", Keys: []store.Key{other.Public()}}, now); err == nil {
		t.Fatal("verified with the wrong key")
	}
	if _, err := verifyDir(dir, src, exp.Add(time.Hour)); err == nil {
		t.Fatal("verified after expiry")
	}
}
