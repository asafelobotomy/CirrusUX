package redact

import (
	"strings"
	"testing"
)

// Nothing in `leaks` may survive redaction.
func TestMasks(t *testing.T) {
	cases := []struct{ in, leak string }{
		{"PICARD_WEB_PASSWORD=hunter2hunter2", "hunter2"},
		{"TMM_PASSWORD: 'p@ss word'", "p@ss"},
		{"ROUTER_PASSWORD=\"x y z\"", "x y z"},
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc", "eyJhbGci"},
		{"key AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQ", "AGE-SECRET-KEY-1Q"},
		{"auth key tskey-auth-kAbc123CNTRL-xyz", "tskey-auth"},
		{"xai-0123456789abcdefghijABCDEFGHIJ", "xai-0123"},
		{"api_key=sk_live_123", "sk_live_123"},
		{"token 3f2a9c4b5d6e7f8091a2b3c4d5e6f708", "3f2a9c4b"},
		{"client 192.0.2.23 connected", "192.0.2.23"},
		{"peer 100.64.0.10:41641", "100.64.0.10"},
		{"lan 192.0.2.0/24", "192.0.2.0"},
		{"addr fd7a:115c:a1e0::10", "fd7a:115c"},
		{"ula fd00:db8:1::/64", "fd00:db8"},
		{"MAC 02:00:5e:00:00:01", "02:00:5e"},
		{"MAC 02-00-5E-00-00-01", "02-00-5E"},
		{"wrote /srv/backups/homeserver-system/x.tar.zst.age", "/srv/backups"},
		{"(see /etc/idle-check.conf)", "/etc/idle-check"},
		{"mail fly.someone@example.com", "example.com"},
		{"host gateway.tail1234.ts.net", "tail1234"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNzaC1rZXk"},
	}
	for _, c := range cases {
		if out := String(c.in); strings.Contains(out, c.leak) {
			t.Errorf("String(%q) = %q, still contains %q", c.in, out, c.leak)
		}
	}
}

// Ordinary status text must stay readable.
func TestKeeps(t *testing.T) {
	cases := []string{
		"Last backup OK at 03:31, 2.0 GB, encrypted",
		"Jellyfin 10.11.11 healthy since 20:20",
		"Asleep since 19:56 GMT",
		"Next wake Mon 2 Nov 03:30",
		"kavita v0.9.1.4-ls130 running",
	}
	for _, c := range cases {
		if out := String(c); out != c {
			t.Errorf("String(%q) changed to %q", c, out)
		}
	}
}
