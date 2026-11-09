package githost

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var credRe = regexp.MustCompile(`(https?://)[^/@\s]+@`)

// redactURL removes user:password@ from URLs in a message.
func redactURL(s string) string { return credRe.ReplaceAllString(s, "$1") }

// Import clones a repository from an https:// address. It is the only
// thing here that fetches from outside, and only when a person asks.
// A password in the address goes to git in its environment, not in its
// arguments, where `ps` would show it. Addresses that point at this
// machine or its network are refused unless AllowPrivateImport is set.
func (h *Host) Import(ctx context.Context, name, rawURL string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" || u.Path == "" || u.Path == "/" {
		return errf(Invalid, "Only https:// addresses can be imported")
	}
	if err := h.checkImportHost(ctx, u.Hostname()); err != nil {
		return err
	}
	unlock, err := h.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	if h.Exists(name) {
		return errf(Exists, "A repository called %s already exists", name)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	tmp := filepath.Join(h.Dir, ".tmp-"+name+"-"+hex.EncodeToString(b))
	defer os.RemoveAll(tmp)
	extra := []string{"GIT_ALLOW_PROTOCOL=https"}
	if u.User != nil {
		pw, _ := u.User.Password()
		auth := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pw))
		// Not followed to another address: the header would go along.
		extra = append(extra, "GIT_CONFIG_PARAMETERS="+configParams("http.extraHeader", auth, "http.followRedirects", "false"))
		u.User = nil
	}
	err = h.exec(ctx, h.Dir, nil, extra, "clone", "--bare", "-q", "--", u.String(), tmp)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Kind == Failed {
			return errf(Failed, "The import failed: %s", lastLine(redactURL(e.stderr)))
		}
		return err
	}
	if _, err := h.run(ctx, tmp, 4096, "remote", "remove", "origin"); err != nil {
		return err
	}
	if err := h.configure(ctx, tmp, ""); err != nil {
		return err
	}
	return os.Rename(tmp, h.path(name))
}

// configParams is GIT_CONFIG_PARAMETERS for key, value pairs: each entry
// single-quoted as git writes it.
func configParams(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, "'"+strings.ReplaceAll(kv[i]+"="+kv[i+1], "'", `'\''`)+"'")
	}
	return strings.Join(parts, " ")
}

// checkImportHost refuses a host that is, or resolves to, an address
// inside this machine or its network. A name that changes its answer
// between this check and git's own lookup can still get through.
func (h *Host) checkImportHost(ctx context.Context, host string) error {
	if h.AllowPrivateImport {
		return nil
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host); err == nil {
		ips = found
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip) {
			return errf(Invalid, "That address points inside your own network. Set CIRRUS_GIT_IMPORT_PRIVATE=1 to allow it")
		}
	}
	return nil
}

// cgnat is 100.64.0.0/10, which Tailscale uses.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return "git gave no reason"
}
