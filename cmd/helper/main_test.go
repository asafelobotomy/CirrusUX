package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/helperapi"
)

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	var b strings.Builder
	for i := range 10 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	os.WriteFile(path, []byte(b.String()), 0o600)
	got, err := tailFile(path, 3)
	if err != nil || strings.Join(got.Lines, ",") != "line 7,line 8,line 9" {
		t.Errorf("tail = %v, %v", got.Lines, err)
	}
	if got, _ := tailFile(path, 50); len(got.Lines) != 10 {
		t.Errorf("short file: %d lines", len(got.Lines))
	}
	if _, err := tailFile(filepath.Join(t.TempDir(), "missing"), 3); err == nil {
		t.Error("missing file should fail")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abcdef", 3) != "abc" || truncate("ab", 3) != "ab" {
		t.Error("truncate")
	}
}

// Only finished system archives are listed: not .partial files, other
// names or directories.
func TestBackupList(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"atlas-system-20261101.tar.zst.age", "atlas-system-20261102.tar.zst.age.partial", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("12345"), 0o600)
	}
	os.Mkdir(filepath.Join(dir, "atlas-system-dir"), 0o700)
	l, err := backupList(dir, "atlas-system-")
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "atlas-system-20261101.tar.zst.age" || l.Files[0].Bytes != 5 || l.Files[0].MTime == 0 {
		t.Errorf("list = %+v, %v", l.Files, err)
	}
}

func TestTimeoutFor(t *testing.T) {
	if timeoutFor(helperapi.CmdEgressReport) != 150*time.Second || timeoutFor(helperapi.CmdIdleLog) != 30*time.Second {
		t.Error("timeouts")
	}
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "helper.env")
	os.WriteFile(p, []byte(body), 0o600)
	ownerCheck = func(*os.File) error { return nil }
	t.Cleanup(func() { ownerCheck = rootOnly })
	return p
}

func TestLoadConf(t *testing.T) {
	c, err := loadConf(writeConf(t, `# comment
IDLE_STATE_DIR=/run/idle
IDLE_LOG="/var/log/idle.log"
STAY_AWAKE_FILE=/run/stay-awake
STAY_AWAKE_BIN=/usr/local/bin/stay-awake
ALLOW_SLEEP_BIN=/usr/local/bin/allow-sleep
BACKUP_UNIT=system-backup.service
FAIL2BAN_JAIL=sshd
FIREWALL_CHECKS=DOCKER-USER:LOCKDOWN INPUT-LAN:DROP inet/cirrusux/input:drop
EGRESS_REPORT=
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sleep", "stay-awake", "backup", "security"}
	if got := c.features().Modules; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("features = %v, want %v", got, want)
	}
	if len(c.FirewallChecks) != 3 || c.FirewallChecks[1] != (firewallCheck{"INPUT-LAN", "DROP"}) ||
		strings.Join(c.FirewallChecks[2].chainCommand(), " ") != "nft list chain inet cirrusux input" {
		t.Errorf("firewall checks = %+v", c.FirewallChecks)
	}
	if c, err := loadConf(filepath.Join(t.TempDir(), "missing")); err != nil || len(c.features().Modules) != 0 {
		t.Errorf("no file should mean no modules: %+v %v", c, err)
	}
}

// Paths must be absolute and clean, and words can't smuggle options.
func TestLoadConfRejects(t *testing.T) {
	for _, body := range []string{
		"IDLE_LOG=relative/log",
		"IDLE_LOG=/var/log/../../etc/shadow",
		"BACKUP_UNIT=--help",
		"BACKUP_UNIT=a;b",
		"FIREWALL_CHECKS=DOCKER-USER",
		"FIREWALL_CHECKS=inet/cirrusux:drop",
		"FIREWALL_CHECKS=inet/../input:drop",
		"SHELL=/bin/sh",
	} {
		if _, err := loadConf(writeConf(t, body)); err == nil {
			t.Errorf("%q should be rejected", body)
		}
	}
}

// Real config files must belong to root; a file the test user owns fails.
func TestRootOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "helper.env")
	os.WriteFile(p, nil, 0o600)
	f, _ := os.Open(p)
	defer f.Close()
	if os.Getuid() != 0 && rootOnly(f) == nil {
		t.Error("a file owned by a normal user passed")
	}
}

// The shipped example and the cloud64 profile must both load. The profile
// leaves the repo before it goes public, so it's skipped when missing.
func TestShippedConfigs(t *testing.T) {
	ownerCheck = func(*os.File) error { return nil }
	t.Cleanup(func() { ownerCheck = rootOnly })
	if c, err := loadConf("../../deploy/server/helper.env.example"); err != nil || len(c.features().Modules) != 0 {
		t.Errorf("example: %v %v", c.features(), err)
	}
	p := "../../deploy/profiles/cloud64/helper.env"
	if _, err := os.Stat(p); err != nil {
		t.Skip("no cloud64 profile")
	}
	c, err := loadConf(p)
	if err != nil || len(c.features().Modules) != 6 {
		t.Errorf("cloud64: %v %v", c.features(), err)
	}
}

// Setup modules drop their own files in helper.d; they add to helper.env.
func TestLoadAll(t *testing.T) {
	main := writeConf(t, "IDLE_LOG=/var/log/idle.log\nFIREWALL_CHECKS=DOCKER-USER:DROP\n")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "50-backup.env"), []byte("BACKUP_UNIT=cirrusux-backup.service\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "60-security.env"), []byte("FIREWALL_CHECKS=inet/cirrusux/input:drop\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644)
	c, err := loadAll(main, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupUnit != "cirrusux-backup.service" || c.IdleLog == "" || len(c.FirewallChecks) != 2 {
		t.Errorf("merged = %+v", c)
	}
	os.WriteFile(filepath.Join(dir, "70-bad.env"), []byte("SHELL=/bin/sh\n"), 0o644)
	if _, err := loadAll(main, dir); err == nil {
		t.Error("a bad helper.d file should fail the whole load")
	}
}
