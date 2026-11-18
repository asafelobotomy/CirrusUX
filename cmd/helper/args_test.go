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
		"self-upgrade v0.9.0", "self-rollback",
		"self-run upgrade aaaa1111 v0.9.0", "self-run rollback aaaa1111 -",
	}
	bad := []string{
		"", "features extra",
		"stay-awake", "stay-awake 31",
		"app-job", "app-job ../x", "app-run",
		"os-run", "os-run upgrade", "os-run reboot aaaa1111",
		"storage-run", "storage-run scan aaaa1111", "storage-run wipe aaaa1111 -",
		"archive-delete", "archive-delete ../etc",
		"journal", "journal sshd.service",
		"self-upgrade", "self-upgrade latest", "self-upgrade 0.9.0", "self-upgrade v0.9.0 -y", "self-upgrade --from=/tmp",
		"self-rollback v0.8.1", "self-rollback -y",
		"self-run", "self-run upgrade aaaa1111", "self-run upgrade aaaa1111 -", "self-run rollback aaaa1111 v0.9.0",
		"self-run apply aaaa1111 -", "self-run upgrade ../x v0.9.0",
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
