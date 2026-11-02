package idle

import (
	"bufio"
	"fmt"
	"net/netip"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Activity is gathered from what the system reports; each source is a
// command's output, parsed here, so the parsing is tested without a system.

// Conn is one inbound TCP connection: a client talking to a local port.
type Conn struct {
	Port int
	Peer netip.Addr
}

// ParseSS reads `ss -Htn state established` and `ss -Htln`: established
// connections whose local port something listens on are inbound.
func ParseSS(established, listening string) []Conn {
	listen := map[int]bool{}
	for _, f := range lines(listening) {
		if fs := strings.Fields(f); len(fs) >= 4 {
			if _, p, ok := splitHostPort(fs[3]); ok {
				listen[p] = true
			}
		}
	}
	var out []Conn
	for _, f := range lines(established) {
		fs := strings.Fields(f)
		if len(fs) < 4 {
			continue
		}
		_, lp, ok1 := splitHostPort(fs[2])
		peer, _, ok2 := splitHostPort(fs[3])
		if ok1 && ok2 && listen[lp] {
			out = append(out, Conn{Port: lp, Peer: peer})
		}
	}
	return out
}

// ParseConntrack reads `conntrack -L -p tcp --state ESTABLISHED`. A
// connection whose reply comes from somewhere other than where it was sent
// was forwarded (DNAT) to a container's published port: that's someone
// using an app. Outbound connections from containers don't count.
func ParseConntrack(out string) []Conn {
	var conns []Conn
	for _, l := range lines(out) {
		kv := map[string][]string{}
		for _, f := range strings.Fields(l) {
			if k, v, ok := strings.Cut(f, "="); ok {
				kv[k] = append(kv[k], v)
			}
		}
		src, dst, dport := kv["src"], kv["dst"], kv["dport"]
		if len(src) < 2 || len(dst) < 1 || len(dport) < 1 || src[1] == dst[0] {
			continue
		}
		peer, err1 := netip.ParseAddr(src[0])
		port, err2 := strconv.Atoi(dport[0])
		if err1 == nil && err2 == nil {
			conns = append(conns, Conn{Port: port, Peer: peer})
		}
	}
	return conns
}

// ParseWho counts login sessions in `who` output.
func ParseWho(out string) int { return len(lines(out)) }

// ParseLoad reads the 5-minute average from /proc/loadavg.
func ParseLoad(s string) float64 {
	fs := strings.Fields(s)
	if len(fs) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(fs[1], 64)
	return v
}

// ParseActiveUnits reads `systemctl list-units --state=active --no-legend
// --plain` and returns the units matching any of the patterns.
func ParseActiveUnits(out string, patterns []string) []string {
	var hits []string
	for _, l := range lines(out) {
		fs := strings.Fields(l)
		if len(fs) == 0 {
			continue
		}
		for _, p := range patterns {
			if ok, _ := path.Match(p, fs[0]); ok {
				hits = append(hits, fs[0])
				break
			}
		}
	}
	return hits
}

// Counted drops connections that don't count: loopback, ignored clients,
// and ports outside WatchPorts when it's set.
func Counted(conns []Conn, cfg Config) []Conn {
	var ignore []netip.Prefix
	for _, c := range cfg.IgnoreClients {
		if p, err := netip.ParsePrefix(c); err == nil {
			ignore = append(ignore, p)
		} else if a, err := netip.ParseAddr(c); err == nil {
			ignore = append(ignore, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	var out []Conn
	for _, c := range conns {
		peer := c.Peer.Unmap()
		if peer.IsLoopback() || slices.ContainsFunc(ignore, func(p netip.Prefix) bool { return p.Contains(peer) }) {
			continue
		}
		if len(cfg.WatchPorts) > 0 && !slices.Contains(cfg.WatchPorts, c.Port) {
			continue
		}
		out = append(out, Conn{Port: c.Port, Peer: peer})
	}
	return out
}

// ConnReason formats connections the way the agent reads them:
// "host conns: 22<-192.0.2.5 8096<-192.0.2.9", one entry per port and peer.
func ConnReason(conns []Conn) string {
	seen := map[string]bool{}
	var parts []string
	for _, c := range conns {
		s := fmt.Sprintf("%d<-%s", c.Port, c.Peer)
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return "host conns: " + strings.Join(parts, " ")
}

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// splitHostPort reads ss's "addr:port", "[v6]:port" and "*:port".
func splitHostPort(s string) (netip.Addr, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return netip.Addr{}, 0, false
	}
	host := strings.Trim(s[:i], "[]")
	if h, _, ok := strings.Cut(host, "%"); ok {
		host = h // fe80::1%eth0
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, port, host == "*" || host == "0.0.0.0" || host == "::"
	}
	return a, port, true
}
