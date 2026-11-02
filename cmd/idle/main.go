// cirrusux-idle is CirrusUX's sleep manager (PHASES-4-6 §4c). A timer runs
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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/idle"
	"cirrusux/internal/version"
)

const (
	configPath = "/etc/cirrusux/sleep.env"
	stateDir   = "/run/cirrusux-idle"
	logPath    = "/var/log/cirrusux-idle.log"
)

func main() {
	args := os.Args[1:]
	switch filepath.Base(os.Args[0]) {
	case "cirrusux-stay-awake":
		args = append([]string{"hold"}, args...)
	case "cirrusux-allow-sleep":
		args = append([]string{"release"}, args...)
	}
	if len(args) == 0 {
		usage()
	}

	var cfg idle.Config
	b, err := os.ReadFile(configPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		cfg = idle.DefaultConfig()
	case err != nil:
		fail(err)
	default:
		if cfg, err = idle.ParseConfig(string(b)); err != nil {
			fail(fmt.Errorf("%s: %v", configPath, err))
		}
	}
	err = nil // a missing config was fine: the defaults apply
	c := &idle.Checker{
		Cfg: cfg, Sys: realSys{}, Now: time.Now, Dir: stateDir, Log: logPath,
		Out: func(s string) { fmt.Println(s) },
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
				fail(fmt.Errorf("hold takes minutes from 1 to 10080, or nothing for until release"))
			}
		}
		err = c.Hold(minutes)
	case "release":
		err = c.Release()
	case "sleep-hook":
		if len(args) < 2 {
			usage()
		}
		err = c.SleepHook(args[1])
	case "version", "-version", "--version":
		fmt.Println("cirrusux-idle", version.String())
	default:
		usage()
	}
	if err != nil {
		fail(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cirrusux-idle check [--dry-run] | status | hold [MINUTES] | release | sleep-hook pre|post")
	os.Exit(64)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "cirrusux-idle:", err)
	os.Exit(1)
}

// realSys asks the machine itself.
type realSys struct{}

func (realSys) Run(name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", idle.ErrNotInstalled
	}
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

func (realSys) ReadFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func (realSys) Suspend() error {
	return exec.Command("systemctl", "suspend").Run()
}

func (realSys) BootTime() time.Time {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
	}
	return time.Time{}
}
