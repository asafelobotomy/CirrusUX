// cirrusux-helper runs as root through one sudoers rule per command line.
// Most subcommands only read; a few are fixed actions (stay awake, allow
// sleep, run backup). Nothing takes a path or free text from the caller:
// paths come from root's own config file (config.go), and nothing prints
// secrets.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/version"
)

const (
	maxLines    = 400
	maxStateLen = 4096
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("cirrusux-helper", version.String())
		return
	}
	if len(os.Args) == 3 && os.Args[1] == helperapi.CmdStayAwake && slices.Contains(helperapi.StayAwakeArgs, os.Args[2]) {
		// the one subcommand with an argument, from a fixed list
	} else if len(os.Args) != 2 {
		fail("usage: cirrusux-helper <%s>", strings.Join(helperapi.Commands, "|"))
	}
	c, err := loadAll(configPath, confDir)
	if err != nil {
		fail("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFor(os.Args[1]))
	defer cancel()

	var out any
	sub := os.Args[1]
	need := func(mod string) {
		if !slices.Contains(c.features().Modules, mod) {
			fail("%s: the %s module isn't set up in %s", sub, mod, configPath)
		}
	}
	switch sub {
	case helperapi.CmdFeatures:
		out = c.features()
	case helperapi.CmdIdleStatus:
		need(helperapi.ModSleep)
		out, err = idleStatus(c)
	case helperapi.CmdIdleLog:
		need(helperapi.ModSleep)
		out, err = tailFile(c.IdleLog, maxLines)
	case helperapi.CmdBackupList:
		need(helperapi.ModBackup)
		out, err = backupList(c.BackupDir, c.BackupPrefix)
	case helperapi.CmdBackupLast:
		need(helperapi.ModBackup)
		var l helperapi.Lines
		l, err = command(ctx, "journalctl", "-u", c.BackupUnit, "-n", "60", "-o", "short-iso", "--no-pager")
		out = helperapi.BackupLast{Unit: c.BackupUnit, Lines: l.Lines}
	case helperapi.CmdAVLast:
		need(helperapi.ModAV)
		out, err = tailFile(c.AVLog, 80)
	case helperapi.CmdSecurityStatus:
		need(helperapi.ModSecurity)
		out = securityStatus(ctx, c)
	case helperapi.CmdStayAwake:
		need(helperapi.ModStayAwake)
		if len(os.Args) != 3 {
			fail("stay-awake needs one of %v", helperapi.StayAwakeArgs)
		}
		out, err = holdAwake(ctx, c, os.Args[2])
	case helperapi.CmdAllowSleep:
		need(helperapi.ModStayAwake)
		out, err = run(ctx, c.AllowSleepBin)
	case helperapi.CmdNudgeAwake:
		need(helperapi.ModStayAwake)
		out, err = nudgeAwake(ctx, c)
	case helperapi.CmdBackupRun:
		need(helperapi.ModBackup)
		out, err = run(ctx, "systemctl", "start", "--no-block", c.BackupUnit)
	case helperapi.CmdEgressReport:
		need(helperapi.ModEgress)
		out, err = run(ctx, c.EgressReport, "-n", "20")
	default:
		fail("unknown subcommand %q", sub)
	}
	if err != nil {
		fail("%s: %v", os.Args[1], err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fail("encode: %v", err)
	}
}

// timeoutFor gives the slow egress report longer than everything else. The
// agent waits a little longer than this for each subcommand.
func timeoutFor(sub string) time.Duration {
	if sub == helperapi.CmdEgressReport {
		return 150 * time.Second
	}
	return 30 * time.Second
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "cirrusux-helper: "+format+"\n", a...)
	os.Exit(1)
}

func idleStatus(c conf) (helperapi.IdleStatus, error) {
	st := helperapi.IdleStatus{State: map[string]string{}}
	if c.IdleStateDir == "" {
		return st, nil
	}
	entries, err := os.ReadDir(c.IdleStateDir)
	if err != nil {
		return st, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.IdleStateDir, e.Name()))
		if err != nil {
			continue
		}
		st.State[e.Name()] = strings.TrimSpace(truncate(string(b), maxStateLen))
	}
	if c.StayAwakeFile != "" {
		if b, err := os.ReadFile(c.StayAwakeFile); err == nil {
			s := strings.TrimSpace(string(b))
			st.StayAwake = &s
		}
	}
	return st, nil
}

func tailFile(path string, n int) (helperapi.Lines, error) {
	f, err := os.Open(path)
	if err != nil {
		return helperapi.Lines{}, err
	}
	defer f.Close()
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return helperapi.Lines{Lines: ring}, sc.Err()
}

func backupList(dir, prefix string) (helperapi.BackupList, error) {
	var l helperapi.BackupList
	if dir == "" {
		return l, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return l, err
	}
	for _, e := range entries {
		// Finished archives only; .partial files are in progress.
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), prefix) || strings.HasSuffix(e.Name(), ".partial") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		l.Files = append(l.Files, helperapi.BackupFile{Name: e.Name(), MTime: info.ModTime().Unix(), Bytes: info.Size()})
	}
	return l, nil
}

func command(ctx context.Context, name string, args ...string) (helperapi.Lines, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return helperapi.Lines{}, err
	}
	return helperapi.Lines{Lines: strings.Split(strings.TrimRight(string(out), "\n"), "\n")}, nil
}

var bannedRe = regexp.MustCompile(`Currently banned:\s*(\d+)`)

func securityStatus(ctx context.Context, c conf) helperapi.SecurityStatus {
	s := helperapi.SecurityStatus{Firewall: []helperapi.FirewallCheck{}}
	if c.Fail2banJail != "" {
		s.Fail2ban = true
		if out, err := exec.CommandContext(ctx, "fail2ban-client", "status", c.Fail2banJail).Output(); err == nil {
			if m := bannedRe.FindSubmatch(out); m != nil {
				s.Banned, _ = strconv.Atoi(string(m[1]))
			}
		} else {
			s.Errors = append(s.Errors, "fail2ban: "+err.Error())
		}
	}
	for _, fc := range c.FirewallChecks {
		argv := fc.chainCommand()
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
		if err != nil {
			s.Errors = append(s.Errors, fc.Chain+": "+err.Error())
		}
		s.Firewall = append(s.Firewall, helperapi.FirewallCheck{Chain: fc.Chain, OK: err == nil && strings.Contains(string(out), fc.Pattern)})
	}
	return s
}

// holdAwake runs the existing stay-awake script: minutes, or no argument
// for "until allow-sleep".
func holdAwake(ctx context.Context, c conf, arg string) (helperapi.Result, error) {
	if arg == "forever" {
		return run(ctx, c.StayAwakeBin)
	}
	return run(ctx, c.StayAwakeBin, arg)
}

// nudgeAwake keeps the server up for at least 10 minutes after an action
// (decision D3) without shortening a hold that is already longer.
func nudgeAwake(ctx context.Context, c conf) (helperapi.Result, error) {
	if b, err := os.ReadFile(c.StayAwakeFile); err == nil {
		s := strings.TrimSpace(string(b))
		if s == "" {
			return helperapi.Result{Output: []string{"held until allow-sleep; unchanged"}}, nil
		}
		if n, err := strconv.ParseInt(strings.Fields(s)[0], 10, 64); err == nil && time.Unix(n, 0).After(time.Now().Add(10*time.Minute)) {
			return helperapi.Result{Output: []string{"longer hold in place; unchanged"}}, nil
		}
	}
	return run(ctx, c.StayAwakeBin, "10")
}

func run(ctx context.Context, name string, args ...string) (helperapi.Result, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	if err != nil {
		return helperapi.Result{Output: lines}, fmt.Errorf("%v: %s", err, strings.Join(lines, " / "))
	}
	return helperapi.Result{Output: lines}, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
