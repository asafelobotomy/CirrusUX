// cirrusux-helper runs as root through one sudoers rule per command line.
// Most subcommands only read; the rest are actions confirmed in the panel.
// The only things taken from the caller are a job id, a unit name, an
// archive entry's name, a stay-awake length and the app request file the
// job id names, each checked against a fixed pattern (validArgs) and again
// where it is used. Paths come from root's own config file (config.go) or
// are built from those checked names, and nothing prints secrets.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/execx"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/version"
)

const (
	maxLines    = 400
	maxStateLen = 4096
)

// rootPath is the PATH the helper runs tools from (as sudoers' secure_path).
const rootPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// argChecks are the subcommands that take arguments, each with how many and
// what they may be. Every other subcommand takes none.
var argChecks = map[string]func(a []string) bool{
	helperapi.CmdStayAwake:     func(a []string) bool { return len(a) == 1 && slices.Contains(helperapi.StayAwakeArgs, a[0]) },
	helperapi.CmdAppJob:        func(a []string) bool { return len(a) == 1 && helperapi.JobRe.MatchString(a[0]) },
	helperapi.CmdArchiveDelete: func(a []string) bool { return len(a) == 1 && entryRe.MatchString(a[0]) },
	helperapi.CmdJournal:       func(a []string) bool { return len(a) == 1 && helperapi.UnitRe.MatchString(a[0]) },
	// Started by systemd-run for a checked job; not in sudoers.
	helperapi.CmdAppRun: func(a []string) bool { return len(a) == 1 && helperapi.JobRe.MatchString(a[0]) },
	helperapi.CmdOSRun: func(a []string) bool {
		return len(a) == 2 && (a[0] == "check" || a[0] == "upgrade") && helperapi.JobRe.MatchString(a[1])
	},
	helperapi.CmdStorageRun: func(a []string) bool {
		return len(a) == 3 && (a[0] == "scan" || a[0] == "delete") && helperapi.JobRe.MatchString(a[1]) && entryRe.MatchString(a[2])
	},
}

// validArgs reports whether a command line (subcommand first) has the
// arguments its subcommand takes, so no subcommand reads past them.
func validArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if check, ok := argChecks[args[0]]; ok {
		return check(args[1:])
	}
	return len(args) == 1
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("cirrusux-helper", version.String())
		return
	}
	if !validArgs(os.Args[1:]) {
		fail("usage: cirrusux-helper <%s>", strings.Join(helperapi.Commands, "|"))
	}
	// Tools are run by name; find them only in root's own folders, whatever
	// PATH sudo was given.
	os.Setenv("PATH", rootPath)
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
	case helperapi.CmdAppsInfo:
		need(helperapi.ModApps)
		out, err = appsInfo(c)
	case helperapi.CmdAppJob:
		need(helperapi.ModApps)
		out, err = appJob(ctx, c, os.Args[2])
	case helperapi.CmdAppRun:
		need(helperapi.ModApps)
		out, err = appRun(c, os.Args[2])
	case helperapi.CmdOSCheck, helperapi.CmdOSUpgrade:
		need(helperapi.ModOS)
		out, err = osJob(ctx, strings.TrimPrefix(sub, "os-"))
	case helperapi.CmdOSRun:
		need(helperapi.ModOS)
		out, err = osRun(os.Args[2], os.Args[3])
	case helperapi.CmdStorageScan:
		need(helperapi.ModStorage)
		out, err = startJob(ctx, "scan", "-")
	case helperapi.CmdArchiveDelete:
		need(helperapi.ModStorage)
		if _, ok := archiveEntry(c, os.Args[2]); !ok {
			fail("%s: %q isn't in the archive", sub, os.Args[2])
		}
		out, err = startJob(ctx, "delete", os.Args[2])
	case helperapi.CmdStorageRun:
		need(helperapi.ModStorage)
		out, err = storageRun(c, os.Args[2], os.Args[3], os.Args[4])
	case helperapi.CmdJournal:
		out, err = command(ctx, "journalctl", "-u", os.Args[2], "-n", "300", "-o", "short-iso", "--no-pager")
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

// tailFile is the last n lines of a log. A log that doesn't exist yet (the
// first scan or check hasn't run) is empty, not an error.
func tailFile(path string, n int) (helperapi.Lines, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return helperapi.Lines{Lines: []string{}}, nil
	}
	if err != nil {
		return helperapi.Lines{}, err
	}
	defer f.Close()
	// Lines over 1 MB are skipped, not fatal: the rest of the log still reads.
	ring := make([]string, 0, n)
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			skipLine(r)
			continue
		}
		if len(line) > 0 {
			if len(ring) == n {
				ring = ring[1:]
			}
			ring = append(ring, strings.TrimRight(string(line), "\r\n"))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return helperapi.Lines{Lines: ring}, nil
			}
			return helperapi.Lines{Lines: ring}, err
		}
	}
}

// skipLine discards the rest of an over-long line.
func skipLine(r *bufio.Reader) {
	for {
		_, err := r.ReadSlice('\n')
		if !errors.Is(err, bufio.ErrBufferFull) {
			return
		}
	}
}

func backupList(dir, prefix string) (helperapi.BackupList, error) {
	var l helperapi.BackupList
	if dir == "" {
		return l, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil // made by the first backup
	}
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
	out, err := execx.Run(ctx, execx.Options{}, name, args...)
	if err != nil {
		return helperapi.Lines{}, err
	}
	return helperapi.Lines{Lines: strings.Split(strings.TrimRight(string(out), "\n"), "\n")}, nil
}

// helperapi.UnitRe: the journal command reads only CirrusUX's own units, and app
// units the app store made (cirrusux-<app>.service).
// entryRe: an archive entry's name, or "-" for none.
var entryRe = regexp.MustCompile(`^(-|[A-Za-z0-9][A-Za-z0-9._-]{0,200})$`)

var bannedRe = regexp.MustCompile(`Currently banned:\s*(\d+)`)

func securityStatus(ctx context.Context, c conf) helperapi.SecurityStatus {
	s := helperapi.SecurityStatus{Firewall: []helperapi.FirewallCheck{}}
	if c.Fail2banJail != "" {
		s.Fail2ban = true
		if out, err := execx.Run(ctx, execx.Options{}, "fail2ban-client", "status", c.Fail2banJail); err == nil {
			if m := bannedRe.FindSubmatch(out); m != nil {
				s.Banned, _ = strconv.Atoi(string(m[1]))
			}
		} else {
			s.Errors = append(s.Errors, "fail2ban: "+err.Error())
		}
	}
	for _, fc := range c.FirewallChecks {
		argv := fc.chainCommand()
		out, err := execx.Run(ctx, execx.Options{}, argv[0], argv[1:]...)
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
	out, err := execx.Run(ctx, execx.Options{Combined: true}, name, args...)
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
