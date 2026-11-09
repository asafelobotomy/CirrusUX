package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A line over 1 MB is skipped; the lines around it still come back.
func TestTailFileSkipsLongLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	body := "one\ntwo\n" + strings.Repeat("x", 3<<20) + "\nthree\nfour"
	os.WriteFile(p, []byte(body), 0o644)
	got, err := tailFile(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Lines, ",") != "two,three,four" {
		t.Errorf("got %v", got.Lines)
	}
	all, _ := tailFile(p, 10)
	if strings.Join(all.Lines, ",") != "one,two,three,four" {
		t.Errorf("got %d lines", len(all.Lines))
	}
}

func TestTailFileMissingIsEmpty(t *testing.T) {
	got, err := tailFile(filepath.Join(t.TempDir(), "none"), 5)
	if err != nil || got.Lines == nil || len(got.Lines) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}
