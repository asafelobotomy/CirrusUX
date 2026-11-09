package idle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/fsutil"
)

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
	return State{LastActive: read("last-active"), LastWake: read("last-wake"), LastSuspend: read("last-suspend"),
		LastIgnored: read("last-ignored"), LastMsg: strings.TrimSpace(string(msg))}
}

func (c *Checker) write(name, content string) error {
	return fsutil.WriteFile(filepath.Join(c.Dir, name), []byte(content+"\n"), 0o644)
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
