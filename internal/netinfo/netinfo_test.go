package netinfo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefaultGateway(t *testing.T) {
	route := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\n" +
		"enp1s0\t0002000A\t00000000\t0001\t0\t0\t0\t00FFFFFF\n" +
		"enp1s0\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\n"
	iface, gw := DefaultGateway(route)
	if iface != "enp1s0" || gw != "192.0.2.1" {
		t.Errorf("got %s %s", iface, gw)
	}
	if i, g := DefaultGateway("Iface\tDestination\n"); i != "" || g != "" {
		t.Errorf("no route: %s %s", i, g)
	}
}

func TestParseResolv(t *testing.T) {
	dns, search := ParseResolv("# generated\nnameserver 192.0.2.53\nnameserver 192.0.2.53\nnameserver fe80::1\nnameserver bogus\nsearch lan example.ts.net\n")
	if !slices.Equal(dns, []string{"192.0.2.53", "fe80::1"}) || !slices.Equal(search, []string{"lan", "example.ts.net"}) {
		t.Errorf("got %v %v", dns, search)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	route := filepath.Join(dir, "route")
	resolv := filepath.Join(dir, "resolv")
	os.WriteFile(route, []byte("Iface\tDestination\tGateway\tFlags\nlo\t00000000\t010200C0\t0003\n"), 0o644)
	os.WriteFile(resolv, []byte("nameserver 192.0.2.53\n"), 0o644)
	info := Read(Paths{Sys: filepath.Join(dir, "sys"), Route: route, Resolv: resolv})
	if info.Gateway != "192.0.2.1" || len(info.DNS) != 1 {
		t.Errorf("got %+v", info)
	}
	// Whatever this machine has, loopback is labelled as such.
	for _, f := range info.Interfaces {
		if f.Name == "lo" && f.Kind != "loopback" {
			t.Errorf("lo is %s", f.Kind)
		}
	}
	if !hidden("veth12ab") || hidden("enp1s0") || !hidden("br-0123456789ab") {
		t.Error("hidden() wrong")
	}
}

func TestParseDNS(t *testing.T) {
	dns, dom, err := ParseDNS("192.0.2.53, 2001:db8::53", "Lan,example.ts.net")
	if err != nil || !slices.Equal(dns, []string{"192.0.2.53", "2001:db8::53"}) || !slices.Equal(dom, []string{"lan", "example.ts.net"}) {
		t.Errorf("got %v %v %v", dns, dom, err)
	}
	if dns, dom, err := ParseDNS("-", "-"); err != nil || dns != nil || dom != nil {
		t.Errorf("reset: %v %v %v", dns, dom, err)
	}
	for _, bad := range []string{"", "127.0.0.1", "0.0.0.0", "224.0.0.1", "example.com", "fe80::1%eth0", "192.0.2.1,192.0.2.2,192.0.2.3,192.0.2.4", "192.0.2.1;rm"} {
		if _, _, err := ParseDNS(bad, "-"); err == nil {
			t.Errorf("servers %q accepted", bad)
		}
	}
	for _, bad := range []string{"lan lan", "-x.com", "a..b", "x\nDNS=1.1.1.1", strings.Repeat("a", 64) + ".com"} {
		if _, _, err := ParseDNS("192.0.2.1", bad); err == nil {
			t.Errorf("search %q accepted", bad)
		}
	}
	if JoinList(nil) != "-" || JoinList([]string{"a", "b"}) != "a,b" {
		t.Error("JoinList")
	}
}
