package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cirrusux/internal/execx"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/netinfo"
)

// Network settings (net-*): DNS servers through a systemd-resolved drop-in,
// and Tailscale's accept-dns and accept-routes. Each change is written down
// first and undone by a systemd timer after two minutes unless net-confirm
// stops it, so a setting that cuts the panel off from the server undoes
// itself. One change waits at a time.

const dnsDropin = "etc/systemd/resolved.conf.d/50-cirrusux.conf"

// netRun runs a tool; tests replace it.
var netRun = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execx.Run(ctx, execx.Options{Combined: true, Timeout: 20 * time.Second}, name, args...)
}

func pendingPath() string { return rootFile(helperapi.NetPendingFile) }

func readPending() *helperapi.NetPending {
	b, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil
	}
	var p helperapi.NetPending
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return &p
}

func netStatus(ctx context.Context) helperapi.NetStatus {
	st := helperapi.NetStatus{DNS: []string{}, Search: []string{}, Pending: readPending()}
	_, err := netRun(ctx, "systemctl", "is-active", "--quiet", "systemd-resolved")
	st.Resolved = err == nil
	if b, err := os.ReadFile(rootFile(dnsDropin)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
			switch k {
			case "DNS":
				st.DNS = strings.Fields(v)
			case "Domains":
				st.Search = strings.Fields(v)
			}
		}
	}
	st.Tailscale = tailscalePrefs(ctx)
	return st
}

func tailscalePrefs(ctx context.Context) *helperapi.TSPrefs {
	out, err := netRun(ctx, "tailscale", "debug", "prefs")
	if err != nil {
		return nil
	}
	var p struct{ CorpDNS, RouteAll bool }
	if json.Unmarshal(out, &p) != nil {
		return nil
	}
	return &helperapi.TSPrefs{AcceptDNS: p.CorpDNS, AcceptRoutes: p.RouteAll}
}

// beginChange writes the pending record and starts the timer that undoes it.
// A record left by a reboot (its timer gone) is undone first.
func beginChange(ctx context.Context, p helperapi.NetPending) error {
	if err := settle(ctx); err != nil {
		return err
	}
	p.Deadline = time.Now().Add(helperapi.NetRevertSeconds * time.Second)
	if err := fsutil.WriteJSON(pendingPath(), p, 0o644); err != nil {
		return err
	}
	self, _ := os.Executable()
	out, err := netRun(ctx, "systemd-run", "--quiet", "--collect", "--unit="+helperapi.NetRevertUnit,
		fmt.Sprintf("--on-active=%ds", helperapi.NetRevertSeconds), self, helperapi.CmdNetRollback)
	if err != nil {
		os.Remove(pendingPath())
		return fmt.Errorf("starting the timer that undoes it: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// settle refuses a change while another waits, and undoes one that was
// never kept (a reboot lost its timer). Callers settle before reading what
// they are about to replace.
func settle(ctx context.Context) error {
	old := readPending()
	if old == nil {
		return nil
	}
	if time.Now().Before(old.Deadline.Add(time.Minute)) {
		return fmt.Errorf("%s is waiting to be kept or undone: keep it, or undo it, first", old.What)
	}
	if _, err := netRollback(ctx); err != nil {
		return fmt.Errorf("undoing the earlier change that was never kept: %v", err)
	}
	return nil
}

// abandon undoes a change that failed to apply.
func abandon(ctx context.Context, cause error) error {
	if _, err := netRollback(ctx); err != nil {
		return fmt.Errorf("%v; and undoing it failed: %v", cause, err)
	}
	return cause
}

func netDNS(ctx context.Context, servers, search string) (helperapi.Result, error) {
	if !netStatus(ctx).Resolved {
		return helperapi.Result{}, errors.New("DNS here isn't managed by systemd-resolved, so the panel won't change it")
	}
	dns, domains, err := netinfo.ParseDNS(servers, search)
	if err != nil {
		return helperapi.Result{}, err
	}
	if err := settle(ctx); err != nil {
		return helperapi.Result{}, err
	}
	file := rootFile(dnsDropin)
	prev, rerr := os.ReadFile(file)
	p := helperapi.NetPending{Kind: "dns", DNSHad: rerr == nil, DNSPrev: string(prev)}
	if len(dns) == 0 && len(domains) == 0 {
		p.What = "Using the system's own DNS"
	} else {
		p.What = "DNS " + netinfo.JoinList(dns)
	}
	if err := beginChange(ctx, p); err != nil {
		return helperapi.Result{}, err
	}
	if len(dns) == 0 && len(domains) == 0 {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return helperapi.Result{}, abandon(ctx, err)
		}
	} else {
		var b strings.Builder
		b.WriteString("# Written by CirrusUX (Network page). It is undone by the panel's Undo.\n[Resolve]\n")
		if len(dns) > 0 {
			b.WriteString("DNS=" + strings.Join(dns, " ") + "\n")
		}
		if len(domains) > 0 {
			b.WriteString("Domains=" + strings.Join(domains, " ") + "\n")
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return helperapi.Result{}, abandon(ctx, err)
		}
		if err := fsutil.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			return helperapi.Result{}, abandon(ctx, err)
		}
	}
	if out, err := netRun(ctx, "systemctl", "restart", "systemd-resolved"); err != nil {
		return helperapi.Result{}, abandon(ctx, fmt.Errorf("restarting systemd-resolved: %v: %s", err, strings.TrimSpace(string(out))))
	}
	return pendingResult(p), nil
}

func netTailscale(ctx context.Context, dns, routes string) (helperapi.Result, error) {
	if err := settle(ctx); err != nil {
		return helperapi.Result{}, err
	}
	cur := tailscalePrefs(ctx)
	if cur == nil {
		return helperapi.Result{}, errors.New("Tailscale isn't installed or isn't running here")
	}
	want := helperapi.TSPrefs{AcceptDNS: dns == "yes", AcceptRoutes: routes == "yes"}
	if want == *cur {
		return helperapi.Result{Output: []string{"Tailscale already has those settings"}}, nil
	}
	p := helperapi.NetPending{Kind: "tailscale", What: "Tailscale settings", TSPrev: cur}
	if err := beginChange(ctx, p); err != nil {
		return helperapi.Result{}, err
	}
	if err := setTailscale(ctx, want); err != nil {
		return helperapi.Result{}, abandon(ctx, err)
	}
	return pendingResult(p), nil
}

func setTailscale(ctx context.Context, p helperapi.TSPrefs) error {
	yn := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	out, err := netRun(ctx, "tailscale", "set", "--accept-dns="+yn(p.AcceptDNS), "--accept-routes="+yn(p.AcceptRoutes))
	if err != nil {
		return fmt.Errorf("tailscale set: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pendingResult(p helperapi.NetPending) helperapi.Result {
	return helperapi.Result{Output: []string{
		p.What + " applied. It is undone in " + fmt.Sprint(helperapi.NetRevertSeconds) + " seconds unless you keep it in the panel.",
	}}
}

// netConfirm keeps the change: the timer is stopped and the record dropped.
func netConfirm(ctx context.Context) (helperapi.Result, error) {
	p := readPending()
	if p == nil {
		return helperapi.Result{Output: []string{"nothing is waiting to be kept"}}, nil
	}
	if time.Now().After(p.Deadline) {
		return helperapi.Result{}, errors.New("that change has already been undone, or is being")
	}
	netRun(ctx, "systemctl", "stop", helperapi.NetRevertUnit+".timer") // may already be gone
	if err := os.Remove(pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{p.What + " kept"}}, nil
}

// netRevert undoes the waiting change now.
func netRevert(ctx context.Context) (helperapi.Result, error) {
	netRun(ctx, "systemctl", "stop", helperapi.NetRevertUnit+".timer")
	return netRollback(ctx)
}

// netRollback puts back what the waiting change replaced. The timer runs
// it; so does net-revert.
func netRollback(ctx context.Context) (helperapi.Result, error) {
	p := readPending()
	if p == nil {
		return helperapi.Result{Output: []string{"nothing to undo"}}, nil
	}
	switch p.Kind {
	case "dns":
		file := rootFile(dnsDropin)
		if p.DNSHad {
			if err := fsutil.WriteFile(file, []byte(p.DNSPrev), 0o644); err != nil {
				return helperapi.Result{}, err
			}
		} else if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return helperapi.Result{}, err
		}
		if out, err := netRun(ctx, "systemctl", "restart", "systemd-resolved"); err != nil {
			return helperapi.Result{}, fmt.Errorf("restarting systemd-resolved: %v: %s", err, strings.TrimSpace(string(out)))
		}
	case "tailscale":
		if p.TSPrev == nil {
			return helperapi.Result{}, errors.New("no earlier Tailscale settings were kept")
		}
		if err := setTailscale(ctx, *p.TSPrev); err != nil {
			return helperapi.Result{}, err
		}
	}
	if err := os.Remove(pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{p.What + " undone"}}, nil
}

var (
	netServersRe = regexp.MustCompile(`^(-|[0-9A-Fa-f.:,]{2,120})$`)
	netSearchRe  = regexp.MustCompile(`^(-|[A-Za-z0-9.,-]{1,300})$`)
)

func yesNo(s string) bool { return s == "yes" || s == "no" }

func init() {
	argChecks[helperapi.CmdNetDNS] = func(a []string) bool {
		return len(a) == 2 && netServersRe.MatchString(a[0]) && netSearchRe.MatchString(a[1])
	}
	argChecks[helperapi.CmdNetTailscale] = func(a []string) bool { return len(a) == 2 && yesNo(a[0]) && yesNo(a[1]) }
}
