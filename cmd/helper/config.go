package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"cirrusux/internal/helperapi"
)

// configPath holds the helper's settings: which modules this machine has
// and where their files are. It must belong to root and be writable only
// by root, because the helper runs as root; the agent can't pass paths.
const configPath = "/etc/cirrusux/helper.env"

// conf is the helper's view of the machine. Empty values switch a module
// off: a fresh install with no modules still lists containers.
type conf struct {
	// Sleep module: the idle checker's state files and log, the stay-awake
	// hold file and the scripts that set and clear it.
	IdleStateDir  string
	IdleLog       string
	StayAwakeFile string
	StayAwakeBin  string
	AllowSleepBin string
	// Backups: the systemd unit that runs them and where archives land.
	BackupUnit   string
	BackupDir    string
	BackupPrefix string
	// Security.
	AVLog          string
	Fail2banJail   string
	FirewallChecks []firewallCheck
	EgressReport   string
}

// firewallCheck passes when `iptables -S Chain` contains Pattern.
type firewallCheck struct{ Chain, Pattern string }

var keys = map[string]func(c *conf, v string) error{
	"IDLE_STATE_DIR":  path(func(c *conf) *string { return &c.IdleStateDir }),
	"IDLE_LOG":        path(func(c *conf) *string { return &c.IdleLog }),
	"STAY_AWAKE_FILE": path(func(c *conf) *string { return &c.StayAwakeFile }),
	"STAY_AWAKE_BIN":  path(func(c *conf) *string { return &c.StayAwakeBin }),
	"ALLOW_SLEEP_BIN": path(func(c *conf) *string { return &c.AllowSleepBin }),
	"BACKUP_UNIT":     word(func(c *conf) *string { return &c.BackupUnit }),
	"BACKUP_DIR":      path(func(c *conf) *string { return &c.BackupDir }),
	"BACKUP_PREFIX":   word(func(c *conf) *string { return &c.BackupPrefix }),
	"AV_LOG":          path(func(c *conf) *string { return &c.AVLog }),
	"FAIL2BAN_JAIL":   word(func(c *conf) *string { return &c.Fail2banJail }),
	"EGRESS_REPORT":   path(func(c *conf) *string { return &c.EgressReport }),
	"FIREWALL_CHECKS": func(c *conf, v string) error {
		for _, f := range strings.Fields(v) {
			chain, pat, ok := strings.Cut(f, ":")
			if !ok || !isWord(chain) || pat == "" {
				return fmt.Errorf("FIREWALL_CHECKS: want CHAIN:pattern, got %q", f)
			}
			c.FirewallChecks = append(c.FirewallChecks, firewallCheck{chain, pat})
		}
		return nil
	},
}

func path(field func(*conf) *string) func(*conf, string) error {
	return func(c *conf, v string) error {
		if !filepath.IsAbs(v) || filepath.Clean(v) != v {
			return fmt.Errorf("want a clean absolute path, got %q", v)
		}
		*field(c) = v
		return nil
	}
}

func word(field func(*conf) *string) func(*conf, string) error {
	return func(c *conf, v string) error {
		if !isWord(v) {
			return fmt.Errorf("want letters, digits, - _ . @ only, got %q", v)
		}
		*field(c) = v
		return nil
	}
}

func isWord(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.@", r)) {
			return false
		}
	}
	return true
}

// loadConf reads configPath. A missing file means no modules.
func loadConf(p string) (conf, error) {
	var c conf
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	if err := ownerCheck(f); err != nil {
		return c, err
	}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		set, known := keys[strings.TrimSpace(k)]
		if !ok || !known {
			return c, fmt.Errorf("%s:%d: unknown setting %q", p, n, k)
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v == "" {
			continue
		}
		if err := set(&c, v); err != nil {
			return c, fmt.Errorf("%s:%d: %s: %v", p, n, k, err)
		}
	}
	return c, sc.Err()
}

// ownerCheck is rootOnly; tests, which can't create root's files, swap it.
var ownerCheck = rootOnly

func rootOnly(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s must belong to root and be writable only by root", f.Name())
	}
	return nil
}

// features lists the modules this machine has, for the agent and the UI.
func (c conf) features() helperapi.Features {
	f := helperapi.Features{Modules: []string{}}
	add := func(on bool, name string) {
		if on {
			f.Modules = append(f.Modules, name)
		}
	}
	add(c.IdleStateDir != "" || c.IdleLog != "", helperapi.ModSleep)
	add(c.StayAwakeBin != "" && c.AllowSleepBin != "" && c.StayAwakeFile != "", helperapi.ModStayAwake)
	add(c.BackupUnit != "", helperapi.ModBackup)
	add(c.AVLog != "", helperapi.ModAV)
	add(c.Fail2banJail != "" || len(c.FirewallChecks) > 0, helperapi.ModSecurity)
	add(c.EgressReport != "", helperapi.ModEgress)
	return f
}
