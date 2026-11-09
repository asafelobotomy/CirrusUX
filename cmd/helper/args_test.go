package main

import (
	"strings"
	"testing"
)

// Each subcommand gets exactly the arguments it reads, so none can index
// past them (a panic as root), and each argument matches its pattern.
func TestValidArgs(t *testing.T) {
	ok := []string{
		"features", "apps-info", "storage-scan", "os-check",
		"stay-awake 30", "stay-awake forever",
		"app-job aaaa1111", "app-run aaaa1111",
		"os-run upgrade aaaa1111",
		"storage-run scan aaaa1111 -", "storage-run delete aaaa1111 old-photos",
		"archive-delete old-photos",
		"journal cirrusux-agent.service",
	}
	bad := []string{
		"", "features extra",
		"stay-awake", "stay-awake 31",
		"app-job", "app-job ../x", "app-run",
		"os-run", "os-run upgrade", "os-run reboot aaaa1111",
		"storage-run", "storage-run scan aaaa1111", "storage-run wipe aaaa1111 -",
		"archive-delete", "archive-delete ../etc",
		"journal", "journal sshd.service",
	}
	for _, s := range ok {
		if !validArgs(strings.Fields(s)) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range bad {
		if validArgs(strings.Fields(s)) {
			t.Errorf("%q accepted", s)
		}
	}
}
