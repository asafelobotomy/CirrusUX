package sysinfo

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Default places to read the network facts from. Tests point at fakes.
const (
	RoutePath  = "/proc/net/route"
	NetSysPath = "/sys/class/net"
)

// DefaultRouteIface is the interface of the IPv4 default route, read from
// the text of /proc/net/route. It is "" if there is none.
func DefaultRouteIface(route string) string {
	for i, line := range strings.Split(route, "\n") {
		f := strings.Fields(line)
		// Skip the header; columns are Iface, Destination, Gateway, Flags…
		if i == 0 || len(f) < 4 || f[1] != "00000000" {
			continue
		}
		// Flags bit 0 is RTF_UP.
		if flags, err := strconv.ParseUint(f[3], 16, 32); err != nil || flags&1 == 0 {
			continue
		}
		return f[0]
	}
	return ""
}

// WakeNIC names the interface the server is woken on and its MAC address.
// want is the owner's choice (CIRRUS_WAKE_IFACE); without one, the interface
// of the default route is used. The MAC comes from sysDir/<iface>/address,
// which needs no root. It returns empty strings if nothing usable is found
// (no interface, loopback, an empty or all-zero address).
func WakeNIC(want, routePath, sysDir string) (iface, mac string) {
	iface = strings.TrimSpace(want)
	if iface == "" {
		b, _ := os.ReadFile(routePath)
		iface = DefaultRouteIface(string(b))
	}
	// An interface name is one path element.
	if iface == "" || iface == "lo" || iface != filepath.Base(iface) || iface == "." || iface == ".." {
		return "", ""
	}
	b, err := os.ReadFile(filepath.Join(sysDir, iface, "address"))
	if err != nil {
		return iface, ""
	}
	hw, err := net.ParseMAC(strings.TrimSpace(string(b)))
	if err != nil || len(hw) != 6 {
		return iface, ""
	}
	zero := true
	for _, x := range hw {
		if x != 0 {
			zero = false
		}
	}
	if zero {
		return iface, ""
	}
	return iface, hw.String()
}
