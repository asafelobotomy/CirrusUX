package gateway

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// The gateway believes an identity header from a trusted proxy, usually
// 127.0.0.1. A web page whose name an attacker points at 127.0.0.1 (DNS
// rebinding) can reach the gateway from a browser on the same machine as a
// same-origin page, set that header itself and read the answers. Its
// requests carry the attacker's name in Host, so the panel answers only to
// names it knows: those a rebinding page can't use.

// hostAllowed reports whether a request's Host is one the panel answers on.
func (s *Server) hostAllowed(hostport string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(strings.ToLower(strings.Trim(h, "[]")), ".")
	switch {
	case h == "":
		return false
	case isIPLiteral(h), h == "localhost":
		// A rebinding page always has a name; an address is reached as itself.
		return true
	case !strings.Contains(h, "."):
		// MagicDNS and LAN short names: nobody outside can hand these out.
		return true
	case strings.HasSuffix(h, ".ts.net"):
		// Tailscale's names: only Tailscale gives them out.
		return true
	}
	for _, a := range s.knownHosts() {
		if h == a || strings.HasPrefix(a, ".") && strings.HasSuffix(h, a) {
			return true
		}
	}
	// A reverse proxy's own name can be anything; until CIRRUS_HOSTS lists
	// it, header mode keeps working as before (the gateway says so at start).
	return s.cfg.Auth == "header" && len(s.cfg.Hosts) == 0
}

func isIPLiteral(h string) bool {
	_, err := netip.ParseAddr(h)
	return err == nil
}

// knownHosts are the names set in the config: CIRRUS_HOSTS, the Git clone
// address and this machine's own name.
func (s *Server) knownHosts() []string {
	out := append([]string{}, s.cfg.Hosts...)
	if u, err := url.Parse(s.cfg.GitURL); err == nil && u.Hostname() != "" {
		out = append(out, strings.ToLower(u.Hostname()))
	}
	if g := strings.ToLower(s.cfg.GatewayName); g != "" {
		out = append(out, g)
	}
	return out
}

// hostGate refuses requests for a name the panel doesn't answer on.
func (s *Server) hostGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			slog.Warn("refused a request for an unknown name", "host", r.Host)
			http.Error(w, "CirrusUX doesn't answer on this address. Add its name to CIRRUS_HOSTS in gateway.env.", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// warnOpenHosts says once, at start, when header mode answers on any name.
func (s *Server) warnOpenHosts() {
	if s.cfg.Auth == "header" && len(s.cfg.Hosts) == 0 && !s.cfg.Dev {
		slog.Warn("CIRRUS_HOSTS is empty: the panel answers on any address name. Set it to your reverse proxy's name to block DNS-rebinding pages")
	}
}
