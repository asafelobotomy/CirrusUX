package idle

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type failingSys struct{ fakeSys }

func (f *failingSys) Suspend() error { f.suspends++; return errors.New("denied") }

// A suspend that fails must not leave last-suspend behind: the next check
// would log a resume that never happened.
func TestFailedSuspendLeavesNoLastSuspend(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &failingSys{fakeSys{boot: now.Add(-5 * time.Hour)}}
	c := newChecker(t, &sys.fakeSys, &now)
	c.Sys = sys
	d, err := c.Check()
	if d.Verdict != Suspend || err == nil {
		t.Fatalf("want a failed suspend, got %v, %v", d.Verdict, err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "last-suspend")); !os.IsNotExist(err) {
		t.Errorf("last-suspend left behind: %v", err)
	}
	now = now.Add(5 * time.Minute)
	c.Check()
	if strings.Contains(readLog(t, c), "RESUMED") {
		t.Errorf("a resume was invented:\n%s", readLog(t, c))
	}
}

// A failed suspend puts back the previous last-suspend.
func TestFailedSuspendRestoresPrevious(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &failingSys{fakeSys{boot: now.Add(-5 * time.Hour)}}
	c := newChecker(t, &sys.fakeSys, &now)
	c.Sys = sys
	prev := now.Add(-3 * time.Hour)
	c.writeTime("last-suspend", prev)
	c.writeTime("last-wake", prev.Add(time.Minute))
	c.Check()
	if got := c.loadState().LastSuspend; !got.Equal(prev) {
		t.Errorf("last-suspend is %v, want %v", got, prev)
	}
}

func TestParseConfigInlineComment(t *testing.T) {
	cfg, err := ParseConfig("IDLE_MIN=20 # minutes\nWAKE_TIMERS=\"a.timer b.timer\" # backups\n")
	if err != nil || cfg.IdleMin != 20 || len(cfg.WakeTimers) != 2 {
		t.Errorf("got %+v, %v", cfg, err)
	}
}

// timeoutSys reports an error from Suspend, as systemctl does when it times
// out, and bumps the kernel's counter as a real sleep would.
type timeoutSys struct {
	fakeSys
	count      int
	slept      bool
	sleepCount bool
}

func (f *timeoutSys) ReadFile(p string) (string, error) {
	if p == "/stats" && f.count >= 0 {
		return strconv.Itoa(f.count) + "\n", nil
	}
	return f.fakeSys.ReadFile(p)
}
func (f *timeoutSys) Suspend() error {
	f.suspends++
	if f.sleepCount {
		f.count++
	}
	return errors.New("timed out")
}
func (f *timeoutSys) Slept() bool { return f.slept }

func timeoutCheck(t *testing.T, sys *timeoutSys) *Checker {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys.boot = now.Add(-5 * time.Hour)
	c := newChecker(t, &sys.fakeSys, &now)
	c.Sys, c.StatsPath = sys, "/stats"
	return c
}

// A timeout after a real sleep (counter went up) keeps the record.
func TestTimeoutAfterSleepKeepsRecord(t *testing.T) {
	sys := &timeoutSys{count: 4, sleepCount: true}
	c := timeoutCheck(t, sys)
	if _, err := c.Check(); err != nil {
		t.Fatalf("a real sleep is not a failure: %v", err)
	}
	if c.loadState().LastSuspend.IsZero() {
		t.Error("last-suspend was rolled back after a real sleep")
	}
	if !strings.Contains(readLog(t, c), "did sleep") {
		t.Errorf("log lacks the note:\n%s", readLog(t, c))
	}
}

// A timeout with no sign of sleep rolls back, counter or clocks.
func TestTimeoutWithoutSleepRollsBack(t *testing.T) {
	for name, sys := range map[string]*timeoutSys{
		"counter":     {count: 4},
		"no counter":  {count: -1},
		"clocks only": {count: -1, slept: false},
	} {
		c := timeoutCheck(t, sys)
		if _, err := c.Check(); err == nil {
			t.Errorf("%s: want the error", name)
		}
		if !c.loadState().LastSuspend.IsZero() {
			t.Errorf("%s: last-suspend left behind", name)
		}
	}
}

// Without the counter, the clock comparison decides.
func TestTimeoutClockFallbackKeepsRecord(t *testing.T) {
	sys := &timeoutSys{count: -1, slept: true}
	c := timeoutCheck(t, sys)
	if _, err := c.Check(); err != nil || c.loadState().LastSuspend.IsZero() {
		t.Errorf("want the record kept, got err %v", err)
	}
}

// A normal suspend is unchanged: record kept, no extra log line.
func TestNormalSuspendUnchanged(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sys := &fakeSys{boot: now.Add(-5 * time.Hour)}
	c := newChecker(t, sys, &now)
	if _, err := c.Check(); err != nil || c.loadState().LastSuspend.IsZero() || strings.Contains(readLog(t, c), "did sleep") {
		t.Errorf("err %v, log:\n%s", err, readLog(t, c))
	}
}
