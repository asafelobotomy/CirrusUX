package main

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/store"
)

func TestStoreSources(t *testing.T) {
	k, _, _ := store.GenerateKey("extra", nil)
	dir := t.TempDir()
	storeDir = dir
	t.Cleanup(func() { storeDir = store.SourcesDir })
	ownerCheck = func(*os.File) error { return nil }
	t.Cleanup(func() { ownerCheck = rootOnly })
	os.WriteFile(filepath.Join(dir, "extra.source"), []byte("url=https://example.org/\nkey="+k.String()+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.source"), []byte("url=http://example.org/\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "old.source.bak-20261103-1200"), []byte("ignored"), 0o644)

	srcs, infos := storeSources(conf{})
	if len(srcs) != 2 || srcs[0].Name != store.Official || srcs[1].Name != "extra" {
		t.Fatalf("sources = %+v", srcs)
	}
	if len(infos) != 3 || infos[1].Name != "broken" || infos[1].Error == "" || infos[2].Fingerprints[0] != k.Fingerprint() {
		t.Fatalf("infos = %+v", infos)
	}
	if srcs, _ := storeSources(conf{StoreNoOfficial: true}); len(srcs) != 1 || srcs[0].Name != "extra" {
		t.Fatalf("STORE_OFFICIAL=no: %+v", srcs)
	}

	// A source file root doesn't own is never trusted.
	ownerCheck = func(f *os.File) error { return errors.New(f.Name() + " must belong to root") }
	if srcs, infos := storeSources(conf{StoreNoOfficial: true}); len(srcs) != 0 || infos[1].Error == "" {
		t.Fatalf("an unowned source was trusted: %+v", srcs)
	}
}

// The helper's job runner trusts exactly the sources it reads: an app from
// one needs a definition signed by that source's key.
func TestHelperVerifiesDefinitions(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	k, sec, _ := store.GenerateKey("extra", nil)
	_, other, _ := store.GenerateKey("extra", nil)
	dir := t.TempDir()
	storeDir = dir
	t.Cleanup(func() { storeDir = store.SourcesDir })
	ownerCheck = func(*os.File) error { return nil }
	t.Cleanup(func() { ownerCheck = rootOnly })
	os.WriteFile(filepath.Join(dir, "extra.source"), []byte("url=https://example.org/\nkey="+k.String()+"\n"), 0o644)

	env, err := appEnv(conf{ContainerRuntime: "podman", AppsDir: t.TempDir(), AppsUser: me.Username, StoreNoOfficial: true})
	if err != nil {
		t.Fatal(err)
	}
	def, _ := os.ReadFile("../../internal/catalogue/apps/navidrome.json")
	signed := func(s store.Secret) *store.Signed {
		b, _, err := store.SealApp("extra", 1, time.Now().Add(time.Hour), def)
		if err != nil {
			t.Fatal(err)
		}
		return &store.Signed{Data: b, Sig: string(s.Sign(b))}
	}
	req := appjob.Request{Job: "aaaa1111", Action: "install", App: "navidrome", Source: "extra"}
	if _, err := env.Check(req); err == nil || !strings.Contains(err.Error(), "needs its signed definition") {
		t.Errorf("unsigned: %v", err)
	}
	req.Definition = signed(other)
	if _, err := env.Check(req); !errors.Is(err, store.ErrSignature) {
		t.Errorf("wrongly signed: %v", err)
	}
	req.Definition = signed(sec)
	if _, err := env.Check(req); err != nil && (errors.Is(err, store.ErrSignature) || strings.Contains(err.Error(), "trusts")) {
		t.Errorf("correctly signed: %v", err)
	}
}
