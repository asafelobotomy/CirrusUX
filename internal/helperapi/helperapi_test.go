package helperapi

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestJobRe(t *testing.T) {
	good := []string{"0123abcd", "0123456789abcdef", strings.Repeat("a", 32)}
	bad := []string{"", "0123abc", strings.Repeat("a", 33), "0123ABCD", "0123abcg",
		"0123abcd\n", "../0123abcd", "0123abcd/..", " 0123abcd", "0123abcd.json"}
	for _, s := range good {
		if !JobRe.MatchString(s) {
			t.Errorf("JobRe should accept %q", s)
		}
	}
	for _, s := range bad {
		if JobRe.MatchString(s) {
			t.Errorf("JobRe should reject %q", s)
		}
	}
}

func TestUnitRe(t *testing.T) {
	good := []string{"cirrusux-agent.service", "cirrusux-a1.service", "cirrusux-" + strings.Repeat("a", 40) + ".service"}
	bad := []string{"", "cirrusux-.service", "cirrusux-agent", "cirrusux-agent.timer", "sshd.service",
		"cirrusux-Agent.service", "cirrusux-agent.service\n", "cirrusux-a/b.service", "cirrusux-../x.service",
		"cirrusux-" + strings.Repeat("a", 41) + ".service", "x-cirrusux-agent.service", "cirrusux-agent.service --user"}
	for _, s := range good {
		if !UnitRe.MatchString(s) {
			t.Errorf("UnitRe should accept %q", s)
		}
	}
	for _, s := range bad {
		if UnitRe.MatchString(s) {
			t.Errorf("UnitRe should reject %q", s)
		}
	}
}

func TestStayAwakeArgs(t *testing.T) {
	if want := []string{"30", "120", "forever"}; !slices.Equal(StayAwakeArgs, want) {
		t.Errorf("StayAwakeArgs = %v, want %v (the sudoers file names each)", StayAwakeArgs, want)
	}
}

func TestCommands(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Commands {
		if c == "" || c != strings.ToLower(c) || strings.ContainsAny(c, " \t") {
			t.Errorf("odd command name %q", c)
		}
		if seen[c] {
			t.Errorf("command %q listed twice", c)
		}
		seen[c] = true
	}
	// Run by systemd, never through sudo: they must not be in the sudo list.
	for _, c := range []string{CmdAppRun, CmdOSRun, CmdStorageRun} {
		if seen[c] {
			t.Errorf("%q is started by systemd and must not be in Commands", c)
		}
	}
	// Every other constant is a sudo-able subcommand.
	for _, c := range []string{CmdFeatures, CmdAppsInfo, CmdIdleStatus, CmdIdleLog, CmdBackupList, CmdBackupLast,
		CmdAVLast, CmdSecurityStatus, CmdStayAwake, CmdAllowSleep, CmdNudgeAwake, CmdBackupRun, CmdEgressReport,
		CmdAppJob, CmdOSCheck, CmdOSUpgrade, CmdJournal, CmdStorageScan, CmdArchiveDelete} {
		if !seen[c] {
			t.Errorf("%q missing from Commands", c)
		}
	}
}

func marshal(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWireNames(t *testing.T) {
	// The agent decodes what the helper prints, so the names are a contract.
	if m := marshal(t, Features{Modules: []string{ModSleep}}); m["modules"] == nil {
		t.Errorf("Features: %v", m)
	}
	if m := marshal(t, BackupFile{Name: "a", MTime: 1, Bytes: 2}); m["name"] != "a" || m["mtime"] != 1.0 || m["bytes"] != 2.0 {
		t.Errorf("BackupFile: %v", m)
	}
	m := marshal(t, IdleStatus{State: map[string]string{"last-msg": "x"}})
	if v, ok := m["stayAwake"]; !ok || v != nil {
		t.Errorf("IdleStatus without a hold must print stayAwake:null, got %v", m)
	}
	hold := "forever"
	if m := marshal(t, IdleStatus{StayAwake: &hold}); m["stayAwake"] != "forever" {
		t.Errorf("IdleStatus hold: %v", m)
	}
	m = marshal(t, SecurityStatus{Firewall: []FirewallCheck{{Chain: "input", OK: true}}})
	if _, ok := m["errors"]; ok {
		t.Errorf("SecurityStatus errors should be omitted when empty: %v", m)
	}
	for _, k := range []string{"fail2ban", "banned", "firewall"} {
		if _, ok := m[k]; !ok {
			t.Errorf("SecurityStatus lacks %q: %v", k, m)
		}
	}
	m = marshal(t, AppsInfo{})
	if _, ok := m["bind"]; ok {
		t.Errorf("AppsInfo bind should be omitted when empty: %v", m)
	}
	for _, k := range []string{"runtime", "appsDir", "mediaRoots", "tz", "uid", "gid", "groups", "sources"} {
		if _, ok := m[k]; !ok {
			t.Errorf("AppsInfo lacks %q: %v", k, m)
		}
	}
}
