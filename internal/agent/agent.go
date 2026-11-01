// Package agent runs on cloud64. It collects a snapshot and pushes it to
// the gateway. Phase 1 opens no listening port at all: every connection is
// outbound, which the idle check does not count as activity.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"cirrusux/internal/snapshot"
	"cirrusux/internal/version"
)

type Config struct {
	GatewayURL string // https://cloud-box.<tailnet>.ts.net/agent/snapshot
	Token      string
	Interval   time.Duration
	// DemoFile replaces the collector with a recorded snapshot whose times
	// are shifted to now. For running the panel without cloud64.
	DemoFile string
}

type Agent struct {
	cfg       Config
	collector *Collector
	client    *http.Client
	poll      *http.Client  // long poll for commands: longer timeout
	pushNow   chan struct{} // an action changed state: report it now
}

func New(cfg Config, c *Collector) *Agent {
	return &Agent{
		cfg:       cfg,
		collector: c,
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
		},
		poll: &http.Client{
			Timeout:   45 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
		},
		pushNow: make(chan struct{}, 1),
	}
}

// Run collects and pushes every Interval. It also pushes straight after a
// resume from sleep, noticed as a jump in wall-clock time, retrying while
// tailscaled comes back (~60 s after resume, brief §5.5).
func (a *Agent) Run(ctx context.Context) error {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()

	go a.commandLoop(ctx)
	a.cycle(ctx)
	last := time.Now().Round(0)
	nextPush := time.Now().Add(a.cfg.Interval)
	retryUntil := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-a.pushNow:
			nextPush = time.Now()
		}
		wall := time.Now().Round(0) // strip monotonic: it stops during suspend
		if wall.Sub(last) > 60*time.Second {
			slog.Info("resume detected", "gap", wall.Sub(last).Round(time.Second))
			retryUntil = wall.Add(3 * time.Minute)
			nextPush = wall
		}
		last = wall
		if !time.Now().Before(nextPush) {
			ok := a.cycle(ctx)
			switch {
			case ok:
				nextPush, retryUntil = time.Now().Add(a.cfg.Interval), time.Time{}
			case time.Now().Before(retryUntil):
				nextPush = time.Now().Add(15 * time.Second)
			default:
				// The gateway or cloud-box's tailnet link is down (it
				// drops now and then); try again soon rather than in a
				// full interval.
				nextPush = time.Now().Add(min(a.cfg.Interval, time.Minute))
			}
		}
	}
}

func (a *Agent) cycle(ctx context.Context) bool {
	s, err := a.snapshot(ctx)
	if err != nil {
		slog.Error("snapshot", "err", err)
		return false
	}
	for _, e := range s.Errors {
		slog.Warn("collect", "problem", e)
	}
	if err := a.push(ctx, s); err != nil {
		slog.Warn("push failed", "err", err)
		return false
	}
	slog.Info("pushed snapshot", "containers", len(s.Containers), "problems", len(s.Errors))
	return true
}

func (a *Agent) snapshot(ctx context.Context) (snapshot.Snapshot, error) {
	var s snapshot.Snapshot
	if a.cfg.DemoFile != "" {
		var err error
		if s, err = demoSnapshot(a.cfg.DemoFile, time.Now()); err != nil {
			return s, err
		}
	} else {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		s = a.collector.Collect(cctx)
	}
	s.AgentVersion = version.String()
	return s, nil
}

func (a *Agent) push(ctx context.Context, s snapshot.Snapshot) error {
	body, err := json.Marshal(s)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.GatewayURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("gateway: %s", resp.Status)
	}
	return nil
}

// demoSnapshot loads a recorded snapshot and moves every time in it by the
// same amount, so its TakenAt becomes now.
func demoSnapshot(path string, now time.Time) (snapshot.Snapshot, error) {
	var s snapshot.Snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	d := now.Sub(s.TakenAt)
	sh := func(t time.Time) time.Time {
		if t.IsZero() {
			return t
		}
		return t.Add(d)
	}
	shp := func(t *time.Time) *time.Time {
		if t == nil {
			return nil
		}
		v := sh(*t)
		return &v
	}
	s.TakenAt, s.BootAt = now, sh(s.BootAt)
	s.Power.Since, s.Power.LastActivity, s.Power.StayAwakeUntil = sh(s.Power.Since), shp(s.Power.LastActivity), shp(s.Power.StayAwakeUntil)
	for i := range s.Power.NextWakes {
		s.Power.NextWakes[i].At = sh(s.Power.NextWakes[i].At)
	}
	for i := range s.Containers {
		s.Containers[i].UpSince = shp(s.Containers[i].UpSince)
	}
	for i := range s.Backups.Files {
		s.Backups.Files[i].At = sh(s.Backups.Files[i].At)
	}
	if r := s.Backups.LastRun; r != nil {
		r.Start, r.End = sh(r.Start), sh(r.End)
	}
	if s.Security != nil && s.Security.ClamAV != nil {
		s.Security.ClamAV.At = sh(s.Security.ClamAV.At)
	}
	for i := range s.Timeline {
		s.Timeline[i].From, s.Timeline[i].To = sh(s.Timeline[i].From), shp(s.Timeline[i].To)
	}
	return s, nil
}
