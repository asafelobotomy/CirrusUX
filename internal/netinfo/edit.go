package netinfo

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// The settings the panel can change on the server (docs/CONFIG.md,
// "Network settings"): DNS servers and Tailscale's two preferences. Every
// change is undone by the server itself after two minutes unless it is kept
// from the panel, so a setting that cuts the panel off reverts on its own.

// MaxDNS is how many name servers one change may set.
const MaxDNS = 3

var domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// ParseDNS checks name servers and search domains as the panel passes them:
// comma-separated, or "-" for none (the system's own). It returns them in
// canonical form. Loopback, unspecified and multicast addresses are refused:
// they would point name lookups at nothing.
func ParseDNS(servers, search string) (dns, domains []string, err error) {
	if servers != "-" {
		for _, s := range strings.Split(servers, ",") {
			a, e := netip.ParseAddr(strings.TrimSpace(s))
			if e != nil || a.Zone() != "" {
				return nil, nil, fmt.Errorf("%q isn't an IP address", s)
			}
			a = a.Unmap()
			if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() {
				return nil, nil, fmt.Errorf("%s can't be a name server", a)
			}
			dns = append(dns, a.String())
		}
		if len(dns) == 0 || len(dns) > MaxDNS {
			return nil, nil, fmt.Errorf("give 1 to %d name servers, or none to use the system's own", MaxDNS)
		}
	}
	if search != "-" && search != "" {
		for _, d := range strings.Split(search, ",") {
			d = strings.ToLower(strings.TrimSpace(d))
			if len(d) > 253 || !domainRe.MatchString(d) {
				return nil, nil, fmt.Errorf("%q isn't a domain name", d)
			}
			domains = append(domains, d)
		}
		if len(domains) > 6 {
			return nil, nil, fmt.Errorf("at most 6 search domains")
		}
	}
	return dns, domains, nil
}

// JoinList writes a list as the panel passes it: comma-separated, "-" if empty.
func JoinList(l []string) string {
	if len(l) == 0 {
		return "-"
	}
	return strings.Join(l, ",")
}
