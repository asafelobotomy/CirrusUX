// Package netinfo reads the server's network settings without root: its
// interfaces and addresses, the default gateway, DNS servers and Tailscale.
// The Network page shows them; nothing here changes them.
package netinfo

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Info is the server's network as the agent saw it.
type Info struct {
	Hostname   string  `json:"hostname"`
	Interfaces []Iface `json:"interfaces"`
	// Gateway is the IPv4 default route's next hop, and the interface it
	// goes out of.
	Gateway      string   `json:"gateway,omitempty"`
	GatewayIface string   `json:"gatewayIface,omitempty"`
	DNS          []string `json:"dns"`
	Search       []string `json:"search,omitempty"`
	// Tailscale is the tailnet address, when tailscale0 is up.
	Tailscale []string `json:"tailscale,omitempty"`
	// Containers counts the container links left out (veth*, docker0's
	// peers), so the list stays readable.
	Hidden int `json:"hidden,omitempty"`
}

// Iface is one network interface.
type Iface struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // ethernet, wifi, tailscale, bridge, vpn, loopback, virtual
	State string   `json:"state"`
	MAC   string   `json:"mac,omitempty"`
	MTU   int      `json:"mtu"`
	Mbps  int      `json:"mbps,omitempty"` // link speed, when the driver says
	Addrs []string `json:"addrs"`          // CIDR form
}

// Paths are where the facts come from; tests point them at fakes.
type Paths struct {
	Sys, Route, Resolv string
}

// DefaultPaths are the real ones.
var DefaultPaths = Paths{Sys: "/sys/class/net", Route: "/proc/net/route", Resolv: "/etc/resolv.conf"}

// Read gathers Info using the machine's interfaces and the files in p.
func Read(p Paths) Info {
	host, _ := os.Hostname()
	info := Info{Hostname: host, Interfaces: []Iface{}, DNS: []string{}}
	ifs, _ := net.Interfaces()
	for _, ni := range ifs {
		if hidden(ni.Name) {
			info.Hidden++
			continue
		}
		f := Iface{Name: ni.Name, MTU: ni.MTU, MAC: ni.HardwareAddr.String(), Addrs: []string{}}
		f.Kind = kind(p.Sys, ni)
		f.State = strings.TrimSpace(readFile(filepath.Join(p.Sys, ni.Name, "operstate")))
		if f.State == "unknown" && ni.Flags&net.FlagUp != 0 {
			f.State = "up" // loopback and tunnels say unknown
		}
		if n, err := strconv.Atoi(strings.TrimSpace(readFile(filepath.Join(p.Sys, ni.Name, "speed")))); err == nil && n > 0 {
			f.Mbps = n
		}
		addrs, _ := ni.Addrs()
		for _, a := range addrs {
			f.Addrs = append(f.Addrs, a.String())
		}
		if f.Kind == "tailscale" {
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok {
					info.Tailscale = append(info.Tailscale, ipn.IP.String())
				}
			}
		}
		info.Interfaces = append(info.Interfaces, f)
	}
	info.GatewayIface, info.Gateway = DefaultGateway(readFile(p.Route))
	info.DNS, info.Search = ParseResolv(readFile(p.Resolv))
	return info
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// hidden leaves out the per-container links Docker and Podman make.
func hidden(name string) bool {
	return strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") && len(name) == 15
}

func kind(sys string, ni net.Interface) string {
	switch {
	case ni.Flags&net.FlagLoopback != 0:
		return "loopback"
	case ni.Name == "tailscale0" || strings.HasPrefix(ni.Name, "ts"):
		return "tailscale"
	case strings.HasPrefix(ni.Name, "wg") || strings.HasPrefix(ni.Name, "tun"):
		return "vpn"
	}
	if _, err := os.Stat(filepath.Join(sys, ni.Name, "wireless")); err == nil {
		return "wifi"
	}
	if _, err := os.Stat(filepath.Join(sys, ni.Name, "bridge")); err == nil {
		return "bridge"
	}
	if _, err := os.Stat(filepath.Join(sys, ni.Name, "device")); err == nil {
		return "ethernet"
	}
	return "virtual"
}

// DefaultGateway reads the IPv4 default route from /proc/net/route: its
// interface and next hop.
func DefaultGateway(route string) (iface, gw string) {
	for i, line := range strings.Split(route, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 4 || f[1] != "00000000" {
			continue
		}
		if flags, err := strconv.ParseUint(f[3], 16, 32); err != nil || flags&1 == 0 {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			return f[0], ""
		}
		// The kernel writes it in host order, little-endian on every
		// machine CirrusUX runs on.
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], binary.LittleEndian.Uint32(b))
		if ip == [4]byte{} {
			return f[0], ""
		}
		return f[0], netip.AddrFrom4(ip).String()
	}
	return "", ""
}

// ParseResolv reads nameserver and search lines. systemd-resolved's stub
// (127.0.0.53) is kept as it is: the page says what it is.
func ParseResolv(text string) (dns, search []string) {
	dns = []string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "nameserver":
			if _, err := netip.ParseAddr(f[1]); err == nil && !slices.Contains(dns, f[1]) {
				dns = append(dns, f[1])
			}
		case "search", "domain":
			search = append(search, f[1:]...)
		}
	}
	return dns, search
}
