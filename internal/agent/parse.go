package agent

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/envfile"
	"cirrusux/internal/snapshot"
)

// Parsers for the text the idle checker, backup unit and virus scanner
// write. Formats checked against real output on 1 Nov 2026 (INSTALL.md §0);
// each parser still skips lines it can't read rather than failing.

// logZone is the zone of log timestamps that carry no offset: the
// machine's own, as the logs are written in local time.
var logZone = time.Local

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
			t, err = time.ParseInLocation(layout, raw, logZone)
		}
		if err != nil {
			continue
		}
		if t.Year() == 0 {
			t = time.Date(now.In(logZone).Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, logZone)
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
		}
		rest := strings.TrimSpace(line[len(m[0]):])
		// Older idle checkers wrote "2026-11-01 17:37:32 GMT ACTIVE: …";
		// cirrusux-idle writes RFC 3339 times, offset included.
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

// sleptBefore returns how long the server slept before the RESUMED event at i,
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

// timeline builds today's awake/asleep segments (the server's local day)
// from idle events.
func timeline(events []idleEvent, now time.Time, bootAt time.Time) []snapshot.Segment {
	l := now.In(logZone)
	dayStart := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, logZone)

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
				rtc, _ = time.ParseInLocation("2006-01-02 15:04:05", m[1], logZone)
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
	// Names for the idle checker's port numbers; the agent's config can
	// add more (CIRRUS_PORT_NAMES).
	portNames = map[string]string{"21": "FTP", "22": "SSH", "80": "HTTP", "443": "HTTPS"}
)

// reasonsFromMsg turns the checker's verdict (its last-msg state file),
// e.g. "ACTIVE: 1 remote login session(s) host conns: 22<-100.64.0.2", into
// readable reasons. Any other verdict (IDLE: …) means none.
func reasonsFromMsg(msg string, extra map[string]string) []string {
	msg = strings.TrimSpace(msg)
	rest, ok := strings.CutPrefix(msg, "ACTIVE:")
	if !ok {
		return nil
	}
	// CirrusUX's own checker separates reasons with "; "; older checkers
	// only with labels ("host conns: …").
	var parts []string
	for _, chunk := range strings.Split(strings.TrimSpace(rest), "; ") {
		last := 0
		for _, loc := range reasonSplit.FindAllStringIndex(chunk, -1) {
			parts = append(parts, chunk[last:loc[0]])
			last = loc[0]
		}
		parts = append(parts, chunk[last:])
	}

	var reasons []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if conns, ok := strings.CutPrefix(p, "host conns:"); ok {
			for _, m := range hostConnRe.FindAllStringSubmatch(conns, -1) {
				name := extra[m[1]]
				if name == "" {
					name = portNames[m[1]]
				}
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

// parseEpoch reads one of the idle checker's timestamp files (unix seconds).
func parseEpoch(s string) *time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	t := time.Unix(n, 0)
	return &t
}

// parseConf reads KEY=VALUE lines from a shell-sourced config file.
func parseConf(text string) map[string]string { return envfile.Map(text) }

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
