// Package redact masks anything identifying or secret in text before it is
// sent to xAI (PLAN §3.6, decision D2: summaries only, IPs/MACs/paths masked).
package redact

import "regexp"

type rule struct {
	re   *regexp.Regexp
	with string
}

// Order matters: secrets first, so a token that contains a path-like or
// IP-like fragment is masked as a whole.
var rules = []rule{
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer [secret]"},
	// key=value / key: value pairs whose key looks secret
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:PASS(?:WORD)?|SECRET|TOKEN|API[_-]?KEY|AUTH|PRIVATE[_-]?KEY|COOKIE|SESSION)[A-Z0-9_]*)\s*[:=]\s*("[^"]*"|'[^']*'|\S+)`), "$1=[secret]"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), "[private key]"},
	{regexp.MustCompile(`\bAGE-SECRET-KEY-[A-Z0-9]+`), "[secret]"},
	{regexp.MustCompile(`\btskey-[A-Za-z0-9-]+`), "[secret]"},
	{regexp.MustCompile(`\bxai-[A-Za-z0-9]{20,}`), "[secret]"},
	// long opaque tokens (hex or base64-ish, 32+ chars)
	{regexp.MustCompile(`\b[A-Fa-f0-9]{32,}\b`), "[token]"},
	{regexp.MustCompile(`\b[A-Za-z0-9+/_-]{40,}={0,2}`), "[token]"},
	// e-mail addresses
	{regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), "[email]"},
	// MAC addresses
	{regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`), "[mac]"},
	// IPv4, optionally with port or prefix
	{regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?:/\d{1,2}|:\d{1,5})?\b`), "[ip]"},
	// IPv6 (at least three groups with colons, to leave times like 18:08 alone)
	{regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}(?::|[0-9a-f]{1,4})(?:/\d{1,3})?|(?i)\b(?:[0-9a-f]{1,4}:){1,6}:(?:/\d{1,3})?`), "[ip]"},
	// absolute paths
	{regexp.MustCompile(`(?:^|[\s"'(=])(/(?:[\w.@+-]+/)*[\w.@+-]+/?)`), " [path]"},
	// tailnet names
	{regexp.MustCompile(`\b[\w-]+\.[\w-]+\.ts\.net\b`), "[tailnet-host]"},
}

// String masks secrets, addresses and paths in s.
func String(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.with)
	}
	return s
}
