package gateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config comes from the environment (/etc/cirrusux/gateway.env, mode
// 600). Secrets are never logged or sent to the browser.
type Config struct {
	Listen  string // 127.0.0.1:8064; published to the tailnet by `tailscale serve`
	DataDir string
	// Dev skips the Tailscale identity check and Tailscale power polling.
	// Only for running on a workstation.
	Dev          bool
	AllowedUsers []string // Tailscale logins allowed to use the panel
	AgentToken   string

	TailscalePeer string   // cloud64
	WakeCmd       []string // argv, no shell
	Changelog     string   // optional CHANGELOG.md to append to

	XAIKey          string
	XAIBase         string
	XAIModel        string
	GrokDailyTokens int
	// DigestGrok lets the morning digest ask Grok for a written summary.
	// Off by default: Grok is otherwise only called when you use it.
	DigestGrok bool
}

func ConfigFromEnv() (Config, error) {
	c := Config{
		Listen:          env("CIRRUS_LISTEN", "127.0.0.1:8064"),
		DataDir:         env("CIRRUS_DATA_DIR", "/var/lib/cirrusux"),
		Dev:             os.Getenv("CIRRUS_DEV") == "1",
		AllowedUsers:    splitList(os.Getenv("CIRRUS_ALLOWED_USERS")),
		AgentToken:      os.Getenv("CIRRUS_AGENT_TOKEN"),
		TailscalePeer:   env("CIRRUS_TAILSCALE_PEER", "cloud64"),
		WakeCmd:         strings.Fields(os.Getenv("CIRRUS_WAKE_CMD")),
		Changelog:       os.Getenv("CIRRUS_CHANGELOG"),
		XAIKey:          os.Getenv("XAI_API_KEY"),
		XAIBase:         env("XAI_BASE_URL", "https://api.x.ai/v1"),
		XAIModel:        os.Getenv("XAI_MODEL"),
		GrokDailyTokens: envInt("CIRRUS_GROK_DAILY_TOKENS", 200_000),
		DigestGrok:      os.Getenv("CIRRUS_DIGEST_GROK") == "1",
	}
	if len(c.AgentToken) < 32 {
		return c, fmt.Errorf("CIRRUS_AGENT_TOKEN must be set to at least 32 random characters")
	}
	if !c.Dev && len(c.AllowedUsers) == 0 {
		return c, fmt.Errorf("CIRRUS_ALLOWED_USERS must list at least one Tailscale login")
	}
	return c, nil
}

func (c Config) GrokEnabled() bool { return c.XAIKey != "" && c.XAIModel != "" }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return n
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, strings.ToLower(f))
	}
	return out
}
