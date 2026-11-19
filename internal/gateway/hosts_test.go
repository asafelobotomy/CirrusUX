package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doHost(h http.Handler, host, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A DNS-rebinding page reaches the gateway under the attacker's own name,
// with any identity header it likes; the panel answers only to names it knows.
func TestHostGate(t *testing.T) {
	_, h := newTestServer(t)
	for _, host := range []string{"gw.tail1234.ts.net", "GW.Tail1234.TS.NET.", "127.0.0.1:8064", "[::1]:8064", "100.101.102.103",
		"localhost:8064", "gw", "atlas:443", "example.com", "example.com:8443"} {
		if w := doHost(h, host, "/api/state", owner); w.Code != http.StatusOK {
			t.Errorf("%s: %d", host, w.Code)
		}
	}
	for _, host := range []string{"rebind.attacker.example:8064", "127.0.0.1.nip.io", "evil.ts.net.attacker.example", "", "x.local"} {
		if w := doHost(h, host, "/api/state", owner); w.Code != http.StatusMisdirectedRequest || !strings.Contains(w.Body.String(), "CIRRUS_HOSTS") {
			t.Errorf("%s: %d %s", host, w.Code, w.Body)
		}
		if w := doHost(h, host, "/", owner); w.Code != http.StatusMisdirectedRequest {
			t.Errorf("UI on %s: %d", host, w.Code)
		}
	}
	// The agent signs in with its token, not a header: no name needed.
	if w := doHost(h, "rebind.attacker.example", "/agent/commands", map[string]string{"Authorization": "Bearer wrong"}); w.Code != http.StatusUnauthorized {
		t.Errorf("agent route: %d", w.Code)
	}
}

func TestHostGateSubdomainsAndGitURL(t *testing.T) {
	cfg := testConfig()
	cfg.Hosts = []string{".example.org"}
	cfg.GitURL = "https://git.example.net"
	_, h := newTestServerWith(t, cfg)
	for host, want := range map[string]int{"panel.example.org": 200, "a.b.example.org": 200, "example.org.evil": 421, "git.example.net": 200, "example.com": 421} {
		if w := doHost(h, host, "/api/state", owner); w.Code != want {
			t.Errorf("%s: %d, want %d", host, w.Code, want)
		}
	}
}

// Header mode used to answer on any name; until CIRRUS_HOSTS is set it
// still does, so an upgrade doesn't lock anyone out.
func TestHostGateHeaderModeWithoutHosts(t *testing.T) {
	cfg := testConfig()
	cfg.Auth, cfg.Hosts = "header", nil
	_, h := newTestServerWith(t, cfg)
	if w := doHost(h, "panel.example.org", "/api/state", map[string]string{"Remote-User": "owner@example.com"}); w.Code != 200 {
		t.Errorf("header mode, no CIRRUS_HOSTS: %d", w.Code)
	}
	cfg.Hosts = []string{"panel.example.org"}
	_, h = newTestServerWith(t, cfg)
	if w := doHost(h, "other.example.org", "/api/state", map[string]string{"Remote-User": "owner@example.com"}); w.Code != 421 {
		t.Errorf("header mode, not listed: %d", w.Code)
	}
}

func TestDevNeedsLoopback(t *testing.T) {
	t.Setenv("CIRRUS_AGENT_TOKEN", strings.Repeat("a", 32))
	t.Setenv("CIRRUS_DEV", "1")
	t.Setenv("CIRRUS_TZ", "UTC")
	for listen, ok := range map[string]bool{"127.0.0.1:8064": true, "[::1]:8064": true, "localhost:8064": true, "0.0.0.0:8064": false, ":8064": false, "192.0.2.5:8064": false} {
		t.Setenv("CIRRUS_LISTEN", listen)
		_, err := ConfigFromEnv()
		if (err == nil) != ok {
			t.Errorf("%s: %v", listen, err)
		}
	}
}

func TestHostList(t *testing.T) {
	got := hostList("Panel.Example.org:443, .example.net. ,")
	if strings.Join(got, "|") != "panel.example.org|.example.net" {
		t.Errorf("hostList: %q", got)
	}
}
