package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type entry struct {
	name, link, body string
	typ              byte
	mode             int64
}

func tarGz(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.typ, Mode: e.mode, Size: int64(len(e.body))}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	zw.Close()
	return &buf
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	src := tarGz(t,
		entry{name: "pax_global_header", typ: tar.TypeXGlobalHeader},
		entry{name: "src/", typ: tar.TypeDir, mode: 0o755},
		entry{name: "src/main.go", typ: tar.TypeReg, mode: 0o644, body: "package main"},
		entry{name: "run.sh", typ: tar.TypeReg, mode: 0o4755, body: "#!/bin/sh"},
		entry{name: "deep/er/file", typ: tar.TypeReg, mode: 0o644, body: "x"},
		entry{name: "link", typ: tar.TypeSymlink, link: "/etc/passwd"},
	)
	if err := Extract(src, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "src/main.go")); string(b) != "package main" {
		t.Fatalf("main.go: %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "run.sh")); fi.Mode().Perm() != 0o755 || fi.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("run.sh mode %v", fi.Mode())
	}
	if l, _ := os.Readlink(filepath.Join(dir, "link")); l != "/etc/passwd" {
		t.Fatalf("link %q", l)
	}
}

func TestExtractRefuses(t *testing.T) {
	outside := t.TempDir()
	for name, entries := range map[string][]entry{
		"dotdot":   {{name: "../x", typ: tar.TypeReg, body: "x"}},
		"absolute": {{name: "/tmp/x", typ: tar.TypeReg, body: "x"}},
		"through a link": {
			{name: "out", typ: tar.TypeSymlink, link: outside},
			{name: "out/pwned", typ: tar.TypeReg, body: "x"},
		},
		"over a link": {
			{name: "f", typ: tar.TypeSymlink, link: filepath.Join(outside, "f")},
			{name: "f", typ: tar.TypeReg, body: "x"},
		},
	} {
		dir := t.TempDir()
		if err := Extract(tarGz(t, entries...), dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("wrote outside: %v", ents)
	}
}
