package idle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
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
	DryRun bool
	Out    func(string) // where DryRun and status print
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

	reasons := c.gather()
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
		c.writeTime("last-suspend", now)
		c.logLine(now, fmt.Sprintf("SUSPEND: idle %ds%s", int(now.Sub(maxTime(st.LastActive, st.LastWake, c.Sys.BootTime())).Seconds()), c.wakeNote(now)))
		return d, c.Sys.Suspend()
	}
	return d, nil
}

// gather lists what's keeping the server awake. A source that can't be
// read counts as activity: better awake than asleep by mistake.
func (c *Checker) gather() []string {
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
		return append(reasons, "can't read connections (ss)")
	}
	conns := ParseSS(est, lis)
	if out, err := c.Sys.Run("conntrack", "-L", "-p", "tcp", "--state", "ESTABLISHED"); err == nil {
		conns = append(conns, ParseConntrack(out)...)
	} else if !errors.Is(err, ErrNotInstalled) {
		reasons = append(reasons, "can't read forwarded connections (conntrack)")
	}
	if r := ConnReason(Counted(conns, c.Cfg)); r != "" {
		reasons = append(reasons, r)
	}
	return reasons
}

// wakeNote is the SUSPEND line's "; RTC wake set …" part: the next of
// WAKE_TIMERS, which systemd wakes the machine for.
func (c *Checker) wakeNote(now time.Time) string {
	if len(c.Cfg.WakeTimers) == 0 {
		return ""
	}
	out, err := c.Sys.Run("systemctl", "list-timers", "--all", "--output=json")
	if err != nil {
		return ""
	}
	var timers []struct {
		Next int64  `json:"next"`
		Unit string `json:"unit"`
	}
	if json.Unmarshal([]byte(out), &timers) != nil {
		return ""
	}
	var next time.Time
	for _, t := range timers {
		at := time.UnixMicro(t.Next)
		if t.Next > 0 && at.After(now) && slices.Contains(c.Cfg.WakeTimers, t.Unit) && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	if next.IsZero() {
		return ""
	}
	return "; RTC wake set " + next.In(time.Local).Format("Mon 2006-01-02 15:04:05 MST")
}

// ---- holds ----

// Hold keeps the server awake for minutes, or until Release when minutes is 0.
func (c *Checker) Hold(minutes int) error {
	content := ""
	if minutes > 0 {
		content = strconv.FormatInt(c.Now().Add(time.Duration(minutes)*time.Minute).Unix(), 10)
	}
	if err := c.write("hold", content); err != nil {
		return err
	}
	if minutes > 0 {
		c.say("staying awake until " + c.Now().Add(time.Duration(minutes)*time.Minute).Format("15:04"))
	} else {
		c.say("staying awake until allow-sleep")
	}
	return nil
}

// Release removes the hold; the idle timer applies again.
func (c *Checker) Release() error {
	err := os.Remove(filepath.Join(c.Dir, "hold"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	c.say("sleep allowed again")
	return nil
}

// hold reads the hold file: an epoch, or empty for "until released". It's
// the same format as the stay-awake file the helper and agent already know.
func (c *Checker) hold(now time.Time) Hold {
	b, err := os.ReadFile(filepath.Join(c.Dir, "hold"))
	if err != nil {
		return Hold{}
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return Hold{Forever: true}
	}
	n, err := strconv.ParseInt(strings.Fields(s)[0], 10, 64)
	if err != nil {
		return Hold{Forever: true} // unreadable: err on the side of awake
	}
	return Hold{Until: time.Unix(n, 0)}
}

// ---- sleep hook ----

// SleepHook is called by systemd around every suspend (system-sleep/):
// "pre" before, "post" after. It records suspends this checker didn't start
// and every resume, with the wake-up interrupt when the kernel knows it.
func (c *Checker) SleepHook(phase string) error {
	now := c.Now()
	switch phase {
	case "pre":
		if st := c.loadState(); now.Sub(st.LastSuspend) > time.Minute {
			c.writeTime("last-suspend", now)
			c.logLine(now, "SUSPEND: requested outside the idle check")
		}
	case "post":
		note := ""
		if irq, err := c.Sys.ReadFile("/sys/power/pm_wakeup_irq"); err == nil && strings.TrimSpace(irq) != "" {
			note = " (wakeup irq " + strings.TrimSpace(irq) + ")"
		}
		c.writeTime("last-wake", now)
		c.logLine(now, "RESUMED"+note)
	default:
		return fmt.Errorf("sleep hook: unknown phase %q", phase)
	}
	return nil
}

// ---- state ----

func (c *Checker) loadState() State {
	read := func(name string) time.Time {
		b, err := os.ReadFile(filepath.Join(c.Dir, name))
		if err != nil {
			return time.Time{}
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			return time.Time{}
		}
		return time.Unix(n, 0)
	}
	msg, _ := os.ReadFile(filepath.Join(c.Dir, "last-msg"))
	return State{LastActive: read("last-active"), LastWake: read("last-wake"), LastSuspend: read("last-suspend"), LastMsg: strings.TrimSpace(string(msg))}
}

func (c *Checker) write(name, content string) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(c.Dir, "."+name)
	if err := os.WriteFile(tmp, []byte(content+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(c.Dir, name))
}

func (c *Checker) writeTime(name string, t time.Time) {
	_ = c.write(name, strconv.FormatInt(t.Unix(), 10))
}

// logLine appends "2026-11-02T12:00:00+00:00 MSG": RFC 3339, so the agent
// reads the time without guessing the zone.
func (c *Checker) logLine(t time.Time, msg string) {
	f, err := os.OpenFile(c.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", t.In(time.Local).Format(time.RFC3339), msg)
}

func (c *Checker) say(s string) {
	if c.Out != nil {
		c.Out(s)
	}
}

// ---- config ----

// ParseConfig reads sleep.env: KEY=value lines, # comments, quotes allowed.
func ParseConfig(text string) (Config, error) {
	cfg := DefaultConfig()
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("line %d: want KEY=value", n)
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		var err error
		switch strings.TrimSpace(k) {
		case "IDLE_MIN":
			cfg.IdleMin, err = positive(v)
		case "GRACE_MIN":
			cfg.GraceMin, err = strconv.Atoi(v)
		case "WATCH_PORTS":
			cfg.WatchPorts = nil
			for _, f := range strings.Fields(v) {
				p, e := strconv.Atoi(f)
				if e != nil {
					err = e
				}
				cfg.WatchPorts = append(cfg.WatchPorts, p)
			}
		case "IGNORE_CLIENTS":
			cfg.IgnoreClients = strings.Fields(v)
		case "BUSY_UNITS":
			cfg.BusyUnits = strings.Fields(v)
		case "WAKE_TIMERS":
			cfg.WakeTimers = strings.Fields(v)
		case "MAX_LOAD":
			cfg.MaxLoad, err = strconv.ParseFloat(v, 64)
		case "COUNT_LOGINS":
			cfg.CountLogins = v != "no"
		default:
			err = fmt.Errorf("unknown setting")
		}
		if err != nil {
			return cfg, fmt.Errorf("line %d: %s: %v", n, k, err)
		}
	}
	return cfg, sc.Err()
}

func positive(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err == nil && n < 1 {
		err = fmt.Errorf("must be at least 1")
	}
	return n, err
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
