package gateway

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// gitFromEnv reads the Git hosting settings.
func (c *Config) gitFromEnv() error {
	c.Git = os.Getenv("CIRRUS_GIT") == "1"
	c.GitDir = env("CIRRUS_GIT_DIR", filepath.Join(c.DataDir, "git"))
	mb, err := strconv.Atoi(env("CIRRUS_GIT_MAX_PUSH_MB", "1024"))
	if err != nil || mb < 1 {
		return fmt.Errorf("CIRRUS_GIT_MAX_PUSH_MB must be a whole number of megabytes, 1 or more")
	}
	c.GitMaxPush = int64(mb) << 20
	if raw := os.Getenv("CIRRUS_GIT_URL"); raw != "" {
		u, err := url.Parse(strings.TrimSuffix(raw, "/"))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
			return fmt.Errorf("CIRRUS_GIT_URL must be an address like https://homeserver.example.ts.net, without a path")
		}
		c.GitURL = u.String()
	}
	return nil
}
