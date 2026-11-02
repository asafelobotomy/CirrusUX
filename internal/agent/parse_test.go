package agent

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 1, 20, 40, 0, 0, time.UTC)

// The recorded logs come from a machine on UK time.
func init() { logZone, _ = time.LoadLocation("Europe/London") }

func TestSplitTS(t *testing.T) {
	cases := map[string]string{
		"2026-11-01T18:08:00+00:00 SUSPEND: idle 1800s":            "2026-11-01T18:08:00Z",
		"2026-11-01 18:08:00 SUSPEND: idle 1800s":                  "2026-11-01T18:08:00Z",
		"[2026-11-01T18:08:00Z] SUSPEND":                           "2026-11-01T18:08:00Z",
		"Nov  1 18:08:00 homeserver idle-check: SUSPEND":           "2026-11-01T18:08:00Z",
		"2026-11-01T03:30:53+0000 homeserver systemd[1]: Starting": "2026-11-01T03:30:53Z",
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
	if segs[3].To != nil || segs[3].Reason != "Woken by network (irq 122)" {
		t.Errorf("last segment = %+v", segs[3])
	}
	if got := lastResume(ev, time.Time{}); !got.Equal(mustTime("2026-11-01T20:20:00Z")) {
		t.Errorf("lastResume = %v", got)
	}
	if got := lastActivity(ev); got == nil || !got.Equal(mustTime("2026-11-01T20:28:00Z")) {
		t.Errorf("lastActivity = %v", got)
	}
}

func TestReasonsFromMsg(t *testing.T) {
	cases := map[string][]string{
		"ACTIVE: 1 remote login session(s) host conns: 22<-100.64.0.2 ":               {"1 remote login session(s)", "SSH from 100.64.0.2"},
		"ACTIVE: host conns: 22457<-192.0.2.133 443<-192.0.2.40":                      {"Admin panel from 192.0.2.133", "HTTPS from 192.0.2.40"},
		"ACTIVE: playback by ignored client 192.0.2.23 (11 MB sent since last check)": {"playback by ignored client 192.0.2.23 (11 MB sent since last check)"},
		"ACTIVE: stay-awake flag": {"stay-awake flag"},
		"ACTIVE: stay-awake hold until 13:00; 1 login session(s); running cirrusux-backup.service; host conns: 22<-192.0.2.5 8096<-192.0.2.9": {
			"stay-awake hold until 13:00", "1 login session(s)", "running cirrusux-backup.service", "SSH from 192.0.2.5", "port 8096 from 192.0.2.9"},
		"IDLE: nothing active, timer started": nil,
		"":                                    nil,
	}
	for in, want := range cases {
		got := reasonsFromMsg(in, map[string]string{"22457": "Admin panel"})
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("reasonsFromMsg(%q) = %q, want %q", in, got, want)
		}
	}
}

// Lines in the exact format of /var/log/idle-check.log (1 Nov 2026),
// addresses replaced with documentation ones.
var realIdleLog = []string{
	"2026-11-01 17:37:32 GMT WAKE-DIAG 20261101-173201: slept ~299s irq 122; inbound after resume: 192.0.2.1 Reply 198.51.100.7 (details /var/log/wake-diag/natural-20261101-173201.summary)",
	"2026-11-01 17:37:52 GMT ACTIVE: 1 remote login session(s) host conns: 22<-100.64.0.2 ",
	"2026-11-01 17:39:10 GMT IDLE: nothing active, timer started",
	"2026-11-01 18:08:00 GMT SUSPEND: idle 1800s; RTC wake set Mon 2026-11-02 03:34:51 GMT",
	"2026-11-01 18:23:39 GMT RESUMED (wakeup irq 122)",
	"2026-11-01 18:24:04 GMT WAKE-DIAG 20261101-180800: slept ~933s irq 122; inbound after resume: Request 192.0.2.1 Reply ; last inbound before freeze: 198.51.100.7",
	"2026-11-01 18:38:01 GMT ACTIVE: playback by ignored client 192.0.2.23 (11 MB sent since last check)",
	"2026-11-01 18:59:45 GMT ACTIVE: host conns: 22<-100.64.0.2 ",
	"2026-11-01 19:08:30 GMT RESUMED (wakeup irq 122)",
	"2026-11-01 19:08:40 GMT IDLE: nothing active, timer started",
	"2026-11-01 19:56:45 GMT SUSPEND: idle 1810s; RTC wake set Mon 2026-11-02 03:29:10 GMT",
	"2026-11-02 03:29:12 GMT RESUMED (wakeup irq 8)",
}

// The log CirrusUX's own checker writes (internal/idle): RFC 3339 times.
func TestCirrusIdleLog(t *testing.T) {
	log := []string{
		"2026-11-01T17:37:52Z ACTIVE: 1 login session(s); host conns: 22<-192.0.2.5",
		"2026-11-01T17:40:00Z IDLE: nothing active, timer started",
		"2026-11-01T18:10:00Z SUSPEND: idle 1800s; RTC wake set Mon 2026-11-02 03:30:00 GMT",
		"2026-11-02T03:30:05Z RESUMED (wakeup irq 8)",
	}
	ev := parseIdleLog(log, now.Add(12*time.Hour))
	if len(ev) != 4 || ev[0].Kind != "ACTIVE" || ev[3].Kind != "RESUMED" {
		t.Fatalf("events = %+v", ev)
	}
	segs := timeline(ev, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), time.Time{})
	if len(segs) != 2 || segs[0].State != "asleep" || segs[1].Reason != "Scheduled wake (RTC)" {
		t.Errorf("timeline = %+v", segs)
	}
}

func TestRealIdleLog(t *testing.T) {
	ev := parseIdleLog(realIdleLog, now)
	if len(ev) != len(realIdleLog) {
		t.Fatalf("parsed %d of %d lines", len(ev), len(realIdleLog))
	}
	if ev[1].Kind != "ACTIVE" || !strings.HasPrefix(ev[1].Detail, "1 remote login") {
		t.Errorf("zone label not stripped: %+v", ev[1])
	}
	if !ev[3].At.Equal(mustTime("2026-11-01T18:08:00Z")) || ev[3].Kind != "SUSPEND" {
		t.Errorf("suspend = %+v", ev[3])
	}

	segs := timeline(ev[:11], now, time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	got := []string{}
	for _, s := range segs {
		got = append(got, s.State+"@"+s.From.UTC().Format("15:04")+" "+s.Reason)
	}
	want := []string{
		"awake@00:00 ",
		"asleep@18:08 ",
		"awake@18:23 Woken by network (irq 122)",
		// 19:08 RESUMED has no SUSPEND and no WAKE-DIAG: no sleep can be placed.
		"asleep@19:56 ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("timeline:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Next day: the 03:29 resume matches the RTC alarm set at 19:56.
	next := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	segs = timeline(ev, next, time.Time{})
	if last := segs[len(segs)-1]; last.State != "awake" || last.Reason != "Scheduled wake (RTC)" {
		t.Errorf("RTC wake = %+v", last)
	}
}

func TestResumeWithoutSuspendUsesWakeDiag(t *testing.T) {
	lines := []string{
		"2026-11-01 17:00:00 GMT IDLE: nothing active, timer started",
		"2026-11-01 17:40:00 GMT RESUMED (wakeup irq 122)",
		"2026-11-01 17:40:25 GMT WAKE-DIAG x: slept ~600s irq 122; inbound after resume: 192.0.2.1",
	}
	segs := timeline(parseIdleLog(lines, now), now, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	if len(segs) != 3 || segs[1].State != "asleep" || segs[1].From.UTC().Format("15:04") != "17:30" {
		t.Errorf("segments = %+v", segs)
	}
}

func TestParseEpoch(t *testing.T) {
	if p := parseEpoch("1793571617\n"); p == nil || p.Unix() != 1793571617 {
		t.Errorf("got %v", p)
	}
	if parseEpoch("") != nil || parseEpoch("x") != nil {
		t.Error("bad input should give nil")
	}
}

func TestParseConf(t *testing.T) {
	m := parseConf("# comment\nIDLE_MIN=30\nGRACE_MIN=\"15\" # minutes\nWAKE_TIMERS=\"system-backup.timer clamav-weekly-scan.timer\"\n")
	if confInt(m, "IDLE_MIN", 0) != 30 || confInt(m, "GRACE_MIN", 0) != 15 || confInt(m, "MISSING", 7) != 7 {
		t.Errorf("parsed %v", m)
	}
	if m["WAKE_TIMERS"] != "system-backup.timer clamav-weekly-scan.timer" {
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
		"2026-10-31T03:31:00+0000 homeserver systemd[1]: Starting system-backup.service - Nightly backup...",
		"2026-10-31T03:31:40+0000 homeserver systemd[1]: Finished system-backup.service - Nightly backup.",
		"2026-11-01T03:30:53+0000 homeserver systemd[1]: Starting system-backup.service - Nightly backup...",
		"2026-11-01T03:30:53+00:00 homeserver crontab[666619]: (root) LIST (root)",
		"2026-11-01T03:31:33+00:00 homeserver system-backup.sh[666620]: tar: /etc/someapp/agent.sock: socket ignored",
		"2026-11-01T03:31:33+0000 homeserver system-backup.sh[812]: OK 2.0G homeserver-system-2026-11-01_0330.tar.zst.age",
		"2026-11-01T03:31:33+0000 homeserver systemd[1]: system-backup.service: Deactivated successfully.",
		"2026-11-01T03:31:33+0000 homeserver systemd[1]: Finished system-backup.service - Nightly backup.",
	}
	r := parseBackupJournal(lines, "system-backup.service", now)
	if r == nil || !r.OK || !r.Start.Equal(mustTime("2026-11-01T03:30:53Z")) || !r.End.Equal(mustTime("2026-11-01T03:31:33Z")) {
		t.Fatalf("got %+v", r)
	}
	if r.Summary != "OK 2.0G homeserver-system-2026-11-01_0330.tar.zst.age" {
		t.Errorf("summary %q", r.Summary)
	}
	failed := append(lines[:3:3], "2026-11-01T03:31:00+0000 homeserver systemd[1]: system-backup.service: Failed with result 'exit-code'.")
	if r := parseBackupJournal(failed, "system-backup.service", now); r == nil || r.OK {
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
		{"docker.io/syncthing/syncthing@sha256:2b71da7c0d0a91a6ae5285ad079226fecc0a365d08b27720e1f7037b018f7546", "docker.io/syncthing/syncthing", "sha256:2b71da7c0d0a"},
		{"docker.io/syncthing/syncthing:2.1.7@sha256:2b71da7c0d0a91a6ae5285ad079226fecc0a365d08b27720e1f7037b018f7546", "docker.io/syncthing/syncthing", "2.1.7"},
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
