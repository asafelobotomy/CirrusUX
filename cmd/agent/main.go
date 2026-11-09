// cirrusux-agent runs on the managed server as the unprivileged user
// cirrusux and pushes snapshots to the gateway. See docs/history/PLAN.md §3.2.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cirrusux/internal/agent"
	"cirrusux/internal/version"
)

func main() {
	var (
		gateway  = flag.String("gateway", env("CIRRUS_GATEWAY_URL", ""), "gateway snapshot URL")
		interval = flag.Duration("interval", 5*time.Minute, "push interval while awake")
		demo     = flag.String("demo", "", "push a recorded snapshot instead of collecting (development)")
		once     = flag.Bool("once", false, "collect once, print the snapshot as JSON and exit")
		disk     = flag.String("disk", env("CIRRUS_DISK", "/"), "file system the disk widget shows; empty to skip")
		idleConf = flag.String("idle-conf", env("CIRRUS_IDLE_CONF", "/etc/cirrusux/sleep.env"), "idle checker config to read (sleep module)")
		dock     = flag.String("docker", "/run/cirrusux/docker.sock", "filtered Docker socket")
		showVer  = flag.Bool("version", false, "print the version and exit")
		helper   = flag.String("helper", "sudo -n /usr/local/sbin/cirrusux-helper", "root helper command; empty to skip")
	)
	flag.Parse()
	if *showVer {
		fmt.Println("cirrusux-agent", version.String())
		return
	}

	c := &agent.Collector{
		IdleConf:     *idleConf,
		PortNames:    portNames(os.Getenv("CIRRUS_PORT_NAMES")),
		DiskMount:    *disk,
		WakeIface:    os.Getenv("CIRRUS_WAKE_IFACE"),
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

	token := os.Getenv("CIRRUS_AGENT_TOKEN")
	if *gateway == "" || token == "" {
		slog.Error("CIRRUS_GATEWAY_URL and CIRRUS_AGENT_TOKEN are required")
		os.Exit(2)
	}
	a := agent.New(agent.Config{GatewayURL: *gateway, Token: token, Interval: *interval, DemoFile: *demo}, c)
	if err := a.Run(ctx); err != nil {
		slog.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}

// portNames reads "8080=Proxy,22457=Admin panel" into a map.
func portNames(s string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Split(s, ",") {
		if port, name, ok := strings.Cut(strings.TrimSpace(f), "="); ok {
			m[strings.TrimSpace(port)] = strings.TrimSpace(name)
		}
	}
	return m
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
