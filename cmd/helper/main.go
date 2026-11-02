// cirrusux-helper runs as root through one sudoers rule per command line.
// Phase 1 subcommands only read; Phase 2 adds a few fixed actions (stay
// awake, allow sleep, run backup). Nothing takes a path or free text from
// the caller, and nothing prints secrets.
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
	idleRunDir    = "/run/cloud64-idle"
	stayAwake     = "/run/stay-awake"
	stayAwakeBin  = "/usr/local/bin/stay-awake"
	allowSleepBin = "/usr/local/bin/allow-sleep"
	egressReport  = "/usr/local/sbin/egress-report.sh"
	idleLog       = "/var/log/cloud64-idle.log"
	backupDir     = "/mnt/media/BackUp/cloud64-system"
	avLog         = "/var/log/clamav/weekly-scan.log"
	maxLines      = 400
	maxStateLen   = 4096
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
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFor(os.Args[1]))
	defer cancel()

	var out any
	var err error
	switch os.Args[1] {
	case helperapi.CmdIdleStatus:
		out, err = idleStatus()
	case helperapi.CmdIdleLog:
		out, err = tailFile(idleLog, maxLines)
	case helperapi.CmdBackupList:
		out, err = backupList(backupDir)
	case helperapi.CmdBackupLast:
		out, err = command(ctx, "journalctl", "-u", "cloud64-backup.service", "-n", "60", "-o", "short-iso", "--no-pager")
	case helperapi.CmdAVLast:
		out, err = tailFile(avLog, 80)
	case helperapi.CmdSecurityStatus:
		out = securityStatus(ctx)
	case helperapi.CmdStayAwake:
		if len(os.Args) != 3 {
			fail("stay-awake needs one of %v", helperapi.StayAwakeArgs)
		}
		out, err = holdAwake(ctx, os.Args[2])
	case helperapi.CmdAllowSleep:
		out, err = run(ctx, allowSleepBin)
	case helperapi.CmdNudgeAwake:
		out, err = nudgeAwake(ctx)
	case helperapi.CmdBackupRun:
		out, err = run(ctx, "systemctl", "start", "--no-block", "cloud64-backup.service")
	case helperapi.CmdEgressReport:
		out, err = run(ctx, egressReport, "-n", "20")
	default:
		fail("unknown subcommand %q", os.Args[1])
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

func idleStatus() (helperapi.IdleStatus, error) {
	st := helperapi.IdleStatus{State: map[string]string{}}
	entries, err := os.ReadDir(idleRunDir)
	if err != nil {
		return st, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(idleRunDir, e.Name()))
		if err != nil {
			continue
		}
		st.State[e.Name()] = strings.TrimSpace(truncate(string(b), maxStateLen))
	}
	if b, err := os.ReadFile(stayAwake); err == nil {
		s := strings.TrimSpace(string(b))
		st.StayAwake = &s
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

func backupList(dir string) (helperapi.BackupList, error) {
	var l helperapi.BackupList
	entries, err := os.ReadDir(dir)
	if err != nil {
		return l, err
	}
	for _, e := range entries {
		// Finished archives only; .partial files are in progress.
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), "cloud64-system-") || strings.HasSuffix(e.Name(), ".partial") {
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

func securityStatus(ctx context.Context) helperapi.SecurityStatus {
	var s helperapi.SecurityStatus
	if out, err := exec.CommandContext(ctx, "fail2ban-client", "status", "sshd").Output(); err == nil {
		if m := bannedRe.FindSubmatch(out); m != nil {
			s.Banned, _ = strconv.Atoi(string(m[1]))
		}
	} else {
		s.Errors = append(s.Errors, "fail2ban: "+err.Error())
	}
	if out, err := exec.CommandContext(ctx, "iptables", "-S", "DOCKER-USER").Output(); err == nil {
		s.Lockdown = strings.Contains(string(out), "LOCKDOWN-PUBLISHED")
	} else {
		s.Errors = append(s.Errors, "DOCKER-USER: "+err.Error())
	}
	if out, err := exec.CommandContext(ctx, "iptables", "-S", "1PANEL_BASIC_AFTER").Output(); err == nil {
		s.PanelDrop = strings.Contains(string(out), "DROP")
	} else {
		s.Errors = append(s.Errors, "1PANEL_BASIC_AFTER: "+err.Error())
	}
	return s
}

// holdAwake runs the existing stay-awake script: minutes, or no argument
// for "until allow-sleep".
func holdAwake(ctx context.Context, arg string) (helperapi.Result, error) {
	if arg == "forever" {
		return run(ctx, stayAwakeBin)
	}
	return run(ctx, stayAwakeBin, arg)
}

// nudgeAwake keeps cloud64 up for at least 10 minutes after an action
// (decision D3) without shortening a hold that is already longer.
func nudgeAwake(ctx context.Context) (helperapi.Result, error) {
	if b, err := os.ReadFile(stayAwake); err == nil {
		s := strings.TrimSpace(string(b))
		if s == "" {
			return helperapi.Result{Output: []string{"held until allow-sleep; unchanged"}}, nil
		}
		if n, err := strconv.ParseInt(strings.Fields(s)[0], 10, 64); err == nil && time.Unix(n, 0).After(time.Now().Add(10*time.Minute)) {
			return helperapi.Result{Output: []string{"longer hold in place; unchanged"}}, nil
		}
	}
	return run(ctx, stayAwakeBin, "10")
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
