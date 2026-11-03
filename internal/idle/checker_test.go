package idle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSys answers like a quiet server unless told otherwise.
type fakeSys struct {
	who, units, est, conntrack string
	conntrack6                 string
	ssErr                      error
	boot                       time.Time
	suspends                   int
	irq                        string
}

func (f *fakeSys) Run(name string, args ...string) (string, error) {
	switch name {
	case "who":
		return f.who, nil
	case "systemctl":
		if args[0] == "list-timers" {
			return `[{"next": 1793682000000000, "unit": "cirrusux-backup.timer"}]`, nil
		}
		return f.units, nil
	case "ss":
		if f.ssErr != nil {
			return "", f.ssErr
		}
		if args[0] == "-Htln" {
			return "LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*", nil
		}
		return f.est, nil
	case "conntrack":
		if f.conntrack == "" {
			return "", ErrNotInstalled
		}
		if args[2] == "ipv6" {
			return f.conntrack6, nil
		}
		return f.conntrack, nil
	}
	return "", errors.New("unexpected " + name)
}
func (f *fakeSys) ReadFile(p string) (string, error) {
	if p == "/sys/power/pm_wakeup_irq" && f.irq != "" {
		return f.irq + "\n", nil
	}
	return "", os.ErrNotExist
}
func (f *fakeSys) Suspend() error      { f.suspends++; return nil }
func (f *fakeSys) BootTime() time.Time { return f.boot }

func newChecker(t *testing.T, sys *fakeSys, now *time.Time) *Checker {
	dir := t.TempDir()
	return &Checker{Cfg: DefaultConfig(), Sys: sys, Now: func() time.Time { return *now }, Dir: filepath.Join(dir, "state"), Log: filepath.Join(dir, "idle.log")}
}

func readLog(t *testing.T, c *Checker) string {
	b, _ := os.ReadFile(c.Log)
	return string(b)
}

// A day in miniature: someone on SSH, then quiet, then asleep, then woken.
func TestCheckerCycle(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-2 * time.Hour), est: "0 0 192.168.1.2:22 192.0.2.5:51000"}
	c := newChecker(t, sys, &now)

	for i := 0; i < 3; i++ {
		if d, _ := c.Check(); d.Verdict != Active {
			t.Fatalf("minute %d: %v", i, d.Msg)
		}
		now = now.Add(time.Minute)
	}
	sys.est = "" // they log out
	var d Decision
	for i := 0; i < 40 && sys.suspends == 0; i++ {
		d, _ = c.Check()
		now = now.Add(time.Minute)
	}
	if sys.suspends != 1 || d.Verdict != Suspend {
		t.Fatalf("never suspended: %+v", d)
	}
	log := readLog(t, c)
	for _, want := range []string{"ACTIVE: host conns: 22<-192.0.2.5", "IDLE: nothing active, timer started", "SUSPEND: idle 1", "RTC wake set "} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Count(log, "ACTIVE") != 1 {
		t.Errorf("ACTIVE should be logged once, when it starts:\n%s", log)
	}

	// The wake: the hook records it, and grace keeps it up afterwards.
	now = now.Add(5 * time.Hour)
	sys.irq = "122"
	if err := c.SleepHook("post"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute)
	if d, _ := c.Check(); d.Verdict != Idle || sys.suspends != 1 {
		t.Errorf("grace after wake: %v", d.Msg)
	}
	if !strings.Contains(readLog(t, c), "RESUMED (wakeup irq 122)") {
		t.Error("resume not logged")
	}
}

func TestHoldAndRelease(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-5 * time.Hour)}
	c := newChecker(t, sys, &now)
	if err := c.Hold(30); err != nil {
		t.Fatal(err)
	}
	if d, _ := c.Check(); d.Verdict != Active || !strings.Contains(d.Msg, "stay-awake hold until 12:30") {
		t.Errorf("hold: %v", d.Msg)
	}
	_ = c.Hold(0)
	now = now.Add(10 * time.Hour)
	if d, _ := c.Check(); d.Verdict != Active {
		t.Errorf("forever hold: %v", d.Msg)
	}
	_ = c.Release()
	if d, _ := c.Check(); d.Verdict == Active {
		t.Errorf("released: %v", d.Msg)
	}
	if err := c.Release(); err != nil {
		t.Errorf("releasing twice: %v", err)
	}
}

// --dry-run must write nothing at all (cloud64's old checker's did).
func TestDryRunWritesNothing(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-5 * time.Hour)}
	c := newChecker(t, sys, &now)
	var said []string
	c.DryRun, c.Out = true, func(s string) { said = append(said, s) }
	if d, _ := c.Check(); d.Verdict != Suspend {
		t.Fatalf("got %v", d.Msg)
	}
	if sys.suspends != 0 {
		t.Error("dry run suspended")
	}
	if _, err := os.Stat(c.Dir); err == nil {
		t.Error("dry run created the state folder")
	}
	if _, err := os.Stat(c.Log); err == nil {
		t.Error("dry run wrote the log")
	}
	if len(said) != 2 || !strings.Contains(said[1], "would suspend") {
		t.Errorf("said %q", said)
	}
}

// When it can't tell, it stays awake.
func TestUnreadableMeansAwake(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-5 * time.Hour), ssErr: errors.New("boom")}
	c := newChecker(t, sys, &now)
	if d, _ := c.Check(); d.Verdict != Active || sys.suspends != 0 {
		t.Errorf("got %v", d.Msg)
	}
	// A hold file it can't read counts as a hold.
	sys.ssErr = nil
	os.MkdirAll(c.Dir, 0o755)
	os.WriteFile(filepath.Join(c.Dir, "hold"), []byte("garbage\n"), 0o644)
	if d, _ := c.Check(); d.Verdict != Active {
		t.Errorf("unreadable hold: %v", d.Msg)
	}
}

func TestResumeWithoutHook(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-24 * time.Hour)}
	c := newChecker(t, sys, &now)
	c.writeTime("last-suspend", now.Add(-3*time.Hour))
	c.writeTime("last-wake", now.Add(-4*time.Hour))
	if d, _ := c.Check(); d.Verdict != Idle {
		t.Errorf("a resume the hook missed still gets grace: %v", d.Msg)
	}
	if !strings.Contains(readLog(t, c), "RESUMED (not noted") {
		t.Error("missed resume not logged")
	}
}

// tvFlow is the TV's connection to Jellyfin in a container, with bytes
// sent back to it so far (conntrack with accounting on).
func tvFlow(sent int) string {
	return fmt.Sprintf("tcp      6 431999 ESTABLISHED src=192.0.2.23 dst=192.168.1.2 sport=40000 dport=8096 packets=900 bytes=60000 src=10.88.0.5 dst=192.0.2.23 sport=8096 dport=40000 packets=9000 bytes=%d [ASSURED] mark=0 use=1", sent)
}

// The TV holding a connection open is ignored; the TV streaming isn't.
func TestIgnoredClientPlayback(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-5 * time.Hour), conntrack: tvFlow(1_000_000)}
	c := newChecker(t, sys, &now)
	c.Cfg.IgnoreClients, c.Cfg.IgnorePlayMB = []string{"192.0.2.23"}, 5

	if d, _ := c.Check(); d.Verdict == Active {
		t.Fatalf("first look has nothing to compare with: %v", d.Msg)
	}
	now = now.Add(time.Minute)
	sys.conntrack = tvFlow(3_000_000) // 2 MB in a minute: idling on a menu
	if d, _ := c.Check(); d.Verdict == Active {
		t.Errorf("2 MB/min counted: %v", d.Msg)
	}
	now = now.Add(time.Minute)
	sys.conntrack = tvFlow(15_000_000) // 12 MB: a film
	d, _ := c.Check()
	if d.Verdict != Active || d.Msg != "ACTIVE: playback by ignored client 192.0.2.23 (12 MB sent since last check)" {
		t.Errorf("playback: %v", d.Msg)
	}
}

// Woken, and only the TV turned up: back to sleep after the short grace.
func TestIgnoredWakeGrace(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-24 * time.Hour), conntrack: tvFlow(0)}
	c := newChecker(t, sys, &now)
	c.Cfg.IgnoreClients, c.Cfg.IgnoreWakeGraceMin = []string{"192.0.2.23"}, 5
	c.writeTime("last-active", now.Add(-3*time.Hour))
	if err := c.SleepHook("post"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if d, _ := c.Check(); d.Verdict != Idle || !strings.Contains(d.Msg, "only ignored clients since the wake, sleeps at 12:05") {
		t.Errorf("short grace: %v", d.Msg)
	}
	now = now.Add(3 * time.Minute)
	if d, _ := c.Check(); d.Verdict != Suspend {
		t.Errorf("after the short grace: %v", d.Msg)
	}

	// The same wake, but someone else used it: the usual times apply.
	now = now.Add(time.Hour)
	_ = c.SleepHook("post")
	sys.est = "0 0 192.168.1.2:22 192.0.2.5:51000"
	if d, _ := c.Check(); d.Verdict != Active {
		t.Fatalf("ssh: %v", d.Msg)
	}
	sys.est = ""
	now = now.Add(6 * time.Minute)
	if d, _ := c.Check(); d.Verdict != Idle {
		t.Errorf("someone else used it, yet the short grace applied: %v", d.Msg)
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig("# comment\nIDLE_MIN=20\nGRACE_MIN=\"5\"\nWATCH_PORTS=22 8096\nIGNORE_CLIENTS=192.0.2.23\nIGNORE_CLIENTS_PLAY_MB=5\nIGNORE_WAKE_GRACE_MIN=5\nMAX_LOAD=1.5\nCOUNT_LOGINS=no\nWAKE_TIMERS=a.timer b.timer\n")
	if err != nil || cfg.IdleMin != 20 || cfg.GraceMin != 5 || len(cfg.WatchPorts) != 2 || cfg.MaxLoad != 1.5 || cfg.CountLogins || len(cfg.WakeTimers) != 2 ||
		cfg.IgnorePlayMB != 5 || cfg.IgnoreWakeGraceMin != 5 {
		t.Errorf("%+v %v", cfg, err)
	}
	for _, bad := range []string{"IDLE_MIN=0", "WATCH_PORTS=ssh", "SHELL=/bin/sh", "nonsense", "IGNORE_CLIENTS_PLAY_MB=-1", "IGNORE_WAKE_GRACE_MIN=x"} {
		if _, err := ParseConfig(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
