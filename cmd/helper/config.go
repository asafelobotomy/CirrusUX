package main

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"cirrusux/internal/envfile"
	"cirrusux/internal/helperapi"
)

// configPath holds the helper's settings: which modules this machine has
// and where their files are. It must belong to root and be writable only
// by root, because the helper runs as root; the agent can't pass paths.
const configPath = "/etc/cirrusux/helper.env"

// confDir holds files that setup modules write for themselves
// (cirrusux-setup's backup module writes backup.env, and so on), so they
// never edit the owner's helper.env. Same rules, read after it.
const confDir = "/etc/cirrusux/helper.d"

// loadAll reads helper.env, then helper.d/*.env in name order. A later
// file's value wins; firewall checks, media roots and storage roots add up
// (each counted once).
func loadAll(main, dir string) (conf, error) {
	var c conf
	files, _ := filepath.Glob(filepath.Join(dir, "*.env"))
	sort.Strings(files)
	for _, p := range append([]string{main}, files...) {
		if err := loadInto(&c, p); err != nil {
			return c, err
		}
	}
	return c, nil
}

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
	// App store: the runtime (docker or podman), where apps keep their
	// folders, where removed ones go, the user they run as, and the folders
	// media may be chosen from.
	ContainerRuntime string
	AppsDir          string
	AppsArchive      string
	AppsUser         string
	MediaRoots       []string
	AppsBind         string // publish apps' ports on this address only
	// StoreNoOfficial: STORE_OFFICIAL=no, the official CirrusStore source
	// isn't trusted (third-party ones are in store.d).
	StoreNoOfficial bool
	// StorageRoots are extra folders the Storage page measures, besides
	// the apps, media, backup and archive folders.
	StorageRoots []string
}

// firewallCheck passes when the chain's rules contain Pattern. Chain is an
// iptables chain ("DOCKER-USER") or an nftables one ("inet/cirrusux/input").
type firewallCheck struct{ Chain, Pattern string }

// validChain accepts CHAIN or family/table/chain, each part a word.
func validChain(c string) bool {
	parts := strings.Split(c, "/")
	if len(parts) != 1 && len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if !isWord(p) || strings.HasPrefix(p, ".") {
			return false
		}
	}
	return true
}

// chainCommand is the argv that lists the chain's rules.
func (f firewallCheck) chainCommand() []string {
	if p := strings.Split(f.Chain, "/"); len(p) == 3 {
		return []string{"nft", "list", "chain", p[0], p[1], p[2]}
	}
	return []string{"iptables", "-S", f.Chain}
}

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
	"CONTAINER_RUNTIME": func(c *conf, v string) error {
		if v != "docker" && v != "podman" {
			return fmt.Errorf("docker or podman")
		}
		c.ContainerRuntime = v
		return nil
	},
	"APPS_DIR":     path(func(c *conf) *string { return &c.AppsDir }),
	"APPS_ARCHIVE": path(func(c *conf) *string { return &c.AppsArchive }),
	"APPS_USER":    word(func(c *conf) *string { return &c.AppsUser }),
	"APPS_BIND": func(c *conf, v string) error {
		if _, err := netip.ParseAddr(v); err != nil {
			return fmt.Errorf("an IP address")
		}
		c.AppsBind = v
		return nil
	},
	"STORE_OFFICIAL": func(c *conf, v string) error {
		if v != "yes" && v != "no" {
			return fmt.Errorf("yes or no")
		}
		c.StoreNoOfficial = v == "no"
		return nil
	},
	"STORAGE_ROOTS": paths(func(c *conf) *[]string { return &c.StorageRoots }),
	"MEDIA_ROOTS":   paths(func(c *conf) *[]string { return &c.MediaRoots }),
	"FIREWALL_CHECKS": func(c *conf, v string) error {
		for _, f := range strings.Fields(v) {
			chain, pat, ok := strings.Cut(f, ":")
			if !ok || !validChain(chain) || pat == "" {
				return fmt.Errorf("FIREWALL_CHECKS: want CHAIN:pattern or family/table/chain:pattern, got %q", f)
			}
			if fc := (firewallCheck{chain, pat}); !slices.Contains(c.FirewallChecks, fc) {
				c.FirewallChecks = append(c.FirewallChecks, fc)
			}
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

// paths takes a space-separated list of paths, added to what earlier files
// gave; one given twice (in helper.env and a helper.d file) counts once.
func paths(field func(*conf) *[]string) func(*conf, string) error {
	return func(c *conf, v string) error {
		for _, f := range strings.Fields(v) {
			var p string
			if err := path(func(*conf) *string { return &p })(c, f); err != nil {
				return err
			}
			if list := field(c); !slices.Contains(*list, p) {
				*list = append(*list, p)
			}
		}
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

// loadConf reads one file. A missing file means no modules.
func loadConf(p string) (conf, error) {
	var c conf
	err := loadInto(&c, p)
	return c, err
}

func loadInto(c *conf, p string) error {
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ownerCheck(f); err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	for _, l := range envfile.Parse(string(b)) {
		set, known := keys[l.Key]
		if !l.HasEquals || !known {
			return fmt.Errorf("%s:%d: unknown setting %q", p, l.N, l.Key)
		}
		if l.Value == "" {
			continue
		}
		if err := set(c, l.Value); err != nil {
			return fmt.Errorf("%s:%d: %s: %v", p, l.N, l.Key, err)
		}
	}
	return nil
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

// hasApt: OS updates need Debian's apt; tests replace it.
var hasApt = func() bool {
	_, err := exec.LookPath("apt-get")
	return err == nil
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
	add(c.ContainerRuntime != "" && c.AppsDir != "" && c.AppsUser != "", helperapi.ModApps)
	add(hasApt(), helperapi.ModOS)
	add(len(c.places().Roots) > 0, helperapi.ModStorage)
	return f
}

// archiveDir is where removed apps' folders go: APPS_ARCHIVE, or "archive"
// beside the apps folder.
func (c conf) archiveDir() string {
	if c.AppsArchive != "" {
		return c.AppsArchive
	}
	return filepath.Join(filepath.Dir(c.AppsDir), "archive")
}
