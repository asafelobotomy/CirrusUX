package appjob

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"cirrusux/internal/catalogue"
)

// healthURL is where an app's check is asked, or "" if it has none. The
// port is the one the check names, else the app's main port; the address
// is the one ports are published on, since with APPS_BIND set nothing
// listens on loopback.
func (e *Env) healthURL(app catalogue.App, p catalogue.Plan) string {
	if app.Check == nil {
		return ""
	}
	port := app.MainPort()
	for _, q := range app.Ports {
		if q.Name == app.Check.Port {
			port = q
		}
	}
	scheme := "http"
	if port.HTTPS {
		scheme = "https"
	}
	addr := net.JoinHostPort(probeHost(e.Host.BindAddress), strconv.Itoa(hostPortOf(p, port)))
	return fmt.Sprintf("%s://%s%s", scheme, addr, app.Check.Path)
}

// probeHost is the address to reach a port published on bind: that
// address itself, or loopback when ports are published on every address.
func probeHost(bind string) string {
	a, err := netip.ParseAddr(bind)
	if err != nil || a.IsUnspecified() {
		return "127.0.0.1"
	}
	return a.String()
}

func (e *Env) waitHealthy(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, e.HealthTimeout)
	defer cancel()
	var last error
	for {
		if last = e.Healthy(ctx, url); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(2 * time.Second):
		}
	}
}

func hostPortOf(p catalogue.Plan, port catalogue.Port) int {
	for _, pm := range p.Ports {
		if pm.Container == port.Container && pm.Protocol == protoOr(port.Protocol) {
			return pm.Host
		}
	}
	return port.Host
}

func protoOr(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}
