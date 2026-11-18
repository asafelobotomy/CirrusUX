package gateway

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func socketServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := testConfig()
	// A short folder: socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg.Socket = filepath.Join(dir, "gw.sock")
	s, _ := newTestServerWith(t, cfg)
	ln, err := ListenSocket(cfg.Socket, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s.SocketHandler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return s, cfg.Socket
}

func unixClient(path string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
}

// `tailscale serve` sends Host: localhost to a socket, with the real name
// in X-Forwarded-Host.
func TestSocketSignsIn(t *testing.T) {
	_, path := socketServer(t)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o660 || fi.Mode().Type() != os.ModeSocket {
		t.Fatalf("socket: %v %v", fi.Mode(), err)
	}
	c := unixClient(path)
	req, _ := http.NewRequest("GET", "http://localhost/api/state", nil)
	req.Header.Set("Tailscale-User-Login", "owner@example.com")
	req.Header.Set("X-Forwarded-Host", "gw.tail1234.ts.net")
	if resp, err := c.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("state over the socket: %v %v", resp, err)
	}
	req, _ = http.NewRequest("POST", "http://localhost/api/wake", nil)
	req.Header.Set("Tailscale-User-Login", "owner@example.com")
	req.Header.Set("X-Forwarded-Host", "gw.tail1234.ts.net")
	req.Header.Set("X-Cirrus-Request", "1")
	req.Header.Set("Origin", "https://gw.tail1234.ts.net")
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("same-origin wake over the socket: %v %v", resp, err)
	}
	req.Header.Set("X-Forwarded-Host", "rebind.attacker.example")
	req.Header.Set("Origin", "https://rebind.attacker.example")
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("unknown name over the socket: %v %v", resp, err)
	}
}

// Over TCP, even from a trusted proxy address, the identity header is no
// longer believed, and only the agent's routes answer.
func TestSocketTCPKeepsOnlyTheAgent(t *testing.T) {
	s, _ := socketServer(t)
	if w := do(s.Handler(), "GET", "/api/state", "", owner); w.Code != http.StatusForbidden {
		t.Errorf("identity over TCP: %d", w.Code)
	}
	h := s.AgentHandler()
	for _, p := range []string{"/api/state", "/", "/git/x.git/info/refs"} {
		if w := do(h, "GET", p, "", owner); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "CIRRUS_SOCKET") {
			t.Errorf("%s over TCP: %d", p, w.Code)
		}
	}
	if w := do(h, "GET", "/agent/commands", "", map[string]string{"Authorization": "Bearer wrong"}); w.Code != http.StatusUnauthorized {
		t.Errorf("agent over TCP: %d", w.Code)
	}
}

func TestListenSocketRefusesAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "not-a-socket")
	os.WriteFile(p, []byte("x"), 0o600)
	if _, err := ListenSocket(p, ""); err == nil {
		t.Error("replaced a file")
	}
	if _, err := ListenSocket(filepath.Join(t.TempDir(), "missing", "gw.sock"), ""); err == nil {
		t.Error("no folder, no error")
	}
}
