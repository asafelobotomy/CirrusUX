package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectWakeMAC(t *testing.T) {
	dir := t.TempDir()
	route := filepath.Join(dir, "route")
	os.WriteFile(route, []byte("Iface\tDestination\tGateway\tFlags\nenp1s0\t00000000\t0100A8C0\t0003\n"), 0o644)
	for n, a := range map[string]string{"enp1s0": "aa:bb:cc:dd:ee:ff", "eth9": "aa:bb:cc:00:00:09"} {
		os.MkdirAll(filepath.Join(dir, "net", n), 0o755)
		os.WriteFile(filepath.Join(dir, "net", n, "address"), []byte(a+"\n"), 0o644)
	}
	c := &Collector{RoutePath: route, NetSysPath: filepath.Join(dir, "net")}
	if s := c.Collect(context.Background()); s.WakeIface != "enp1s0" || s.WakeMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("default route: %q %q", s.WakeIface, s.WakeMAC)
	}
	c.WakeIface = "eth9"
	if s := c.Collect(context.Background()); s.WakeIface != "eth9" || s.WakeMAC != "aa:bb:cc:00:00:09" {
		t.Errorf("CIRRUS_WAKE_IFACE: %q %q", s.WakeIface, s.WakeMAC)
	}
}
