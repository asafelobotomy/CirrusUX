package agent

// Parsers for the text the cloud64 scripts and services write. Formats
// checked against real output on 1 Nov 2026 (INSTALL.md §0); each parser
// still skips lines it can't read rather than failing.

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud64-panel/internal/snapshot"
)

var London = mustLoc("Europe/London")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return l
}

var tsLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05-0700", // journalctl -o short-iso
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"Jan _2 15:04:05", // syslog, no year
}

var leadingTS = regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?|[A-Z][a-z]{2} [ \d]\d \d{2}:\d{2}:\d{2})\]?\s*`)

// splitTS returns the timestamp at the start of a line and the rest.
func splitTS(line string, now time.Time) (time.Time, string, bool) {
	m := leadingTS.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}, line, false
	}
	raw := m[1]
	for _, layout := range tsLayouts {
		var t time.Time
		var err error
		if strings.ContainsAny(raw[10:], "Z+-") || strings.HasSuffix(raw, "Z") {
			t, err = time.Parse(layout, raw)
		} else {
			t, err = time.ParseInLocation(layout, raw, London)
		}
		if err != nil {
			continue
		}
		if t.Year() == 0 {
			t = time.Date(now.In(London).Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, London)
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
		}
		rest := strings.TrimSpace(line[len(m[0]):])
		// cloud64-idle.log writes "2026-11-01 17:37:32 GMT ACTIVE: …"
		for _, z := range []string{"GMT ", "BST ", "UTC "} {
			rest = strings.TrimPrefix(rest, z)
		}
		return t, rest, true
	}
	return time.Time{}, line, false
}

type idleEvent struct {
	At     time.Time
	Kind   string // ACTIVE, IDLE, SUSPEND, RESUMED
	Detail string
}

var idleKinds = []string{"SUSPEND", "RESUMED", "ACTIVE", "IDLE", "WAKE-DIAG"}

func parseIdleLog(lines []string, now time.Time) []idleEvent {
	var out []idleEvent
	for _, line := range lines {
		at, rest, ok := splitTS(line, now)
		if !ok {
			continue
		}
		for _, k := range idleKinds {
			i := strings.Index(rest, k)
			if i < 0 {
				continue
			}
			detail := strings.TrimLeft(rest[i+len(k):], ": ")
			out = append(out, idleEvent{At: at, Kind: k, Detail: strings.TrimSpace(detail)})
			break
		}
	}
	return out
}

var (
	irqRe     = regexp.MustCompile(`irq\s*(\d+)`)
	rtcSetRe  = regexp.MustCompile(`RTC wake set \w{3} (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
	sleptRe   = regexp.MustCompile(`slept ~(\d+)s`)
	rtcWindow = 5 * time.Minute
)

// wakeReason tells a scheduled RTC wake from a network one: the checker logs
// "RTC wake set <time>" when it suspends.
func wakeReason(resume idleEvent, rtc time.Time) string {
	if !rtc.IsZero() && resume.At.Sub(rtc).Abs() <= rtcWindow {
		return "Scheduled wake (RTC)"
	}
	if m := irqRe.FindStringSubmatch(resume.Detail); m != nil {
		return "Woken by network (irq " + m[1] + ")"
	}
	return "Woken"
}

// sleptBefore returns how long cloud64 slept before the RESUMED event at i,
// from the WAKE-DIAG line logged shortly after it ("slept ~933s").
func sleptBefore(events []idleEvent, i int) time.Duration {
	for _, e := range events[i+1:] {
		if e.At.Sub(events[i].At) > 2*time.Minute {
			break
		}
		if e.Kind == "WAKE-DIAG" {
			if m := sleptRe.FindStringSubmatch(e.Detail); m != nil {
				n, _ := strconv.Atoi(m[1])
				return time.Duration(n) * time.Second
			}
		}
	}
	return 0
}

// timeline builds today's awake/asleep segments (UK day) from idle events.
func timeline(events []idleEvent, now time.Time, bootAt time.Time) []snapshot.Segment {
	l := now.In(London)
	dayStart := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, London)

	state := "awake"
	from := dayStart
	if bootAt.After(dayStart) {
		from = bootAt
	}
	var reason string
	var segs []snapshot.Segment
	push := func(to time.Time) {
		if !to.After(from) {
			return
		}
		t := to
		segs = append(segs, snapshot.Segment{From: from, To: &t, State: state, Reason: reason})
	}
	var rtc time.Time
	for i, e := range events {
		var next string
		switch e.Kind {
		case "SUSPEND":
			next = "asleep"
			rtc = time.Time{}
			if m := rtcSetRe.FindStringSubmatch(e.Detail); m != nil {
				rtc, _ = time.ParseInLocation("2006-01-02 15:04:05", m[1], London)
			}
		case "RESUMED":
			next = "awake"
		default:
			continue
		}
		if e.At.Before(dayStart) {
			state, reason = next, ""
			continue
		}
		if next == "awake" && state == "awake" {
			// A resume with no SUSPEND logged before it (e.g. a manual
			// test cycle): place the sleep using WAKE-DIAG's duration.
			d := sleptBefore(events, i)
			if d == 0 || e.At.Add(-d).Before(from) {
				continue
			}
			push(e.At.Add(-d))
			state, from, reason = "asleep", e.At.Add(-d), ""
		}
		if next == state {
			continue
		}
		push(e.At)
		state, from, reason = next, e.At, ""
		if next == "awake" {
			reason = wakeReason(e, rtc)
		}
	}
	segs = append(segs, snapshot.Segment{From: from, State: state, Reason: reason})
	return segs
}

// lastResume is the last RESUMED event, or the boot time.
func lastResume(events []idleEvent, bootAt time.Time) time.Time {
	t := bootAt
	for _, e := range events {
		if e.Kind == "RESUMED" && e.At.After(t) {
			t = e.At
		}
	}
	return t
}

func lastActivity(events []idleEvent) *time.Time {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == "ACTIVE" {
			t := events[i].At
			return &t
		}
	}
	return nil
}

var (
	// A new reason starts where a lowercase "label:" begins, e.g.
	// "1 remote login session(s) host conns: 22<-100.64.0.2".
	reasonSplit = regexp.MustCompile(`(?:^|\s+)[a-z][a-z -]*:\s`)
	hostConnRe  = regexp.MustCompile(`(\d+)<-(\S+)`)
	portNames   = map[string]string{"22": "SSH", "21": "FTP", "443": "OpenResty", "8080": "OpenResty", "22457": "1Panel"}
)

// reasonsFromMsg turns the checker's verdict (/run/cloud64-idle/last-msg),
// e.g. "ACTIVE: 1 remote login session(s) host conns: 22<-100.64.0.2", into
// readable reasons. Any other verdict (IDLE: …) means none.
func reasonsFromMsg(msg string) []string {
	msg = strings.TrimSpace(msg)
	rest, ok := strings.CutPrefix(msg, "ACTIVE:")
	if !ok {
		return nil
	}
	rest = strings.TrimSpace(rest)
	var parts []string
	last := 0
	for _, loc := range reasonSplit.FindAllStringIndex(rest, -1) {
		parts = append(parts, rest[last:loc[0]])
		last = loc[0]
	}
	parts = append(parts, rest[last:])

	var reasons []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if conns, ok := strings.CutPrefix(p, "host conns:"); ok {
			for _, m := range hostConnRe.FindAllStringSubmatch(conns, -1) {
				name := portNames[m[1]]
				if name == "" {
					name = "port " + m[1]
				}
				reasons = append(reasons, name+" from "+m[2])
			}
			continue
		}
		reasons = append(reasons, p)
	}
	return reasons
}

// parseEpoch reads a /run/cloud64-idle timestamp file (unix seconds).
func parseEpoch(s string) *time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	t := time.Unix(n, 0)
	return &t
}

// parseConf reads KEY=VALUE lines from a shell-sourced config file.
func parseConf(text string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if i := strings.Index(v, " #"); i >= 0 {
			v = v[:i]
		}
		m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

func confInt(m map[string]string, key string, def int) int {
	if n, err := strconv.Atoi(m[key]); err == nil {
		return n
	}
	return def
}

// parseStayAwake reads /run/stay-awake: an expiry epoch, or empty for "until
// allow-sleep".
func parseStayAwake(content string) (until *time.Time, forever bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, true
	}
	n, err := strconv.ParseInt(strings.Fields(content)[0], 10, 64)
	if err != nil {
		return nil, true
	}
	t := time.Unix(n, 0)
	return &t, false
}

// parseBackupJournal finds the last run of cloud64-backup.service in
// `journalctl -o short-iso` output.
func parseBackupJournal(lines []string, now time.Time) *snapshot.BackupRun {
	var run *snapshot.BackupRun
	for _, line := range lines {
		at, rest, ok := splitTS(line, now)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(rest, "Starting cloud64-backup"):
			run = &snapshot.BackupRun{Start: at}
		case run == nil:
			continue
		case strings.Contains(rest, "Failed with result") || strings.Contains(rest, "Failed to start cloud64-backup"):
			run.End, run.OK = at, false
		case strings.Contains(rest, "Finished cloud64-backup") || strings.Contains(rest, "Deactivated successfully"):
			if run.End.IsZero() {
				run.End, run.OK = at, true
			}
		case strings.Contains(rest, " OK ") || strings.HasSuffix(rest, " OK") || strings.Contains(rest, "OK "):
			run.Summary = rest
		}
	}
	if run != nil && run.End.IsZero() {
		return nil // still running, or never finished
	}
	if run != nil && run.Summary != "" {
		if _, msg, ok := strings.Cut(run.Summary, ": "); ok {
			run.Summary = msg
		}
	}
	return run
}

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
			if t, err := time.ParseInLocation("2006:01:02 15:04:05", m[1], London); err == nil {
				cur.At = t
				c := cur
				res = &c
				cur = snapshot.ClamAV{}
			}
		}
	}
	return res
}

var healthRe = regexp.MustCompile(`\((healthy|unhealthy|health: starting)\)`)

// containerHealth turns Docker's state and status text into one word.
func containerHealth(state, status string) string {
	if state != "running" {
		return "exited"
	}
	if m := healthRe.FindStringSubmatch(status); m != nil {
		if m[1] == "health: starting" {
			return "starting"
		}
		return m[1]
	}
	return "running"
}

// splitImage separates "repo/name:tag" into image and tag ("latest" if none).
func splitImage(ref string) (string, string) {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[:i], ref[i+1:]
	}
	return ref, "latest"
}
