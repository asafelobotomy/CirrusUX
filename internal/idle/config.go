package idle

import (
	"fmt"
	"strconv"
	"strings"

	"cirrusux/internal/envfile"
)

// ParseConfig reads sleep.env: KEY=value lines, # comments, quotes allowed.
func ParseConfig(text string) (Config, error) {
	cfg := DefaultConfig()
	for _, l := range envfile.Parse(text) {
		if !l.HasEquals {
			return cfg, fmt.Errorf("line %d: want KEY=value", l.N)
		}
		k, v := l.Key, l.Value
		var err error
		switch k {
		case "IDLE_MIN":
			cfg.IdleMin, err = positive(v)
		case "GRACE_MIN":
			cfg.GraceMin, err = strconv.Atoi(v)
		case "WATCH_PORTS":
			cfg.WatchPorts = nil
			for _, f := range strings.Fields(v) {
				p, e := strconv.Atoi(f)
				if e != nil {
					err = e
				}
				cfg.WatchPorts = append(cfg.WatchPorts, p)
			}
		case "IGNORE_CLIENTS":
			cfg.IgnoreClients = strings.Fields(v)
		case "IGNORE_CLIENTS_PLAY_MB":
			if cfg.IgnorePlayMB, err = strconv.ParseFloat(v, 64); err == nil && cfg.IgnorePlayMB < 0 {
				err = fmt.Errorf("must be 0 or more")
			}
		case "IGNORE_WAKE_GRACE_MIN":
			if cfg.IgnoreWakeGraceMin, err = strconv.Atoi(v); err == nil && cfg.IgnoreWakeGraceMin < 0 {
				err = fmt.Errorf("must be 0 or more")
			}
		case "BUSY_UNITS":
			cfg.BusyUnits = strings.Fields(v)
		case "WAKE_TIMERS":
			cfg.WakeTimers = strings.Fields(v)
		case "MAX_LOAD":
			cfg.MaxLoad, err = strconv.ParseFloat(v, 64)
		case "COUNT_LOGINS":
			cfg.CountLogins = v != "no"
		default:
			err = fmt.Errorf("unknown setting")
		}
		if err != nil {
			return cfg, fmt.Errorf("line %d: %s: %v", l.N, k, err)
		}
	}
	return cfg, nil
}

func positive(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err == nil && n < 1 {
		err = fmt.Errorf("must be at least 1")
	}
	return n, err
}
