package gateway

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config comes from the environment (/etc/cirrusux/gateway.env, mode
// 600). Secrets are never logged or sent to the browser. See
// docs/CONFIG.md for every setting.
type Config struct {
	Listen  string // 127.0.0.1:8064
	DataDir string
	// Dev skips sign-in and Tailscale power polling. Only for running on a
	// workstation.
	Dev        bool
	AgentToken string

	// What the panel manages, as shown in the UI.
	ServerName  string // e.g. "homeserver"
	ServerHost  string // host name or address apps open on; defaults to ServerName
	GatewayName string // the machine the gateway runs on
	// ServerAbout is a sentence or two for the assistant: what the server
	// is and runs. Optional.
	ServerAbout string
	// Setup is "single" (gateway and agent on one machine that doesn't
	// sleep) or "split" (a server that sleeps and an always-on gateway).
	Setup string
	TZ    *time.Location
	// TZName is the IANA name sent to the browser, e.g. "Europe/London".
	TZName string

	// Sign-in: "tailscale" trusts the Tailscale-User-Login header that
	// `tailscale serve` adds; "header" trusts AuthHeader from a reverse
	// proxy (Authelia, Authentik, oauth2-proxy). Either way the header is
	// only believed from TrustedProxies.
	Auth           string
	AuthHeader     string
	TrustedProxies []netip.Prefix
	AllowedUsers   []string

	// Power and wake (split setup only).
	TailscalePeer string // the server's Tailscale name; "" skips Tailscale
	Wake          string // "wol", "command" or "none"
	WakeMAC       net.HardwareAddr
	WakeBroadcast string   // host:port for the magic packet
	WakeCmd       []string // argv, no shell
	// PowerCheckCmd asks the router whether the server is on the network
	// (prints "online" or "offline"); a second opinion beside Tailscale.
	PowerCheckCmd   []string
	PowerCheckEvery time.Duration
	Changelog       string // optional CHANGELOG.md to append to

	Assistant            AssistantConfig
	AssistantDailyTokens int
	// DigestAssistant lets the morning digest ask the assistant for a
	// written summary. Off by default: the assistant is otherwise only
	// called when you use it.
	DigestAssistant bool

	// Git hosting (docs/GIT.md): off unless Git is set.
	Git        bool
	GitDir     string // bare repositories
	GitURL     string // base for clone URLs; "" uses the address the panel was opened on
	GitMaxPush int64  // bytes
}

// AssistantConfig picks an OpenAI-compatible chat API.
type AssistantConfig struct {
	Provider string // xai, openai, anthropic, ollama, custom; "" when not set up
	Name     string // shown in the UI, e.g. "Grok"
	Base     string
	Model    string
	Key      string
}

// assistantPresets fill in the API address and display name per provider.
var assistantPresets = map[string]struct{ base, name string }{
	"xai":       {"https://api.x.ai/v1", "Grok"},
	"openai":    {"https://api.openai.com/v1", "Assistant"},
	"anthropic": {"https://api.anthropic.com/v1", "Claude"},
	"ollama":    {"http://127.0.0.1:11434/v1", "Assistant"},
	"custom":    {"", "Assistant"},
}

func ConfigFromEnv() (Config, error) {
	c := Config{
		Listen:      env("CIRRUS_LISTEN", "127.0.0.1:8064"),
		DataDir:     env("CIRRUS_DATA_DIR", "/var/lib/cirrusux-gateway"),
		Dev:         os.Getenv("CIRRUS_DEV") == "1",
		AgentToken:  os.Getenv("CIRRUS_AGENT_TOKEN"),
		ServerName:  env("CIRRUS_SERVER_NAME", "server"),
		GatewayName: env("CIRRUS_GATEWAY_NAME", hostname()),
		ServerAbout: os.Getenv("CIRRUS_SERVER_ABOUT"),
		Setup:       env("CIRRUS_SETUP", "single"),

		Auth:         env("CIRRUS_AUTH", "tailscale"),
		AuthHeader:   env("CIRRUS_AUTH_HEADER", "Remote-User"),
		AllowedUsers: splitList(os.Getenv("CIRRUS_ALLOWED_USERS")),

		WakeBroadcast:   env("CIRRUS_WAKE_BROADCAST", "255.255.255.255:9"),
		WakeCmd:         strings.Fields(os.Getenv("CIRRUS_WAKE_CMD")),
		PowerCheckCmd:   strings.Fields(os.Getenv("CIRRUS_POWER_CHECK_CMD")),
		PowerCheckEvery: time.Duration(envInt("CIRRUS_POWER_CHECK_SECS", 60)) * time.Second,
		Changelog:       os.Getenv("CIRRUS_CHANGELOG"),

		AssistantDailyTokens: envInt("CIRRUS_ASSISTANT_DAILY_TOKENS", 200_000),
		DigestAssistant:      os.Getenv("CIRRUS_DIGEST_ASSISTANT") == "1",
	}
	c.ServerHost = env("CIRRUS_SERVER_HOST", c.ServerName)

	if len(c.AgentToken) < 32 {
		return c, fmt.Errorf("CIRRUS_AGENT_TOKEN must be set to at least 32 random characters")
	}
	if c.Setup != "single" && c.Setup != "split" {
		return c, fmt.Errorf("CIRRUS_SETUP must be single or split, not %q", c.Setup)
	}

	var err error
	if c.TZ, c.TZName, err = loadZone(os.Getenv("CIRRUS_TZ")); err != nil {
		return c, err
	}

	switch c.Auth {
	case "tailscale", "header":
	default:
		return c, fmt.Errorf("CIRRUS_AUTH must be tailscale or header, not %q", c.Auth)
	}
	if c.TrustedProxies, err = parsePrefixes(env("CIRRUS_TRUSTED_PROXIES", "127.0.0.1/32,::1/128")); err != nil {
		return c, err
	}
	if !c.Dev && len(c.AllowedUsers) == 0 {
		return c, fmt.Errorf("CIRRUS_ALLOWED_USERS must list at least one login")
	}

	if c.Setup == "split" {
		c.TailscalePeer = env("CIRRUS_TAILSCALE_PEER", c.ServerName)
		if os.Getenv("CIRRUS_TAILSCALE_PEER") == "-" {
			c.TailscalePeer = ""
		}
		c.Wake = os.Getenv("CIRRUS_WAKE")
		if c.Wake == "" {
			// Older env files only set the command.
			c.Wake = "none"
			if len(c.WakeCmd) > 0 {
				c.Wake = "command"
			}
		}
		switch c.Wake {
		case "none":
		case "command":
			if len(c.WakeCmd) == 0 {
				return c, fmt.Errorf("CIRRUS_WAKE=command needs CIRRUS_WAKE_CMD")
			}
		case "wol":
			if c.WakeMAC, err = net.ParseMAC(os.Getenv("CIRRUS_WAKE_MAC")); err != nil || len(c.WakeMAC) != 6 {
				return c, fmt.Errorf("CIRRUS_WAKE=wol needs CIRRUS_WAKE_MAC, a 6-byte MAC address")
			}
		default:
			return c, fmt.Errorf("CIRRUS_WAKE must be wol, command or none, not %q", c.Wake)
		}
	} else {
		c.Wake = "none"
	}

	if c.Assistant, err = assistantFromEnv(); err != nil {
		return c, err
	}
	if err := c.gitFromEnv(); err != nil {
		return c, err
	}
	return c, nil
}

func assistantFromEnv() (AssistantConfig, error) {
	a := AssistantConfig{
		Provider: os.Getenv("ASSISTANT_PROVIDER"),
		Key:      os.Getenv("ASSISTANT_API_KEY"),
		Model:    os.Getenv("ASSISTANT_MODEL"),
	}
	if a.Provider == "" {
		// Not set up: the panel says so under a neutral name.
		a.Name = env("ASSISTANT_NAME", "Assistant")
		if a.Model != "" {
			return a, fmt.Errorf("ASSISTANT_MODEL is set: also set ASSISTANT_PROVIDER (xai, openai, anthropic, ollama or custom)")
		}
		return a, nil
	}
	p, ok := assistantPresets[a.Provider]
	if !ok {
		return a, fmt.Errorf("ASSISTANT_PROVIDER must be xai, openai, anthropic, ollama or custom, not %q", a.Provider)
	}
	a.Base = env("ASSISTANT_BASE_URL", p.base)
	a.Name = env("ASSISTANT_NAME", p.name)
	if a.Provider == "custom" && a.Base == "" && a.Model != "" {
		return a, fmt.Errorf("ASSISTANT_PROVIDER=custom needs ASSISTANT_BASE_URL")
	}
	return a, nil
}

// AssistantEnabled reports whether a model is configured, with a key for
// the hosted providers (a local Ollama needs none).
func (c Config) AssistantEnabled() bool {
	a := c.Assistant
	return a.Model != "" && a.Base != "" && (a.Key != "" || a.Provider == "ollama" || a.Provider == "custom")
}

// Split reports whether the managed server sleeps behind this gateway.
func (c Config) Split() bool { return c.Setup == "split" }

// loadZone returns the configured zone, or the machine's own.
func loadZone(name string) (*time.Location, string, error) {
	if name == "" {
		name = localZoneName()
	}
	if name == "" {
		return time.UTC, "UTC", nil // the browser is told UTC, so stamps are in UTC too
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", fmt.Errorf("CIRRUS_TZ: %v", err)
	}
	return l, name, nil
}

// localZoneName finds the machine's IANA zone name: $TZ, /etc/timezone, or
// the /etc/localtime link.
func localZoneName() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, tz, ok := strings.Cut(p, "zoneinfo/"); ok {
			return tz
		}
	}
	return ""
}

func parsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range splitList(s) {
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("CIRRUS_TRUSTED_PROXIES: %v", err)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("CIRRUS_TRUSTED_PROXIES: %v", err)
		}
		out = append(out, p)
	}
	return out, nil
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "gateway"
	}
	return h
}

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
