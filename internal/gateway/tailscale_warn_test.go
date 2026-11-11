package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// A gateway without Tailscale (a reverse proxy signs people in) can't read
// its status. It says so once in the log, not every 20 seconds, and the
// panel keeps its notice.
func TestTailscaleMissingIsLoggedOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no tailscale
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	p := NewPower(PowerOptions{Server: "homeserver", Gateway: "gateway", Split: true, Peer: "homeserver"})
	for range 3 {
		p.pollTailscale(context.Background())
	}
	if n := strings.Count(buf.String(), "tailscale status"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, buf.String())
	}
	p.mu.Lock()
	notice := p.notice
	p.mu.Unlock()
	if notice == "" {
		t.Error("the panel lost its notice")
	}
}
