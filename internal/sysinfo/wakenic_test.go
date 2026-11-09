package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

const route = "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
	"docker0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n" +
	"enp1s0\t00000000\t0100A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
	"enp1s0\t0000A8C0\t00000000\t0001\t0\t0\t100\t0000FFFF\t0\t0\t0\n"

func fakeNet(t *testing.T, addrs map[string]string) (routeFile, sys string) {
	t.Helper()
	dir := t.TempDir()
	routeFile = filepath.Join(dir, "route")
	if err := os.WriteFile(routeFile, []byte(route), 0o644); err != nil {
		t.Fatal(err)
	}
	sys = filepath.Join(dir, "net")
	for n, a := range addrs {
		if err := os.MkdirAll(filepath.Join(sys, n), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sys, n, "address"), []byte(a+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return routeFile, sys
}

func TestDefaultRouteIface(t *testing.T) {
	if got := DefaultRouteIface(route); got != "enp1s0" {
		t.Errorf("got %q", got)
	}
	if got := DefaultRouteIface("Iface\tDestination\tGateway\tFlags\n"); got != "" {
		t.Errorf("no route: %q", got)
	}
	down := "Iface\tDestination\tGateway\tFlags\nenp1s0\t00000000\t0100A8C0\t0002\n"
	if got := DefaultRouteIface(down); got != "" {
		t.Errorf("route not up: %q", got)
	}
}

func TestWakeNIC(t *testing.T) {
	r, sys := fakeNet(t, map[string]string{
		"enp1s0": "aa:bb:cc:dd:ee:ff", "wlan0": "AA:BB:CC:00:00:01",
		"lo": "00:00:00:00:00:00", "dummy0": "00:00:00:00:00:00", "bad0": "xyz",
	})
	if i, m := WakeNIC("", r, sys); i != "enp1s0" || m != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("default route: %q %q", i, m)
	}
	// CIRRUS_WAKE_IFACE wins over the route.
	if i, m := WakeNIC("wlan0", r, sys); i != "wlan0" || m != "aa:bb:cc:00:00:01" {
		t.Errorf("override: %q %q", i, m)
	}
	for _, name := range []string{"lo", "dummy0", "bad0", "missing0", "../net/wlan0"} {
		if _, m := WakeNIC(name, r, sys); m != "" {
			t.Errorf("%s: want no MAC, got %q", name, m)
		}
	}
	if i, m := WakeNIC("", filepath.Join(sys, "none"), sys); i != "" || m != "" {
		t.Errorf("no route file: %q %q", i, m)
	}
}
