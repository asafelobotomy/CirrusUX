// cirrusux-idle is CirrusUX's sleep manager (docs/history/PHASES-4-6 §4c). A timer runs
// `cirrusux-idle check` every minute; it suspends the server after
// IDLE_MIN quiet minutes. Runs as root.
//
//	cirrusux-idle check [--dry-run]   one check (--dry-run writes nothing)
//	cirrusux-idle status              what it sees now, without changing anything
//	cirrusux-idle hold [MINUTES]      stay awake that long, or until release
//	cirrusux-idle release             allow sleep again
//	cirrusux-idle sleep-hook pre|post called by systemd around a suspend
//
// Installed under the names cirrusux-stay-awake and cirrusux-allow-sleep
// too, it behaves like the stay-awake/allow-sleep scripts the panel's
// helper runs: `cirrusux-stay-awake [MINUTES]`, `cirrusux-allow-sleep`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/execx"
	"cirrusux/internal/idle"
	"cirrusux/internal/sysinfo"
	"cirrusux/internal/version"
)

const (
	configPath = "/etc/cirrusux/sleep.env"
	stateDir   = "/run/cirrusux-idle"
	logPath    = "/var/log/cirrusux-idle.log"
)

// cmdTimeout limits every command the checker runs: a hung conntrack, ss or
// systemctl must not stall the per-minute oneshot (and with it, sleep) forever.
const cmdTimeout = 20 * time.Second

// errUsage is returned for a command line the program doesn't understand.
var errUsage = errors.New("usage: cirrusux-idle check [--dry-run] | status | hold [MINUTES] | release | sleep-hook pre|post")

func main() {
	args := os.Args[1:]
	switch filepath.Base(os.Args[0]) {
	case "cirrusux-stay-awake":
		args = append([]string{"hold"}, args...)
	case "cirrusux-allow-sleep":
		args = append([]string{"release"}, args...)
	}
	err := run(args, &realSys{}, os.Stdout, configPath, stateDir, logPath)
	if errors.Is(err, errUsage) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cirrusux-idle:", err)
		os.Exit(1)
	}
}

// run does what args ask, with sys standing in for the machine.
func run(args []string, sys idle.System, out io.Writer, cfgFile, dir, log string) error {
	if len(args) == 0 {
		return errUsage
	}
	cfg := idle.DefaultConfig()
	b, err := os.ReadFile(cfgFile)
	switch {
	case errors.Is(err, os.ErrNotExist): // the defaults apply
	case err != nil:
		return err
	default:
		if cfg, err = idle.ParseConfig(string(b)); err != nil {
			return fmt.Errorf("%s: %v", cfgFile, err)
		}
	}
	err = nil // a missing config was fine
	c := &idle.Checker{
		Cfg: cfg, Sys: sys, Now: time.Now, Dir: dir, Log: log,
		Out: func(s string) { fmt.Fprintln(out, s) },
	}

	switch args[0] {
	case "check":
		c.DryRun = len(args) > 1 && args[1] == "--dry-run"
		_, err = c.Check()
	case "status":
		c.DryRun = true
		_, err = c.Check()
	case "hold":
		minutes := 0
		if len(args) > 1 {
			if minutes, err = strconv.Atoi(args[1]); err != nil || minutes < 1 || minutes > 7*24*60 {
				return errors.New("hold takes minutes from 1 to 10080, or nothing for until release")
			}
		}
		err = c.Hold(minutes)
	case "release":
		err = c.Release()
	case "sleep-hook":
		if len(args) < 2 {
			return errUsage
		}
		err = c.SleepHook(args[1])
	case "version", "-version", "--version":
		fmt.Fprintln(out, "cirrusux-idle", version.String())
	default:
		return errUsage
	}
	return err
}

func (realSys) Run(name string, args ...string) (string, error) {
	if _, err := lookPath(name); err != nil {
		return "", idle.ErrNotInstalled
	}
	out, err := execx.Run(context.Background(), execx.Options{Timeout: cmdTimeout}, name, args...)
	return string(out), err
}

func (realSys) ReadFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

// realSys asks the machine itself. It also remembers how the clocks moved during Suspend: an uptime that
// grew more than the monotonic timer means the machine slept.
type realSys struct{ slept bool }

func (r *realSys) Suspend() error {
	up0, ok0 := procUptime()
	t0 := time.Now() // carries a monotonic reading, which stops in sleep
	_, err := execx.Run(context.Background(), execx.Options{Timeout: cmdTimeout, Combined: true}, "systemctl", "suspend")
	up1, ok1 := procUptime()
	r.slept = ok0 && ok1 && (up1-up0)-time.Since(t0) > 5*time.Second
	return err
}

// Slept reports whether the last Suspend call spanned a real sleep.
func (r *realSys) Slept() bool { return r.slept }

// procUptime is /proc/uptime, which is CLOCK_BOOTTIME and so counts sleep.
func procUptime() (time.Duration, bool) {
	b, err := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if err != nil || len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return time.Duration(v * float64(time.Second)), err == nil
}

func (realSys) BootTime() time.Time { return sysinfo.BootTime() }

// lookPath is exec.LookPath; a variable so a test can check the timeout path.
var lookPath = exec.LookPath
