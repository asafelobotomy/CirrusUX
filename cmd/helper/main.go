// c64panel-helper runs as root through one sudoers rule per subcommand.
// It only reads, takes no arguments beyond the subcommand name, and never
// prints secrets: just the state the panel shows.
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
	"strconv"
	"strings"
	"time"

	"cloud64-panel/internal/helperapi"
)

const (
	idleRunDir  = "/run/cloud64-idle"
	stayAwake   = "/run/stay-awake"
	idleLog     = "/var/log/cloud64-idle.log"
	backupDir   = "/mnt/media/BackUp/cloud64-system"
	avLog       = "/var/log/clamav/weekly-scan.log"
	maxLines    = 400
	maxStateLen = 4096
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: c64panel-helper <%s>", strings.Join(helperapi.Commands, "|"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var out any
	var err error
	switch os.Args[1] {
	case helperapi.CmdIdleStatus:
		out, err = idleStatus()
	case helperapi.CmdIdleLog:
		out, err = tailFile(idleLog, maxLines)
	case helperapi.CmdBackupList:
		out, err = backupList()
	case helperapi.CmdBackupLast:
		out, err = command(ctx, "journalctl", "-u", "cloud64-backup.service", "-n", "60", "-o", "short-iso", "--no-pager")
	case helperapi.CmdAVLast:
		out, err = tailFile(avLog, 80)
	case helperapi.CmdSecurityStatus:
		out = securityStatus(ctx)
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

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "c64panel-helper: "+format+"\n", a...)
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

func backupList() (helperapi.BackupList, error) {
	var l helperapi.BackupList
	entries, err := os.ReadDir(backupDir)
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

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
