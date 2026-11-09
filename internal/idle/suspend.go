package idle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SuspendStatsPath is the kernel's count of successful suspends. Anyone can
// read it, and it only goes up when the machine really went to sleep.
const SuspendStatsPath = "/sys/power/suspend_stats/success"

// Sleeper is an optional extra for a System: after Suspend returns, Slept
// says whether the clocks show the machine was asleep during that call
// (CLOCK_BOOTTIME kept counting, the monotonic clock did not). It is the
// fallback when the kernel's suspend counter can't be read.
type Sleeper interface {
	Slept() bool
}

// suspend logs the SUSPEND line and suspends. last-suspend is written
// before the machine goes down (the next check reads it after the resume).
// If the suspend call fails, the record is taken back, unless the machine
// really did sleep: systemctl can time out after the wake, and that sleep
// must stay on record.
func (c *Checker) suspend(now time.Time, st State) error {
	c.logLine(now, fmt.Sprintf("SUSPEND: idle %ds%s", int(now.Sub(maxTime(st.LastActive, st.LastWake, c.Sys.BootTime())).Seconds()), c.wakeNote(now)))
	c.writeTime("last-suspend", now)
	before, haveBefore := c.suspendCount()
	err := c.Sys.Suspend()
	if err == nil {
		return nil
	}
	if c.reallySlept(before, haveBefore) {
		c.logLine(c.Now(), "SUSPEND: the command reported an error ("+err.Error()+") but the machine did sleep; keeping the record")
		return nil
	}
	c.logLine(c.Now(), "SUSPEND failed: "+err.Error())
	if st.LastSuspend.IsZero() {
		_ = os.Remove(filepath.Join(c.Dir, "last-suspend"))
	} else {
		c.writeTime("last-suspend", st.LastSuspend)
	}
	return err
}

// suspendCount reads the kernel's successful-suspend counter.
func (c *Checker) suspendCount() (int, bool) {
	path := c.StatsPath
	if path == "" {
		path = SuspendStatsPath
	}
	s, err := c.Sys.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// reallySlept prefers the kernel counter (it went up) and falls back to the
// clock comparison when the counter isn't there.
func (c *Checker) reallySlept(before int, haveBefore bool) bool {
	if after, ok := c.suspendCount(); ok && haveBefore {
		return after > before
	}
	if s, ok := c.Sys.(Sleeper); ok {
		return s.Slept()
	}
	return false
}
