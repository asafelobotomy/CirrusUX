package idle

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseSS(t *testing.T) {
	listening := `LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*
LISTEN 0 4096 *:8096 *:*
LISTEN 0 4096 [::]:443 [::]:*
LISTEN 0 4096 127.0.0.1:8064 0.0.0.0:*`
	established := `0 0 192.0.2.2:22 192.0.2.5:51000
0 0 [::ffff:192.0.2.2]:8096 [::ffff:192.0.2.9]:40000
0 0 192.0.2.2:51234 198.51.100.7:443
0 0 127.0.0.1:8064 127.0.0.1:40500
0 0 [fd7a:115c:a1e0::1]:443 [fd7a:115c:a1e0::2]:6000`
	got := ParseSS(established, listening)
	want := []Conn{
		{22, netip.MustParseAddr("192.0.2.5")},
		{8096, netip.MustParseAddr("::ffff:192.0.2.9")},
		{8064, netip.MustParseAddr("127.0.0.1")},
		{443, netip.MustParseAddr("fd7a:115c:a1e0::2")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestParseConntrack(t *testing.T) {
	out := `tcp      6 431999 ESTABLISHED src=192.0.2.9 dst=192.0.2.2 sport=40000 dport=8096 src=172.17.0.2 dst=192.0.2.9 sport=8096 dport=40000 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=172.17.0.2 dst=198.51.100.7 sport=50000 dport=443 src=198.51.100.7 dst=192.0.2.2 sport=443 dport=50000 [ASSURED] mark=0 use=1
tcp      6 300 ESTABLISHED src=192.0.2.5 dst=192.0.2.2 sport=51000 dport=22 src=192.0.2.2 dst=192.0.2.5 sport=22 dport=51000 [ASSURED] mark=0 use=1`
	got := ParseConntrack(out)
	if len(got) != 1 || got[0].Port != 8096 || got[0].Peer.String() != "192.0.2.9" {
		t.Errorf("only the forwarded app connection counts: %v", got)
	}
}

// Podman (netavark): a tailnet client reaching Jellyfin through a proxy on
// the host arrives from loopback, and the container sees the bridge
// gateway. Both families are read; IPv6 lines look the same.
func TestParseConntrackPodman(t *testing.T) {
	out := `tcp      6 431999 ESTABLISHED src=100.64.0.7 dst=100.64.0.2 sport=40000 dport=8096 src=10.88.0.5 dst=100.64.0.7 sport=8096 dport=40000 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=127.0.0.1 dst=127.0.0.1 sport=41000 dport=8096 src=10.88.0.5 dst=10.88.0.1 sport=8096 dport=41000 [ASSURED] mark=0 use=1
tcp      6 431999 ESTABLISHED src=fd7a:115c:a1e0::7 dst=fd7a:115c:a1e0::2 sport=40001 dport=8096 src=fd00:88::5 dst=fd7a:115c:a1e0::7 sport=8096 dport=40001 [ASSURED] mark=0 use=1`
	got := ConnReason(Counted(ParseConntrack(out), Config{}))
	if got != "host conns: 8096<-10.88.0.1 8096<-100.64.0.7 8096<-fd7a:115c:a1e0::7" {
		t.Errorf("reason = %q", got)
	}
}

func TestParseFlows(t *testing.T) {
	out := `tcp      6 431999 ESTABLISHED src=192.0.2.23 dst=192.0.2.2 sport=40000 dport=8096 packets=9 bytes=600 src=10.88.0.5 dst=192.0.2.23 sport=8096 dport=40000 packets=90 bytes=7000000 [ASSURED] mark=0 use=1
tcp      6 300 ESTABLISHED src=192.0.2.5 dst=192.0.2.2 sport=51000 dport=22 src=192.0.2.2 dst=192.0.2.5 sport=22 dport=51000 [ASSURED] mark=0 use=1`
	got := ParseFlows(out)
	if len(got) != 2 || got[0].Bytes != 7000000 || got[0].Peer.String() != "192.0.2.23" || got[0].Port != 8096 || got[1].Bytes != 0 {
		t.Errorf("flows = %+v", got)
	}
	if got[0].Key == got[1].Key {
		t.Error("flows share a key")
	}
	conns := []Conn{{8096, netip.MustParseAddr("::ffff:192.0.2.23")}, {22, netip.MustParseAddr("192.0.2.5")}}
	if p := IgnoredPeers(conns, Config{IgnoreClients: []string{"192.0.2.23"}}); len(p) != 1 || p[0].String() != "192.0.2.23" {
		t.Errorf("ignored peers = %v", p)
	}
}

func TestCountedAndReason(t *testing.T) {
	conns := []Conn{
		{22, netip.MustParseAddr("192.0.2.5")},
		{22, netip.MustParseAddr("192.0.2.5")},
		{8096, netip.MustParseAddr("::ffff:192.0.2.9")},
		{8064, netip.MustParseAddr("127.0.0.1")},
		{8096, netip.MustParseAddr("192.0.2.23")},
	}
	cfg := Config{IgnoreClients: []string{"192.0.2.16/28"}}
	if r := ConnReason(Counted(conns, cfg)); r != "host conns: 22<-192.0.2.5 8096<-192.0.2.9" {
		t.Errorf("reason = %q", r)
	}
	cfg.WatchPorts = []int{22}
	if r := ConnReason(Counted(conns, cfg)); r != "host conns: 22<-192.0.2.5" {
		t.Errorf("watch ports: %q", r)
	}
	if ConnReason(nil) != "" {
		t.Error("no connections, no reason")
	}
}

func TestSmallParsers(t *testing.T) {
	if ParseWho("alice pts/0 2026-11-02 11:00 (192.0.2.5)\nalice pts/1 2026-11-02 11:05\n") != 2 {
		t.Error("who")
	}
	if ParseLoad("0.42 1.75 1.10 2/400 1234") != 1.75 {
		t.Error("load")
	}
	units := "cirrusux-backup.service loaded active running CirrusUX system backup\ncirrusux-job-ab12.service loaded active running job\nssh.service loaded active running OpenSSH"
	if got := ParseActiveUnits(units, DefaultConfig().BusyUnits); !reflect.DeepEqual(got, []string{"cirrusux-backup.service", "cirrusux-job-ab12.service"}) {
		t.Errorf("busy units = %v", got)
	}
}
