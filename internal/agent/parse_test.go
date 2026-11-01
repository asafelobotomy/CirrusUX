package agent

import (
	"testing"
	"time"
)

var now = time.Date(2026, 11, 1, 20, 40, 0, 0, time.UTC)

func TestSplitTS(t *testing.T) {
	cases := map[string]string{
		"2026-11-01T18:08:00+00:00 SUSPEND: idle 1800s":         "2026-11-01T18:08:00Z",
		"2026-11-01 18:08:00 SUSPEND: idle 1800s":               "2026-11-01T18:08:00Z",
		"[2026-11-01T18:08:00Z] SUSPEND":                        "2026-11-01T18:08:00Z",
		"Nov  1 18:08:00 cloud64 cloud64-idle: SUSPEND":         "2026-11-01T18:08:00Z",
		"2026-11-01T03:30:53+0000 cloud64 systemd[1]: Starting": "2026-11-01T03:30:53Z",
	}
	for in, want := range cases {
		at, _, ok := splitTS(in, now)
		if !ok || !at.UTC().Equal(mustTime(want)) {
			t.Errorf("splitTS(%q) = %v %v, want %s", in, at, ok, want)
		}
	}
	if _, _, ok := splitTS("no timestamp here", now); ok {
		t.Error("expected no timestamp")
	}
}

func TestTimeline(t *testing.T) {
	lines := []string{
		"2026-10-31T23:10:00Z SUSPEND: idle 1800s; RTC wake set 2026-11-01T03:27:00Z",
		"2026-11-01T03:27:10Z RESUMED (wakeup irq 9)",
		"2026-11-01T04:02:00Z ACTIVE: rsync",
		"2026-11-01T04:02:30Z SUSPEND: idle 1800s",
		"2026-11-01T20:20:00Z RESUMED (wakeup irq 122)",
		"2026-11-01T20:28:00Z ACTIVE: ssh session",
	}
	ev := parseIdleLog(lines, now)
	segs := timeline(ev, now, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	if len(segs) != 4 {
		t.Fatalf("got %d segments: %+v", len(segs), segs)
	}
	want := []string{"asleep", "awake", "asleep", "awake"}
	for i, s := range segs {
		if s.State != want[i] {
			t.Errorf("segment %d state %s, want %s", i, s.State, want[i])
		}
	}
	if segs[3].To != nil || segs[3].Reason != "Woken (irq 122)" {
		t.Errorf("last segment = %+v", segs[3])
	}
	if got := lastResume(ev, time.Time{}); !got.Equal(mustTime("2026-11-01T20:20:00Z")) {
		t.Errorf("lastResume = %v", got)
	}
	if got := lastActivity(ev); got == nil || !got.Equal(mustTime("2026-11-01T20:28:00Z")) {
		t.Errorf("lastActivity = %v", got)
	}
}

func TestReasonsFromDryRun(t *testing.T) {
	got := reasonsFromDryRun("checking…\nACTIVE: ssh session (admin); tcp 22, jellyfin stream\n")
	if len(got) != 3 || got[0] != "ssh session (admin)" || got[2] != "jellyfin stream" {
		t.Errorf("got %q", got)
	}
	if got := reasonsFromDryRun("IDLE: nothing active, timer started"); len(got) != 0 {
		t.Errorf("idle gave reasons %q", got)
	}
}

func TestParseConf(t *testing.T) {
	m := parseConf("# comment\nIDLE_MIN=30\nGRACE_MIN=\"15\" # minutes\nWAKE_TIMERS=\"cloud64-backup.timer clamav-weekly-scan.timer\"\n")
	if confInt(m, "IDLE_MIN", 0) != 30 || confInt(m, "GRACE_MIN", 0) != 15 || confInt(m, "MISSING", 7) != 7 {
		t.Errorf("parsed %v", m)
	}
	if m["WAKE_TIMERS"] != "cloud64-backup.timer clamav-weekly-scan.timer" {
		t.Errorf("WAKE_TIMERS = %q", m["WAKE_TIMERS"])
	}
}

func TestParseStayAwake(t *testing.T) {
	if u, f := parseStayAwake(""); u != nil || !f {
		t.Error("empty should be forever")
	}
	if u, f := parseStayAwake("1793570000\n"); f || u == nil || u.Unix() != 1793570000 {
		t.Errorf("epoch gave %v %v", u, f)
	}
}

func TestParseBackupJournal(t *testing.T) {
	lines := []string{
		"2026-10-31T03:31:00+0000 cloud64 systemd[1]: Starting cloud64-backup.service - Nightly backup...",
		"2026-10-31T03:31:40+0000 cloud64 systemd[1]: Finished cloud64-backup.service - Nightly backup.",
		"2026-11-01T03:30:53+0000 cloud64 systemd[1]: Starting cloud64-backup.service - Nightly backup...",
		"2026-11-01T03:31:33+0000 cloud64 cloud64-backup.sh[812]: OK 2.0G cloud64-system-2026-11-01_0330.tar.zst.age",
		"2026-11-01T03:31:33+0000 cloud64 systemd[1]: cloud64-backup.service: Deactivated successfully.",
		"2026-11-01T03:31:33+0000 cloud64 systemd[1]: Finished cloud64-backup.service - Nightly backup.",
	}
	r := parseBackupJournal(lines, now)
	if r == nil || !r.OK || !r.Start.Equal(mustTime("2026-11-01T03:30:53Z")) || !r.End.Equal(mustTime("2026-11-01T03:31:33Z")) {
		t.Fatalf("got %+v", r)
	}
	if r.Summary != "OK 2.0G cloud64-system-2026-11-01_0330.tar.zst.age" {
		t.Errorf("summary %q", r.Summary)
	}
	failed := append(lines[:3:3], "2026-11-01T03:31:00+0000 cloud64 systemd[1]: cloud64-backup.service: Failed with result 'exit-code'.")
	if r := parseBackupJournal(failed, now); r == nil || r.OK {
		t.Errorf("failed run = %+v", r)
	}
}

func TestParseClamAV(t *testing.T) {
	lines := []string{
		"----------- SCAN SUMMARY -----------",
		"Scanned files: 24098",
		"Infected files: 0",
		"Start Date: 2026:11:01 04:38:02",
		"End Date:   2026:11:01 05:11:40",
	}
	r := parseClamAV(lines)
	if r == nil || r.Files != 24098 || r.Infected != 0 || r.At.UTC().Hour() != 5 {
		t.Errorf("got %+v", r)
	}
}

func TestContainerHealth(t *testing.T) {
	cases := [][3]string{
		{"running", "Up 20 minutes (healthy)", "healthy"},
		{"running", "Up 5 seconds (health: starting)", "starting"},
		{"running", "Up 2 hours", "running"},
		{"exited", "Exited (0) 3 days ago", "exited"},
	}
	for _, c := range cases {
		if got := containerHealth(c[0], c[1]); got != c[2] {
			t.Errorf("containerHealth(%q, %q) = %q", c[0], c[1], got)
		}
	}
}

func TestSplitImage(t *testing.T) {
	cases := [][3]string{
		{"jellyfin/jellyfin:10.11.11", "jellyfin/jellyfin", "10.11.11"},
		{"nginx", "nginx", "latest"},
		{"localhost:5000/app", "localhost:5000/app", "latest"},
		{"lscr.io/linuxserver/kavita:v0.9.1.4-ls130", "lscr.io/linuxserver/kavita", "v0.9.1.4-ls130"},
	}
	for _, c := range cases {
		img, tag := splitImage(c[0])
		if img != c[1] || tag != c[2] {
			t.Errorf("splitImage(%q) = %q %q", c[0], img, tag)
		}
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
