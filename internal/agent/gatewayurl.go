package agent

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// The agent's token lets whoever holds it feed the panel and collect its
// commands, so it only travels where nobody else can read it: over HTTPS,
// or over plain HTTP to this machine or across the tailnet (WireGuard
// encrypts that).

// tailnet holds Tailscale's addresses.
var tailnet = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// CheckGatewayURL refuses a gateway address that would send the token in
// the clear across a network.
func CheckGatewayURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("CIRRUS_GATEWAY_URL %q isn't an http(s):// address", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
	default:
		return fmt.Errorf("CIRRUS_GATEWAY_URL %q isn't an http(s):// address", raw)
	}
	if u.User != nil {
		return errors.New("CIRRUS_GATEWAY_URL must not carry a user or password")
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".ts.net") {
		return nil
	}
	if a, err := netip.ParseAddr(h); err == nil {
		a = a.Unmap()
		if a.IsLoopback() || tailnet[0].Contains(a) || tailnet[1].Contains(a) {
			return nil
		}
	}
	return fmt.Errorf("CIRRUS_GATEWAY_URL %q would send the agent's token unencrypted: use https://, or http:// only to 127.0.0.1 or a Tailscale address", raw)
}

// noRedirects keeps the token on the address it was set up for: Go would
// send it along a redirect to the same host, even one down to plain HTTP.
func noRedirects(*http.Request, []*http.Request) error {
	return errors.New("the gateway answered with a redirect; set CIRRUS_GATEWAY_URL to the final address")
}
