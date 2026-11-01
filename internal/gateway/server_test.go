package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"cirrusux/internal/snapshot"
)

const token = "0123456789abcdef0123456789abcdef"

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{AllowedUsers: []string{"owner@example.com"}, AgentToken: token}
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	s := NewServer(cfg, store, NewPower("cloud64", nil, false), web)
	return s, s.Handler()
}

func do(h http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var owner = map[string]string{"Tailscale-User-Login": "Owner@Example.com"}

func TestIdentityRequired(t *testing.T) {
	_, h := newTestServer(t)
	if w := do(h, "GET", "/api/state", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("no identity: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", map[string]string{"Tailscale-User-Login": "someone@else.com"}); w.Code != http.StatusForbidden {
		t.Errorf("wrong user: %d", w.Code)
	}
	if w := do(h, "GET", "/", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("UI without identity: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", owner); w.Code != http.StatusOK {
		t.Errorf("owner: %d", w.Code)
	}
	if w := do(h, "GET", "/", "", owner); w.Code != http.StatusOK || w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("UI: %d, CSP %q", w.Code, w.Header().Get("Content-Security-Policy"))
	}
}

func TestCSRF(t *testing.T) {
	_, h := newTestServer(t)
	if w := do(h, "POST", "/api/wake", "", owner); w.Code != http.StatusForbidden {
		t.Errorf("POST without X-Cirrus-Request: %d", w.Code)
	}
	hdr := map[string]string{"Tailscale-User-Login": "owner@example.com", "X-Cirrus-Request": "1", "Origin": "https://evil.example"}
	if w := do(h, "POST", "/api/wake", "", hdr); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d", w.Code)
	}
	hdr["Origin"] = "http://example.com" // httptest's Host
	if w := do(h, "POST", "/api/wake", "", hdr); w.Code != http.StatusAccepted {
		t.Errorf("same-origin wake: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/wake", "", hdr); w.Code != http.StatusConflict {
		t.Errorf("second wake while waking: %d", w.Code)
	}
}

func TestAgentToken(t *testing.T) {
	s, h := newTestServer(t)
	body, _ := json.Marshal(snapshot.Snapshot{Host: "cloud64", TakenAt: time.Now()})
	if w := do(h, "POST", "/agent/snapshot", string(body), map[string]string{"Authorization": "Bearer wrong"}); w.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d", w.Code)
	}
	if s.store.Snapshot() != nil {
		t.Fatal("snapshot stored with bad token")
	}
	if w := do(h, "POST", "/agent/snapshot", string(body), map[string]string{"Authorization": "Bearer " + token}); w.Code != http.StatusNoContent {
		t.Errorf("good token: %d", w.Code)
	}
	if s.store.Snapshot() == nil {
		t.Error("snapshot not stored")
	}
}

func TestUIVersioning(t *testing.T) {
	_, h := newTestServer(t)
	hdr := map[string]string{"Tailscale-User-Login": "owner@example.com", "X-Cirrus-Request": "1"}
	if w := do(h, "PUT", "/api/ui", `{"baseVersion":0,"data":{"a":1}}`, hdr); w.Code != http.StatusOK {
		t.Fatalf("first save: %d", w.Code)
	}
	if w := do(h, "PUT", "/api/ui", `{"baseVersion":0,"data":{"a":2}}`, hdr); w.Code != http.StatusConflict {
		t.Errorf("stale save: %d", w.Code)
	}
	if w := do(h, "PUT", "/api/ui", `{"baseVersion":1,"data":{"a":3}}`, hdr); w.Code != http.StatusOK {
		t.Errorf("second save: %d", w.Code)
	}
	w := do(h, "GET", "/api/ui", "", owner)
	var doc UIDoc
	_ = json.Unmarshal(w.Body.Bytes(), &doc)
	if doc.Version != 2 || string(doc.Data) != `{"a":3}` {
		t.Errorf("doc = %+v %s", doc, doc.Data)
	}
}

func TestPowerView(t *testing.T) {
	now := time.Now()
	fresh := &snapshot.Snapshot{TakenAt: now.Add(-2 * time.Minute), Power: snapshot.Power{Since: now.Add(-time.Hour)}}
	stale := &snapshot.Snapshot{TakenAt: now.Add(-40 * time.Minute)}

	p := NewPower("cloud64", nil, true)
	if v := p.View(now, fresh); v.State != "awake" {
		t.Errorf("no tailscale info, fresh snapshot: %s", v.State)
	}
	if v := p.View(now, stale); v.State != "asleep" {
		t.Errorf("no tailscale info, stale snapshot: %s", v.State)
	}
	p.tsKnown, p.online = true, true
	if v := p.View(now, stale); v.State != "unreachable" {
		t.Errorf("online but agent silent: %s", v.State)
	}
	p.online = false
	if v := p.View(now, fresh); v.State != "asleep" {
		t.Errorf("peer offline: %s", v.State)
	}
	p.waking = &wakeState{started: now.Add(-time.Minute), step: 2}
	if v := p.View(now, stale); v.State != "waking" || v.WakeStep != 2 {
		t.Errorf("waking: %+v", v)
	}
	p.online = true
	after := &snapshot.Snapshot{TakenAt: now, Power: snapshot.Power{Since: now}}
	if v := p.View(now, after); v.State != "awake" || p.waking != nil {
		t.Errorf("snapshot after wake should finish it: %+v", v)
	}
	p.waking = &wakeState{started: now.Add(-8 * time.Minute), step: 1}
	if v := p.View(now, stale); v.State != "awake" && v.WakeErr == "" {
		t.Errorf("timed-out wake should report an error: %+v", v)
	}
}

func TestParseStatusSelfOffline(t *testing.T) {
	up := []byte(`{"BackendState":"Running","Self":{"Online":true},"Peer":{"k":{"HostName":"cloud64","Online":false}}}`)
	if st := parseStatus(up, "cloud64"); !st.selfOK || !st.found || st.online {
		t.Errorf("connected: %+v", st)
	}
	down := []byte(`{"BackendState":"Running","Self":{"Online":false},"Peer":{"k":{"HostName":"cloud64","Online":false}}}`)
	if st := parseStatus(down, "cloud64"); st.selfOK {
		t.Errorf("disconnected cloud-box should not be trusted: %+v", st)
	}
}

func TestFindPeer(t *testing.T) {
	js := []byte(`{"Peer":{"nodekey:a":{"HostName":"tablet","Online":true},"nodekey:b":{"HostName":"localhost","DNSName":"cloud64.tail1234.ts.net.","Online":false,"LastSeen":"2026-11-01T19:56:00Z"}}}`)
	online, seen, found := findPeer(js, "cloud64")
	if !found || online || seen.IsZero() {
		t.Errorf("found=%v online=%v seen=%v", found, online, seen)
	}
}
