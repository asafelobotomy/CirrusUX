// Package sysinfo reads two facts about the machine that the idle checker
// and the agent both need: when it booted and when its systemd timers fire.
package sysinfo

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BootTime is when the machine booted, from /proc/stat; zero if unknown.
func BootTime() time.Time { return ParseBootTime(readStat()) }

func readStat() string {
	b, _ := os.ReadFile("/proc/stat")
	return string(b)
}

// ParseBootTime reads the "btime" line of /proc/stat.
func ParseBootTime(stat string) time.Time {
	for _, line := range strings.Split(stat, "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
	}
	return time.Time{}
}

// ListTimersArgs is the `systemctl` command line whose output ParseTimers reads.
var ListTimersArgs = []string{"list-timers", "--all", "--output=json"}

// Timer is a systemd timer that has a next run.
type Timer struct {
	Unit string
	Next time.Time
}

// ParseTimers reads `systemctl list-timers --all --output=json`, keeping
// timers with a next run, soonest first.
func ParseTimers(out string) ([]Timer, error) {
	var raw []struct {
		Next int64  `json:"next"` // µs since epoch
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	var timers []Timer
	for _, t := range raw {
		if t.Next > 0 {
			timers = append(timers, Timer{Unit: t.Unit, Next: time.UnixMicro(t.Next)})
		}
	}
	sort.Slice(timers, func(i, j int) bool { return timers[i].Next.Before(timers[j].Next) })
	return timers, nil
}
