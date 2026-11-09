package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
		{"CIRRUS_SETUP": "split", "CIRRUS_WAKE": "wol", "CIRRUS_WAKE_MAC": "zz"},
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

// The router's answer is a second opinion: it fills in when Tailscale
// can't be read, explains a server that's on the LAN but not the tailnet,
// and shows a wake's progress before Tailscale or the agent are back.
func TestRouterCheck(t *testing.T) {
	answer := filepath.Join(t.TempDir(), "answer")
	script := filepath.Join(t.TempDir(), "check")
	os.WriteFile(script, []byte("#!/bin/sh\ncat "+answer+"\n"), 0o755)
	say := func(s string) { os.WriteFile(answer, []byte(s), 0o644) }
	p := NewPower(PowerOptions{Server: "atlas", Split: true, CheckCmd: []string{script}, CheckEvery: time.Minute})
	now := time.Now()
	fresh := &snapshot.Snapshot{TakenAt: now, Power: snapshot.Power{Since: now.Add(-time.Hour)}}
	stale := &snapshot.Snapshot{TakenAt: now.Add(-time.Hour)}

	say("offline\n")
	p.pollRouter(context.Background())
	if v := p.View(now, stale); v.State != "asleep" || v.Source != "router" {
		t.Errorf("router offline, no Tailscale: %+v", v)
	}
	say("online\n")
	p.pollRouter(context.Background())
	if v := p.View(now, stale); v.State != "unreachable" || v.Source != "router" {
		t.Errorf("router online, agent silent: %+v", v)
	}
	if v := p.View(now, fresh); v.State != "awake" || v.Source != "router+agent" {
		t.Errorf("router online, agent fresh: %+v", v)
	}

	// Tailscale says offline while the router sees it.
	p.tsKnown, p.online = true, false
	if v := p.View(now, stale); v.State != "unreachable" || !strings.Contains(v.Notice, "router sees atlas") {
		t.Errorf("on the LAN, not the tailnet: %+v", v)
	}

	// Garbage keeps the last good answer until it's too old.
	say("Error: login failed")
	p.pollRouter(context.Background())
	if online, known := p.routerSays(now); !known || !online {
		t.Error("an unreadable answer replaced a good one")
	}
	if _, known := p.routerSays(now.Add(4 * time.Minute)); known {
		t.Error("an old answer still trusted")
	}

	// During a wake the router seeing the server is step 2.
	p.tsKnown = false
	p.waking = &wakeState{started: now, step: 1}
	say("online")
	p.pollRouter(context.Background())
	if p.waking.step != 2 {
		t.Errorf("wake step = %d", p.waking.step)
	}
}

func TestZoneLabel(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	london, _ := time.LoadLocation("Europe/London")
	dubai, _ := time.LoadLocation("Asia/Dubai") // no abbreviation: "+04"
	if got := zoneLabel(at.In(london)); got != "GMT" {
		t.Errorf("London = %q", got)
	}
	if got := zoneLabel(at.In(dubai)); got != "UTC+04:00" {
		t.Errorf("Dubai = %q", got)
	}
}

func TestConfigWolMAC(t *testing.T) {
	base := map[string]string{"CIRRUS_SETUP": "split", "CIRRUS_SERVER_NAME": "atlas", "CIRRUS_WAKE": "wol"}
	setEnv(t, base)
	c, err := ConfigFromEnv()
	if err != nil || c.Wake != "wol" || c.WakeMAC != nil {
		t.Errorf("wol without a MAC is valid, got %v %v", c.WakeMAC, err)
	}
	base["CIRRUS_WAKE_MAC"] = "not-a-mac"
	setEnv(t, base)
	if _, err := ConfigFromEnv(); err == nil {
		t.Error("a malformed MAC should be a config error")
	}
	base["CIRRUS_WAKE_MAC"] = "00:11:22:33:44:55:66:77"
	setEnv(t, base)
	if _, err := ConfigFromEnv(); err == nil {
		t.Error("an 8-byte MAC should be a config error")
	}
}
