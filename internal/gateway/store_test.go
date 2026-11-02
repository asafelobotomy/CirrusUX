package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestAuditKeptInMemory(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	for i := range auditKeep + 5 {
		if err := s.Audit(AuditEntry{What: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.RecentAudit(3)
	if len(got) != 3 || got[0].What != fmt.Sprint(auditKeep+4) || got[2].What != fmt.Sprint(auditKeep+2) {
		t.Errorf("recent = %+v", got)
	}
	if n := len(s.RecentAudit(10_000)); n != auditKeep {
		t.Errorf("kept %d entries in memory, want %d", n, auditKeep)
	}
	// A restart reloads the newest entries from the file.
	s2, _ := OpenStore(dir)
	if got := s2.RecentAudit(1); len(got) != 1 || got[0].What != fmt.Sprint(auditKeep+4) {
		t.Errorf("after reopen = %+v", got)
	}
	if got := s2.RecentAudit(10_000); len(got) != auditKeep {
		t.Errorf("after reopen kept %d", len(got))
	}
}

func TestAuditRotation(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	_ = s.Audit(AuditEntry{What: "old"})
	// Pad the file past the limit.
	f, _ := os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.Write(bytes.Repeat([]byte("{}\n"), auditRotate/3+1))
	f.Close()

	if err := s.Audit(AuditEntry{What: "new"}); err != nil {
		t.Fatal(err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "audit-*.jsonl"))
	if len(archives) != 1 {
		t.Fatalf("archives = %v", archives)
	}
	if b, _ := os.ReadFile(archives[0]); !strings.Contains(string(b), `"old"`) {
		t.Error("the archive lost the old entries")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl")); strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), `"new"`) {
		t.Errorf("fresh audit.jsonl = %q", b)
	}
	if got := s.RecentAudit(2); len(got) != 2 || got[0].What != "new" || got[1].What != "old" {
		t.Errorf("recent after rotation = %+v", got)
	}
}

func TestUIHistoryPruned(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	for v := range uiHistory + 5 {
		if _, err := s.SaveUI(v, json.RawMessage(fmt.Sprintf(`{"n":%d}`, v))); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "ui-history", "*.json"))
	if len(files) != uiHistory {
		t.Fatalf("history files = %d, want %d", len(files), uiHistory)
	}
	// The oldest go first; the newest previous version is kept.
	if filepath.Base(files[0]) != fmt.Sprintf("%06d.json", 5) || filepath.Base(files[len(files)-1]) != fmt.Sprintf("%06d.json", uiHistory+4) {
		t.Errorf("kept %s … %s", filepath.Base(files[0]), filepath.Base(files[len(files)-1]))
	}
}

func TestWallpaper(t *testing.T) {
	_, h := newTestServer(t)
	put := map[string]string{"Tailscale-User-Login": "owner@example.com", "X-Cirrus-Request": "1"}
	if w := do(h, "GET", "/api/wallpaper", "", owner); w.Code != http.StatusNotFound {
		t.Errorf("before upload: %d", w.Code)
	}

	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	w := do(h, "PUT", "/api/wallpaper", png, put)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "savedAt") {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	w = do(h, "GET", "/api/wallpaper?v=1", "", owner)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Body.String() != png || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("get: %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("X-Content-Type-Options"))
	}

	avif := "\x00\x00\x00\x1cftypavif" + strings.Repeat("\x00", 32)
	if w := do(h, "PUT", "/api/wallpaper", avif, put); w.Code != http.StatusOK {
		t.Errorf("avif: %d %s", w.Code, w.Body)
	}

	for name, body := range map[string]string{
		"svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"html": "<!doctype html><script>alert(1)</script>",
		"big":  "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", maxWallpaper),
	} {
		if w := do(h, "PUT", "/api/wallpaper", body, put); w.Code < 400 {
			t.Errorf("%s accepted: %d", name, w.Code)
		}
	}
	// The AVIF upload is still the one served.
	if w := do(h, "GET", "/api/wallpaper", "", owner); w.Header().Get("Content-Type") != "image/avif" {
		t.Errorf("after refused uploads: %q", w.Header().Get("Content-Type"))
	}
	// Uploading needs the CSRF header like any other change.
	if w := do(h, "PUT", "/api/wallpaper", png, owner); w.Code != http.StatusForbidden {
		t.Errorf("without X-Cirrus-Request: %d", w.Code)
	}
}

// The digest sends nothing to xAI unless CIRRUS_DIGEST_GROK is set.
func TestDigestGrokOptIn(t *testing.T) {
	var calls atomic.Int32
	xai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"All fine."}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer xai.Close()

	for _, optIn := range []bool{false, true} {
		store, _ := OpenStore(t.TempDir())
		_ = store.SaveSnapshot(awakeSnap())
		cfg := Config{AgentToken: token, XAIKey: "k", XAIModel: "m", XAIBase: xai.URL, GrokDailyTokens: 1000, DigestGrok: optIn}
		s := NewServer(cfg, store, NewPower("cloud64", nil, false), fstest.MapFS{})
		before := calls.Load()
		d := s.makeDigest(context.Background(), time.Now())
		called := calls.Load() > before
		if called != optIn || (d.Summary != "") != optIn {
			t.Errorf("opt-in %v: called xAI %v, summary %q", optIn, called, d.Summary)
		}
	}
}

// A wake is logged when asked for, and again if the wake command fails.
func TestWakeFailureLogged(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	s := NewServer(Config{AllowedUsers: []string{"owner@example.com"}, AgentToken: token}, store, NewPower("cloud64", []string{"false"}, false), fstest.MapFS{})
	if w := do(s.Handler(), "POST", "/api/wake", "", actor); w.Code != http.StatusAccepted {
		t.Fatalf("wake: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(store.RecentAudit(5)) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := store.RecentAudit(5)
	if len(got) != 2 || got[1].What != "Sent a wake to cloud64" || got[0].What != "Wake of cloud64 (failed)" || got[0].Result == "" || got[0].Who != "owner@example.com" {
		t.Errorf("audit = %+v", got)
	}
}
