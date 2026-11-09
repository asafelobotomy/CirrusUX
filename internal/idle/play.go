package idle

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Flow is one tracked TCP connection, as conntrack sees it: who opened it,
// to which port, and how many bytes went back to them.
type Flow struct {
	Key   string // the original direction's addresses and ports
	Peer  netip.Addr
	Port  int
	Bytes int64 // reply direction: from the server to the client
}

// ParseFlows reads `conntrack -L -p tcp` output. The bytes are there only
// with accounting on (net.netfilter.nf_conntrack_acct=1, which the sleep
// module sets); without it every flow has 0 and none reaches a threshold.
func ParseFlows(out string) []Flow {
	var flows []Flow
	for _, l := range lines(out) {
		kv := map[string][]string{}
		for _, f := range strings.Fields(l) {
			if k, v, ok := strings.Cut(f, "="); ok {
				kv[k] = append(kv[k], v)
			}
		}
		src, dst, sport, dport, bytes := kv["src"], kv["dst"], kv["sport"], kv["dport"], kv["bytes"]
		if len(src) < 1 || len(dst) < 1 || len(sport) < 1 || len(dport) < 1 {
			continue
		}
		peer, err1 := netip.ParseAddr(src[0])
		port, err2 := strconv.Atoi(dport[0])
		if err1 != nil || err2 != nil {
			continue
		}
		var n int64
		if len(bytes) >= 2 {
			n, _ = strconv.ParseInt(bytes[1], 10, 64)
		}
		key := src[0] + " " + sport[0] + " " + dst[0] + " " + dport[0]
		flows = append(flows, Flow{Key: key, Peer: peer.Unmap(), Port: port, Bytes: n})
	}
	return flows
}

// ignorePrefixes reads IgnoreClients: addresses or networks.
func ignorePrefixes(cfg Config) []netip.Prefix {
	var out []netip.Prefix
	for _, c := range cfg.IgnoreClients {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p)
		} else if a, err := netip.ParseAddr(c); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

func ignored(prefixes []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	return slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(a) })
}

// IgnoredPeers lists the ignored clients among the connections.
func IgnoredPeers(conns []Conn, cfg Config) []netip.Addr {
	ign := ignorePrefixes(cfg)
	var out []netip.Addr
	for _, c := range conns {
		if p := c.Peer.Unmap(); ignored(ign, p) && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// playState is the play-bytes file: each ignored client's flows and the
// bytes they had sent back at the last check.
type playState struct {
	At    int64            `json:"at"`
	Flows map[string]int64 `json:"flows"`
}

// playback names the ignored clients that pulled more than
// IgnorePlayMB megabytes (10^6 bytes) a minute since the last check: a TV
// that only holds a connection open stays ignored, one that streams counts.
// The first check has nothing to compare with, so it counts nothing.
func (c *Checker) playback(now time.Time, flows []Flow) []string {
	if c.Cfg.IgnorePlayMB <= 0 || len(c.Cfg.IgnoreClients) == 0 {
		return nil
	}
	ign := ignorePrefixes(c.Cfg)
	cur := playState{At: now.Unix(), Flows: map[string]int64{}}
	peerOf := map[string]netip.Addr{}
	for _, f := range flows {
		if ignored(ign, f.Peer) {
			cur.Flows[f.Key] = f.Bytes
			peerOf[f.Key] = f.Peer
		}
	}

	var prev playState
	b, err := os.ReadFile(filepath.Join(c.Dir, "play-bytes"))
	havePrev := err == nil && json.Unmarshal(b, &prev) == nil && prev.At > 0
	if !c.DryRun {
		if out, err := json.Marshal(cur); err == nil {
			_ = c.write("play-bytes", string(out))
		}
	}
	if !havePrev {
		return nil
	}

	minutes := float64(now.Unix()-prev.At) / 60
	if minutes < 1 {
		minutes = 1
	}
	sent := map[netip.Addr]int64{}
	for k, n := range cur.Flows {
		// A flow new since the last check sent all its bytes since then.
		if p, ok := prev.Flows[k]; ok && p <= n {
			n -= p
		}
		sent[peerOf[k]] += n
	}
	var out []string
	for peer, n := range sent {
		// Worded as the old idle checker did; the panel reads both the same.
		if mb := float64(n) / 1e6; mb/minutes > c.Cfg.IgnorePlayMB {
			out = append(out, fmt.Sprintf("playback by ignored client %s (%.0f MB sent since last check)", peer, mb))
		}
	}
	sort.Strings(out)
	return out
}
