// Package osupdate checks for and installs Debian package updates, only
// when the owner asks (PHASES-4-6 §6b). Both run as root background jobs
// in the helper; the result is a file the agent reports.
package osupdate

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Package is one upgradable package.
type Package struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	Security bool   `json:"security,omitempty"`
}

// Result is the last check, or what an upgrade left.
type Result struct {
	Checked        time.Time `json:"checked"`
	Packages       []Package `json:"packages"`
	RebootRequired bool      `json:"rebootRequired,omitempty"`
	// Upgraded is set after an upgrade: when, and how many packages.
	Upgraded      *time.Time `json:"upgraded,omitempty"`
	UpgradedCount int        `json:"upgradedCount,omitempty"`
}

// ParseUpgradable reads `apt list --upgradable`:
//
//	bash/stable-security 5.2.15-2+b7 amd64 [upgradable from: 5.2.15-2+b2]
func ParseUpgradable(out string) []Package {
	pkgs := []Package{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || !strings.Contains(f[0], "/") || f[3] != "[upgradable" {
			continue
		}
		name, suite, _ := strings.Cut(f[0], "/")
		pkgs = append(pkgs, Package{
			Name: name, To: f[1], From: strings.TrimSuffix(f[len(f)-1], "]"),
			Security: strings.Contains(suite, "-security"),
		})
	}
	return pkgs
}

// Exec runs a command and returns its output; tests replace it.
type Exec func(ctx context.Context, env []string, name string, args ...string) (string, error)

// Runner does the work; Progress reports each step.
type Runner struct {
	Exec     Exec
	Progress func(step string)
	Now      func() time.Time
	// ResultFile is where the result goes (readable by the agent);
	// RebootFile is Debian's /run/reboot-required.
	ResultFile, RebootFile string
}

var aptEnv = []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}

// Check refreshes the package lists and records what's upgradable.
func (r *Runner) Check(ctx context.Context) (Result, error) {
	r.Progress("Refreshing the package lists")
	if _, err := r.Exec(ctx, aptEnv, "apt-get", "update", "-qq"); err != nil {
		return Result{}, err
	}
	return r.record(ctx, nil, 0)
}

// Upgrade installs the pending upgrades: `apt-get upgrade --with-new-pkgs`,
// which never removes packages and installs new ones only when an upgrade
// needs them (a new kernel arrives as a new package; without this, kernel
// security fixes would wait forever). Changed config files are kept as they
// are. Then it checks again.
func (r *Runner) Upgrade(ctx context.Context) (Result, error) {
	r.Progress("Refreshing the package lists")
	if _, err := r.Exec(ctx, aptEnv, "apt-get", "update", "-qq"); err != nil {
		return Result{}, err
	}
	before, _ := r.Exec(ctx, aptEnv, "apt", "list", "--upgradable")
	n := len(ParseUpgradable(before))
	r.Progress("Installing " + plural(n, "package"))
	if _, err := r.Exec(ctx, aptEnv, "apt-get", "upgrade", "--with-new-pkgs", "-y", "-q",
		"-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef"); err != nil {
		return Result{}, err
	}
	t := r.Now()
	return r.record(ctx, &t, n)
}

func (r *Runner) record(ctx context.Context, upgraded *time.Time, count int) (Result, error) {
	r.Progress("Listing upgradable packages")
	out, err := r.Exec(ctx, aptEnv, "apt", "list", "--upgradable")
	if err != nil {
		return Result{}, err
	}
	res := Result{Checked: r.Now(), Packages: ParseUpgradable(out), Upgraded: upgraded, UpgradedCount: count}
	if _, err := os.Stat(r.RebootFile); err == nil {
		res.RebootRequired = true
	}
	return res, write(r.ResultFile, res)
}

// Read is the last result, or nil. Whether a restart is needed is read
// again from rebootFile, which a restart clears: the saved answer would
// still ask for one after it.
func Read(path, rebootFile string) *Result {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	_, err = os.Stat(rebootFile)
	r.RebootRequired = err == nil
	return &r
}

func write(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
