// c64panel-agent runs on cloud64 as the unprivileged user c64panel and
// pushes snapshots to the gateway on cloud-box. See docs/PLAN.md §3.2.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud64-panel/internal/agent"
)

func main() {
	var (
		gateway  = flag.String("gateway", env("C64_GATEWAY_URL", ""), "gateway snapshot URL")
		interval = flag.Duration("interval", 5*time.Minute, "push interval while awake")
		demo     = flag.String("demo", "", "push a recorded snapshot instead of collecting (development)")
		once     = flag.Bool("once", false, "collect once, print the snapshot as JSON and exit")
		media    = flag.String("media", "/mnt/media", "media mount point")
		dock     = flag.String("docker", "/run/c64panel/docker.sock", "filtered Docker socket")
		helper   = flag.String("helper", "sudo -n /usr/local/sbin/c64panel-helper", "root helper command; empty to skip")
	)
	flag.Parse()

	c := &agent.Collector{
		IdleConf:     "/etc/cloud64-idle.conf",
		MediaMount:   *media,
		DockerSocket: *dock,
		Helper:       strings.Fields(*helper),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(c.Collect(ctx))
		return
	}

	token := os.Getenv("C64_AGENT_TOKEN")
	if *gateway == "" || token == "" {
		slog.Error("C64_GATEWAY_URL and C64_AGENT_TOKEN are required")
		os.Exit(2)
	}
	a := agent.New(agent.Config{GatewayURL: *gateway, Token: token, Interval: *interval, DemoFile: *demo}, c)
	if err := a.Run(ctx); err != nil {
		slog.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
