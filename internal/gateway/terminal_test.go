//go:build linux

package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func terminalServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.DataDir = t.TempDir()
	cfg.Terminal = TerminalConfig{On: true, Host: "homeserver", Port: 22}
	cfg.TrustedProxies = append(cfg.TrustedProxies, netip.MustParsePrefix("127.0.0.1/32"))
	s := NewServer(cfg, store, testPower(), fstest.MapFS{})
	s.termCmd = func(kind, user string) []string {
		return []string{"/bin/sh", "-c", "echo ready " + kind + " " + user + "; exec cat"}
	}
	if err := store.SaveSnapshot(awakeSnap()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); s.Close() })
	return s, ts
}

func call(t *testing.T, ts *httptest.Server, method, path, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// readUntil reads the stream until it has seen want.
func readUntil(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	var got strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		for !strings.Contains(got.String(), want) {
			n, err := r.Read(buf)
			got.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("waited for %q, got %q", want, got.String())
	}
	if !strings.Contains(got.String(), want) {
		t.Fatalf("stream ended without %q: %q", want, got.String())
	}
}

func TestTerminalSession(t *testing.T) {
	s, ts := terminalServer(t)
	// Without the request header, even a read is refused.
	if res := call(t, ts, "GET", "/api/terminal", "", owner); res.StatusCode != http.StatusForbidden {
		t.Errorf("no header: %d", res.StatusCode)
	}
	for _, body := range []string{`{"user":"-oProxyCommand=x"}`, `{"user":"alice","kind":"telnet"}`, `{"user":""}`} {
		if res := call(t, ts, "POST", "/api/terminal", body, actor); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", body, res.StatusCode)
		}
	}
	res := call(t, ts, "POST", "/api/terminal", `{"user":"alice","kind":"ssh","cols":100,"rows":30}`, actor)
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("open: %d %s", res.StatusCode, b)
	}
	var opened struct{ ID string }
	json.NewDecoder(res.Body).Decode(&opened)

	out := call(t, ts, "GET", "/api/terminal/"+opened.ID+"/output", "", actor)
	if out.StatusCode != http.StatusOK {
		t.Fatalf("output: %d", out.StatusCode)
	}
	r := bufio.NewReader(out.Body)
	readUntil(t, r, "ready ssh alice")
	// A second reader is turned away while the first is open.
	if res := call(t, ts, "GET", "/api/terminal/"+opened.ID+"/output", "", actor); res.StatusCode != http.StatusConflict {
		t.Errorf("second reader: %d", res.StatusCode)
	}
	if res := call(t, ts, "POST", "/api/terminal/"+opened.ID+"/input", "hello there\n", actor); res.StatusCode != http.StatusNoContent {
		t.Fatalf("input: %d", res.StatusCode)
	}
	readUntil(t, r, "hello there")
	if res := call(t, ts, "POST", "/api/terminal/"+opened.ID+"/resize", `{"cols":120,"rows":40}`, actor); res.StatusCode != http.StatusNoContent {
		t.Errorf("resize: %d", res.StatusCode)
	}

	// Someone else's session isn't found.
	s.terms.mu.Lock()
	s.terms.sessions[opened.ID].owner = "someone@example.com"
	s.terms.mu.Unlock()
	if res := call(t, ts, "POST", "/api/terminal/"+opened.ID+"/input", "x", actor); res.StatusCode != http.StatusNotFound {
		t.Errorf("another person's session: %d", res.StatusCode)
	}
	s.terms.mu.Lock()
	s.terms.sessions[opened.ID].owner = "owner@example.com"
	s.terms.mu.Unlock()

	if res := call(t, ts, "DELETE", "/api/terminal/"+opened.ID, "", actor); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	io.Copy(io.Discard, r) // the stream ends
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.terms.mu.Lock()
		n := len(s.terms.sessions)
		s.terms.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session still open after close")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var opens, closes int
	for _, e := range s.store.RecentAudit(50) {
		opens += strings.Count(e.What, "Opened a terminal to atlas as alice")
		closes += strings.Count(e.What, "Closed a terminal to atlas as alice")
	}
	if opens != 1 || closes != 1 {
		t.Errorf("audit: %d opened, %d closed", opens, closes)
	}
}

func TestTerminalOff(t *testing.T) {
	_, h := newTestServer(t)
	if w := do(h, "POST", "/api/terminal", `{"user":"alice"}`, actor); w.Code != http.StatusNotFound {
		t.Errorf("terminal off: %d", w.Code)
	}
}

func TestTerminalArgv(t *testing.T) {
	s := &Server{cfg: Config{DataDir: "/var/lib/cirrusux-gateway", Terminal: TerminalConfig{Host: "homeserver", Port: 2222}}}
	ssh := strings.Join(s.terminalArgv("ssh", "alice"), " ")
	if !strings.HasPrefix(ssh, "ssh -F /dev/null ") || !strings.HasSuffix(ssh, "-p 2222 -l alice -- homeserver") || !strings.Contains(ssh, "StrictHostKeyChecking=ask") {
		t.Errorf("ssh: %s", ssh)
	}
	sftp := strings.Join(s.terminalArgv("sftp", "alice"), " ")
	if !strings.HasPrefix(sftp, "sftp -F /dev/null ") || !strings.HasSuffix(sftp, "-P 2222 -- alice@homeserver") {
		t.Errorf("sftp: %s", sftp)
	}
}
