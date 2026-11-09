package idle

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"cirrusux/internal/sysinfo"
)

// System is everything the checker asks the machine. Tests replace it.
type System interface {
	Run(name string, args ...string) (string, error)
	ReadFile(path string) (string, error)
	Suspend() error
	BootTime() time.Time
}

// ErrNotInstalled from Run means the command doesn't exist here.
var ErrNotInstalled = errors.New("not installed")

// Checker runs one check, holds and releases, and records sleep and wake.
type Checker struct {
	Cfg Config
	Sys System
	Now func() time.Time
	// Dir holds the state files (last-msg, last-active, last-wake,
	// last-suspend, hold); Log is the line-per-event log the agent reads.
	Dir, Log string
	// DryRun decides and says what it would do, writing nothing at all.
	// StatsPath overrides SuspendStatsPath (tests).
	StatsPath string
	DryRun    bool
	Out       func(string) // where DryRun and status print
}

// Check is one minute's run: gather activity, decide, record, maybe suspend.
func (c *Checker) Check() (Decision, error) {
	now := c.Now()
	st := c.loadState()
	if st.LastSuspend.After(st.LastWake) && now.Sub(st.LastSuspend) > 2*time.Minute {
		// It suspended and came back without the sleep hook noting it.
		if !c.DryRun {
			c.logLine(now, "RESUMED (not noted by the sleep hook)")
			c.writeTime("last-wake", now)
		}
		st.LastWake = now
	}

	reasons, sawIgnored := c.gather(now)
	if sawIgnored {
		st.LastIgnored = now
		if !c.DryRun {
			c.writeTime("last-ignored", now)
		}
	}
	d := Decide(now, c.Cfg, st, c.Sys.BootTime(), reasons, c.hold(now))

	if c.DryRun {
		c.say(d.Msg)
		if d.Verdict == Suspend {
			c.say("would suspend now" + c.wakeNote(now))
		}
		return d, nil
	}

	if d.Verdict == Active {
		c.writeTime("last-active", now)
		if d.Msg != st.LastMsg {
			c.logLine(now, d.Msg)
		}
	} else if strings.HasPrefix(st.LastMsg, "ACTIVE") || st.LastMsg == "" {
		c.logLine(now, "IDLE: nothing active, timer started")
	}
	if err := c.write("last-msg", d.Msg); err != nil {
		return d, err
	}
	if d.Verdict == Suspend {
		return d, c.suspend(now, st)
	}
	return d, nil
}

// gather lists what's keeping the server awake, and whether an ignored
// client is connected. A source that can't be read counts as activity:
// better awake than asleep by mistake.
func (c *Checker) gather(now time.Time) ([]string, bool) {
	var reasons []string
	if c.Cfg.CountLogins {
		if out, err := c.Sys.Run("who"); err != nil {
			reasons = append(reasons, "can't read logins ("+err.Error()+")")
		} else if n := ParseWho(out); n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d login session(s)", n))
		}
	}
	if len(c.Cfg.BusyUnits) > 0 {
		out, err := c.Sys.Run("systemctl", "list-units", "--type=service", "--state=active", "--no-legend", "--plain")
		if err != nil {
			reasons = append(reasons, "can't read running services")
		}
		for _, u := range ParseActiveUnits(out, c.Cfg.BusyUnits) {
			reasons = append(reasons, "running "+u)
		}
	}
	if c.Cfg.MaxLoad > 0 {
		if s, err := c.Sys.ReadFile("/proc/loadavg"); err == nil {
			if l := ParseLoad(s); l > c.Cfg.MaxLoad {
				reasons = append(reasons, fmt.Sprintf("load %.2f", l))
			}
		}
	}

	est, err1 := c.Sys.Run("ss", "-Htn", "state", "established")
	lis, err2 := c.Sys.Run("ss", "-Htln")
	if err1 != nil || err2 != nil {
		return append(reasons, "can't read connections (ss)"), false
	}
	conns := ParseSS(est, lis)
	// Each family on its own: tailnet clients may come over IPv6. IPv4
	// failing counts as activity; a host without IPv6 tracking just has none.
	var tracked string
	if out, err := c.Sys.Run("conntrack", "-L", "-f", "ipv4", "-p", "tcp", "--state", "ESTABLISHED"); err == nil {
		tracked = out
		if out6, err := c.Sys.Run("conntrack", "-L", "-f", "ipv6", "-p", "tcp", "--state", "ESTABLISHED"); err == nil {
			tracked += "\n" + out6
		}
		conns = append(conns, ParseConntrack(tracked)...)
	} else if !errors.Is(err, ErrNotInstalled) {
		reasons = append(reasons, "can't read forwarded connections (conntrack)")
	}
	if r := ConnReason(Counted(conns, c.Cfg)); r != "" {
		reasons = append(reasons, r)
	}
	reasons = append(reasons, c.playback(now, ParseFlows(tracked))...)
	return reasons, len(IgnoredPeers(conns, c.Cfg)) > 0
}

// wakeNote is the SUSPEND line's "; RTC wake set …" part: the next of
// WAKE_TIMERS, which systemd wakes the machine for.
func (c *Checker) wakeNote(now time.Time) string {
	if len(c.Cfg.WakeTimers) == 0 {
		return ""
	}
	out, err := c.Sys.Run("systemctl", sysinfo.ListTimersArgs...)
	if err != nil {
		return ""
	}
	timers, err := sysinfo.ParseTimers(out)
	if err != nil {
		return ""
	}
	var next time.Time // timers come soonest first
	for _, t := range timers {
		if t.Next.After(now) && slices.Contains(c.Cfg.WakeTimers, t.Unit) {
			next = t.Next
			break
		}
	}
	if next.IsZero() {
		return ""
	}
	return "; RTC wake set " + next.In(time.Local).Format("Mon 2006-01-02 15:04:05 MST")
}

func maxTime(ts ...time.Time) time.Time {
	var m time.Time
	for _, t := range ts {
		if t.After(m) {
			m = t
		}
	}
	return m
}
