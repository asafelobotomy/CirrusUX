package gateway

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"cirrusux/internal/snapshot"
)

func setEnv(t *testing.T, kv map[string]string) {
	for _, k := range []string{"CIRRUS_SETUP", "CIRRUS_SERVER_NAME", "CIRRUS_WAKE", "CIRRUS_WAKE_MAC", "CIRRUS_WAKE_CMD",
		"CIRRUS_AUTH", "CIRRUS_TZ", "CIRRUS_TAILSCALE_PEER", "ASSISTANT_PROVIDER", "ASSISTANT_MODEL", "ASSISTANT_API_KEY", "ASSISTANT_BASE_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv("CIRRUS_AGENT_TOKEN", token)
	t.Setenv("CIRRUS_ALLOWED_USERS", "Owner@Example.com")
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestConfigDefaults(t *testing.T) {
	setEnv(t, map[string]string{"CIRRUS_TZ": "Europe/Paris"})
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.Setup != "single" || c.Wake != "none" || c.TailscalePeer != "" || c.ServerName != "server" || c.ServerHost != "server" {
		t.Errorf("single-setup defaults: %+v", c)
	}
	if c.TZName != "Europe/Paris" || c.AllowedUsers[0] != "owner@example.com" || c.Auth != "tailscale" {
		t.Errorf("zone/users/auth: %s %v %s", c.TZName, c.AllowedUsers, c.Auth)
	}
	if c.AssistantEnabled() || c.Assistant.Name != "Assistant" {
		t.Errorf("unset assistant: %+v", c.Assistant)
	}
}

func TestConfigSplit(t *testing.T) {
	setEnv(t, map[string]string{"CIRRUS_SETUP": "split", "CIRRUS_SERVER_NAME": "atlas", "CIRRUS_WAKE": "wol", "CIRRUS_WAKE_MAC": "00:11:22:33:44:55"})
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.TailscalePeer != "atlas" || c.Wake != "wol" || c.WakeMAC.String() != "00:11:22:33:44:55" {
		t.Errorf("split: %+v", c)
	}

	// Older env files only set a wake command.
	setEnv(t, map[string]string{"CIRRUS_SETUP": "split", "CIRRUS_WAKE_CMD": "sudo -n /usr/local/sbin/cirrusux-wake", "CIRRUS_TAILSCALE_PEER": "-"})
	if c, err = ConfigFromEnv(); err != nil || c.Wake != "command" || len(c.WakeCmd) != 3 || c.TailscalePeer != "" {
		t.Errorf("command wake: %+v %v", c, err)
	}
}

func TestConfigRejects(t *testing.T) {
	for _, kv := range []map[string]string{
		{"CIRRUS_SETUP": "cluster"},
		{"CIRRUS_SETUP": "split", "CIRRUS_WAKE": "wol"},
		{"CIRRUS_SETUP": "split", "CIRRUS_WAKE": "command"},
		{"CIRRUS_AUTH": "none"},
		{"CIRRUS_TZ": "Mars/Olympus"},
		{"ASSISTANT_PROVIDER": "skynet"},
		{"ASSISTANT_MODEL": "m"},
	} {
		setEnv(t, kv)
		if _, err := ConfigFromEnv(); err == nil {
			t.Errorf("%v should be rejected", kv)
		}
	}
}

func TestAssistantPresets(t *testing.T) {
	setEnv(t, map[string]string{"ASSISTANT_PROVIDER": "ollama", "ASSISTANT_MODEL": "llama3"})
	c, _ := ConfigFromEnv()
	if !c.AssistantEnabled() || c.Assistant.Base != "http://127.0.0.1:11434/v1" {
		t.Errorf("ollama needs no key: %+v", c.Assistant)
	}
	setEnv(t, map[string]string{"ASSISTANT_PROVIDER": "anthropic", "ASSISTANT_MODEL": "m"})
	if c, _ = ConfigFromEnv(); c.AssistantEnabled() || c.Assistant.Name != "Claude" {
		t.Errorf("hosted provider without a key: %+v", c.Assistant)
	}
}

// The identity header only counts from the trusted front end.
func TestUntrustedProxy(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	cfg := testConfig()
	cfg.TrustedProxies = nil // 127.0.0.1/::1 only in real configs; nothing here
	h := NewServer(cfg, store, testPower(), fstest.MapFS{}).Handler()
	if w := do(h, "GET", "/api/me", "", owner); w.Code != http.StatusForbidden {
		t.Errorf("header from an untrusted address was believed: %d", w.Code)
	}
}

func TestHeaderAuth(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	cfg := testConfig()
	cfg.Auth = "header"
	h := NewServer(cfg, store, testPower(), fstest.MapFS{}).Handler()
	if w := do(h, "GET", "/api/me", "", owner); w.Code != http.StatusForbidden {
		t.Errorf("Tailscale header accepted in header mode: %d", w.Code)
	}
	w := do(h, "GET", "/api/me", "", map[string]string{"Remote-User": "owner@example.com"})
	var me struct {
		User, Server, Setup, TZ string
		Assistant               struct{ Enabled bool }
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if w.Code != http.StatusOK || me.User != "owner@example.com" || me.Server != "atlas" || me.Setup != "split" || me.TZ != "UTC" {
		t.Errorf("me: %d %s", w.Code, w.Body)
	}
}

func TestSingleSetup(t *testing.T) {
	p := NewPower(PowerOptions{Server: "atlas"})
	now := time.Now()
	if v := p.View(now, &snapshot.Snapshot{TakenAt: now}); v.State != "awake" {
		t.Errorf("fresh: %s", v.State)
	}
	if v := p.View(now, &snapshot.Snapshot{TakenAt: now.Add(-time.Hour)}); v.State != "unreachable" {
		t.Errorf("a single setup is never asleep: %s", v.State)
	}
	if p.CanWake() {
		t.Error("single setup can wake")
	}

	store, _ := OpenStore(t.TempDir())
	cfg := testConfig()
	cfg.Setup = "single"
	h := NewServer(cfg, store, p, fstest.MapFS{}).Handler()
	if w := do(h, "POST", "/api/wake", "", actor); w.Code != http.StatusNotImplemented {
		t.Errorf("wake without a method: %d", w.Code)
	}
}

func TestMagicPacket(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	if err := sendMagicPacket(mac, ln.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 200)
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0xff}, 6)
	for range 16 {
		want = append(want, mac...)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Errorf("packet = %x", buf[:n])
	}
}
