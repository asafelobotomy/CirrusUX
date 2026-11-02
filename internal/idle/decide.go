// Package idle is CirrusUX's sleep manager (PHASES-4-6 §4c): once a minute
// it looks for activity, and suspends the server after IDLE_MIN quiet
// minutes. It writes the state files and log the agent already reads, so
// the panel shows it like any idle checker.
package idle

import (
	"fmt"
	"strings"
	"time"
)

// Config is /etc/cirrusux/sleep.env. The agent reads the same file for
// IDLE_MIN, GRACE_MIN and WAKE_TIMERS.
type Config struct {
	IdleMin  int // quiet minutes before suspending
	GraceMin int // minutes after a wake before it may sleep again
	// WatchPorts limits which local ports count; empty counts every port
	// something listens on.
	WatchPorts []int
	// IgnoreClients are addresses or networks whose connections don't count
	// (a TV that holds a connection open, a monitoring box).
	IgnoreClients []string
	// BusyUnits keep the server awake while active; globs allowed.
	BusyUnits []string
	// WakeTimers are the timers that wake it (for the SUSPEND line).
	WakeTimers []string
	// MaxLoad: a 5-minute load average above this counts as activity; 0 off.
	MaxLoad float64
	// CountLogins: logged-in sessions count as activity.
	CountLogins bool
}

func DefaultConfig() Config {
	return Config{
		IdleMin: 30, GraceMin: 15, CountLogins: true,
		BusyUnits:  []string{"cirrusux-backup.service", "cirrusux-av-scan.service", "cirrusux-job-*"},
		WakeTimers: []string{"cirrusux-backup.timer", "cirrusux-av-scan.timer"},
	}
}

// State is what the checker remembers between runs (its state folder).
type State struct {
	LastActive  time.Time // last minute anything counted
	LastWake    time.Time // last resume
	LastSuspend time.Time // last time it suspended
	LastMsg     string    // last verdict
}

// Hold is a stay-awake hold: none, until a time, or until released.
type Hold struct {
	Until   time.Time
	Forever bool
}

func (h Hold) Active(now time.Time) bool { return h.Forever || h.Until.After(now) }

type Verdict int

const (
	Active Verdict = iota
	Idle
	Suspend
)

// Decision is one minute's verdict.
type Decision struct {
	Verdict Verdict
	// Msg goes in the last-msg file: "ACTIVE: …" or "IDLE: …".
	Msg string
	// SleepsAt is when it will suspend if nothing happens (Idle only).
	SleepsAt time.Time
}

// Decide is the whole policy, kept pure so it can be tested by table.
// boot stands in for the last wake when the server hasn't slept yet.
func Decide(now time.Time, cfg Config, st State, boot time.Time, reasons []string, hold Hold) Decision {
	if hold.Active(now) {
		r := "stay-awake hold until you allow sleep"
		if !hold.Forever {
			r = "stay-awake hold until " + hold.Until.Format("15:04")
		}
		reasons = append([]string{r}, reasons...)
	}
	if len(reasons) > 0 {
		return Decision{Verdict: Active, Msg: "ACTIVE: " + strings.Join(reasons, "; ")}
	}

	woke := st.LastWake
	if boot.After(woke) {
		woke = boot
	}
	quietSince := st.LastActive
	if woke.After(quietSince) {
		quietSince = woke
	}
	at := quietSince.Add(time.Duration(cfg.IdleMin) * time.Minute)
	if graceEnd := woke.Add(time.Duration(cfg.GraceMin) * time.Minute); graceEnd.After(at) {
		at = graceEnd
	}
	if !now.Before(at) {
		return Decision{Verdict: Suspend, Msg: fmt.Sprintf("IDLE: nothing active for %d min", int(now.Sub(quietSince).Minutes()))}
	}
	return Decision{Verdict: Idle, Msg: "IDLE: nothing active, sleeps at " + at.Format("15:04"), SleepsAt: at}
}
