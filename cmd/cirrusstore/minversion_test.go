package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/store"
	"cirrusux/internal/version"
)

func TestMinVersionReachesTheIndex(t *testing.T) {
	prev := version.Version
	version.Version = "0.7.0"
	defer func() { version.Version = prev }()

	pub, sec, _ := store.GenerateKey("example", nil)
	dir, now := t.TempDir(), time.Now()
	exp := now.Add(48 * time.Hour)
	raw, _ := os.ReadFile("../../internal/catalogue/apps/navidrome.json")
	def := filepath.Join(t.TempDir(), "navidrome.json")
	os.WriteFile(def, []byte(strings.Replace(string(raw), `"id": "navidrome"`, `"id": "navidrome", "minVersion": "0.7.0"`, 1)), 0o644)
	if _, err := signApps(dir, sec, "example", exp, []string{def}); err != nil {
		t.Fatal(err)
	}
	ix, err := buildIndex(dir, sec, "example", exp, now)
	if err != nil || len(ix.Apps) != 1 || ix.Apps[0].MinVersion != "0.7.0" {
		t.Fatalf("index = %+v, %v", ix, err)
	}
	if _, err := verifyDir(dir, store.Source{Name: "example", Keys: []store.Key{pub}}, now); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// A definition for a newer CirrusUX than this tool can't be signed here.
	os.WriteFile(def, []byte(strings.Replace(string(raw), `"id": "navidrome"`, `"id": "navidrome", "minVersion": "0.8.0"`, 1)), 0o644)
	if _, err := signApps(t.TempDir(), sec, "example", exp, []string{def}); err == nil {
		t.Fatal("signed a definition this tool can't check")
	}
}
