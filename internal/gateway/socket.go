package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// With CIRRUS_SOCKET set, the panel is served on a Unix socket that only
// the front end can open (root's tailscaled, or a reverse proxy in
// CIRRUS_SOCKET_GROUP), and only requests that came through it are signed
// in. Any other program on this machine can still open 127.0.0.1, but
// there it finds only the agent's routes, which need the agent's token: it
// can no longer pose as a signed-in person by setting the identity header.

type socketKey struct{}

// viaSocket reports a request that came through the panel's socket.
func viaSocket(r *http.Request) bool {
	v, _ := r.Context().Value(socketKey{}).(bool)
	return v
}

// SocketHandler serves the whole panel to the front end on the socket. A
// front end on a socket can't send the name it was asked for as Host
// (`tailscale serve` sends "localhost"), so X-Forwarded-Host, which only a
// front end can set here, stands in for it, and X-Forwarded-For for the
// caller's address.
func (s *Server) SocketHandler() http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), socketKey{}, true))
		if fh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fh != "" {
			r.Host = fh
		}
		if ff, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ","); ff != "" {
			if a, err := netip.ParseAddr(strings.TrimSpace(ff)); err == nil {
				r.RemoteAddr = netip.AddrPortFrom(a, 0).String()
			}
		}
		h.ServeHTTP(w, r)
	})
}

// AgentHandler is what CIRRUS_LISTEN serves once the panel is on the
// socket: the agent's routes, which sign in with its token, and nothing
// that believes an identity header.
func (s *Server) AgentHandler() http.Handler {
	full := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/agent/") {
			http.Error(w, "the panel is served on CIRRUS_SOCKET, through tailscale serve or your reverse proxy", http.StatusNotFound)
			return
		}
		full.ServeHTTP(w, r)
	})
}

// ListenSocket opens the panel's socket: readable and writable by this
// user and group (CIRRUS_SOCKET_GROUP, or this user's own), nobody else.
// Root's tailscaled opens it whatever its mode.
func ListenSocket(path, group string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode().Type() != os.ModeSocket {
			return nil, fmt.Errorf("CIRRUS_SOCKET %s exists and isn't a socket", path)
		}
		_ = os.Remove(path) // left by a gateway that didn't stop cleanly
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("CIRRUS_SOCKET: its folder is missing (systemd's RuntimeDirectory= makes /run/cirrusux-gateway): %w", err)
	}
	gid := -1
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return nil, fmt.Errorf("CIRRUS_SOCKET_GROUP: %w", err)
		}
		if gid, err = strconv.Atoi(g.Gid); err != nil {
			return nil, fmt.Errorf("CIRRUS_SOCKET_GROUP %s: %w", group, err)
		}
	}
	// Made with no access for others at all, so there's no moment when
	// anyone else could connect.
	old := umask(0o117)
	ln, err := net.Listen("unix", path)
	umask(old)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		if err := os.Chown(path, -1, gid); err != nil {
			ln.Close()
			return nil, fmt.Errorf("CIRRUS_SOCKET_GROUP %s: %w (the gateway's user must be in that group)", group, err)
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

var errSocketPath = errors.New("CIRRUS_SOCKET must be an absolute path, e.g. /run/cirrusux-gateway/gateway.sock")
