package agent

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/snapshot"
)

// parseBackupJournal finds the last run of the backup unit in
// `journalctl -o short-iso` output.
func parseBackupJournal(lines []string, unit string, now time.Time) *snapshot.BackupRun {
	base := strings.TrimSuffix(unit, ".service")
	var run *snapshot.BackupRun
	for _, line := range lines {
		at, rest, ok := splitTS(line, now)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(rest, "Starting "+base):
			run = &snapshot.BackupRun{Start: at}
		case run == nil:
			continue
		case strings.Contains(rest, "Failed with result") || strings.Contains(rest, "Failed to start "+base):
			run.End, run.OK = at, false
		case strings.Contains(rest, "Finished "+base) || strings.Contains(rest, "Deactivated successfully"):
			if run.End.IsZero() {
				run.End, run.OK = at, true
			}
		default:
			// The backup script's last line: "OK <size> <archive>", logged
			// as "host script[pid]: OK 2.0G name". Nothing else matches, so
			// a tar warning that happens to say "OK" is not taken for it.
			if m := backupOKLine.FindStringSubmatch(rest); m != nil {
				run.Summary = m[1]
			}
		}
	}
	if run != nil && run.End.IsZero() {
		return nil // still running, or never finished
	}
	return run
}

// backupOKLine matches the script's success line after the "[pid]: " the
// journal puts in front of it.
var backupOKLine = regexp.MustCompile(`\]: (OK \S+ \S+)$`)

var (
	avScanned  = regexp.MustCompile(`Scanned files:\s*(\d+)`)
	avInfected = regexp.MustCompile(`Infected files:\s*(\d+)`)
	avEnd      = regexp.MustCompile(`End Date:\s*(\d{4}:\d{2}:\d{2} \d{2}:\d{2}:\d{2})`)
)

// parseClamAV reads the last clamscan summary block.
func parseClamAV(lines []string) *snapshot.ClamAV {
	var res *snapshot.ClamAV
	var cur snapshot.ClamAV
	for _, line := range lines {
		if m := avScanned.FindStringSubmatch(line); m != nil {
			cur.Files, _ = strconv.Atoi(m[1])
		}
		if m := avInfected.FindStringSubmatch(line); m != nil {
			cur.Infected, _ = strconv.Atoi(m[1])
		}
		if m := avEnd.FindStringSubmatch(line); m != nil {
			if t, err := time.ParseInLocation("2006:01:02 15:04:05", m[1], logZone); err == nil {
				cur.At = t
				c := cur
				res = &c
				cur = snapshot.ClamAV{}
			}
		}
	}
	return res
}
