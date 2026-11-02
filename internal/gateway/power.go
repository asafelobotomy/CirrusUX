package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/snapshot"
)

var london = mustLoc("Europe/London")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return l
}

func ukStamp(t time.Time) string {
	l := t.In(london)
	return l.Format("2006-01-02 15:04 ") + zoneLabel(l)
}

func zoneLabel(t time.Time) string {
	if name, _ := t.Zone(); name == "BST" {
		return "BST"
	}
	return "GMT"
}

const (
	// A snapshot older than this means the agent is not reporting.
	snapshotFresh = 7 * time.Minute
	// Give up waiting for a wake after this long (cloud64-wake polls ~6 min).
	wakeTimeout = 7 * time.Minute
	tsPoll      = 20 * time.Second
)

// Power tracks whether cloud64 is awake without ever contacting it:
// the local tailscaled's view of the peer plus the age of the last
// snapshot (PLAN §3.1).
type Power struct {
	peer    string
	wakeCmd []string
	useTS   bool

	mu       sync.Mutex
	notice   string // a problem on cloud-box's side, shown in the UI
	tsKnown  bool
	online   bool
	lastSeen time.Time
	waking   *wakeState
}

type wakeState struct {
	started time.Time
	step    int // 1 packet sent, 2 network up, 3 tailscale connected
	err     string
}

type PowerView struct {
	State    string     `json:"state"` // awake, asleep, waking, unreachable
	Since    *time.Time `json:"since,omitempty"`
	WakeStep int        `json:"wakeStep,omitempty"`
	WakeErr  string     `json:"wakeError,omitempty"`
	// What the state is based on, for the UI's "how do we know" hint.
	Source string `json:"source"`
	// Set when the gateway can't trust its own view, e.g. cloud-box lost
	// its connection to Tailscale's coordination server.
	Notice string `json:"notice,omitempty"`
}

func NewPower(peer string, wakeCmd []string, useTailscale bool) *Power {
	return &Power{peer: peer, wakeCmd: wakeCmd, useTS: useTailscale}
}

func (p *Power) Run(ctx context.Context) {
	if !p.useTS {
		return
	}
	t := time.NewTicker(tsPoll)
	defer t.Stop()
	for {
		p.pollTailscale(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *Power) pollTailscale(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		slog.Warn("tailscale status", "err", err)
		p.tsKnown = false
		p.notice = "The gateway can't read Tailscale's status, so awake/asleep comes from the agent's last report only."
		return
	}
	st := parseStatus(out, p.peer)
	if !st.selfOK {
		// cloud-box's tailscaled has dropped off the coordination server
		// (seen on 1 Nov). Peer states are stale then: fall back to the
		// snapshot's age rather than calling cloud64 asleep.
		p.tsKnown = false
		p.notice = "cloud-box has lost its connection to Tailscale. Reports from cloud64 can't arrive until it reconnects."
		return
	}
	p.notice = ""
	p.tsKnown = st.found
	p.online, p.lastSeen = st.online, st.lastSeen
	if p.waking != nil && st.online && p.waking.step < 3 {
		p.waking.step = 3
	}
}

type tsStatus struct {
	selfOK        bool // our own tailscaled is running and connected
	found, online bool
	lastSeen      time.Time
}

func parseStatus(statusJSON []byte, peer string) tsStatus {
	var st struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			Online bool `json:"Online"`
		} `json:"Self"`
	}
	if json.Unmarshal(statusJSON, &st) != nil {
		return tsStatus{}
	}
	online, lastSeen, found := findPeer(statusJSON, peer)
	return tsStatus{selfOK: st.BackendState == "Running" && st.Self.Online, found: found, online: online, lastSeen: lastSeen}
}

func findPeer(statusJSON []byte, name string) (online bool, lastSeen time.Time, found bool) {
	var st struct {
		Peer map[string]struct {
			HostName string    `json:"HostName"`
			DNSName  string    `json:"DNSName"`
			Online   bool      `json:"Online"`
			LastSeen time.Time `json:"LastSeen"`
		} `json:"Peer"`
	}
	if json.Unmarshal(statusJSON, &st) != nil {
		return false, time.Time{}, false
	}
	for _, peer := range st.Peer {
		if strings.EqualFold(peer.HostName, name) || strings.EqualFold(strings.SplitN(peer.DNSName, ".", 2)[0], name) {
			return peer.Online, peer.LastSeen, true
		}
	}
	return false, time.Time{}, false
}

// View works out the state shown to the user.
func (p *Power) View(now time.Time, snap *snapshot.Snapshot) PowerView {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.view(now, snap)
	v.Notice = p.notice
	return v
}

func (p *Power) view(now time.Time, snap *snapshot.Snapshot) PowerView {
	fresh := snap != nil && now.Sub(snap.TakenAt) < snapshotFresh

	if w := p.waking; w != nil {
		reported := snap != nil && snap.TakenAt.After(w.started)
		switch {
		case reported:
			p.waking = nil
		case w.err != "" || now.Sub(w.started) > wakeTimeout:
			msg := w.err
			if msg == "" {
				msg = "cloud64 did not report back within 7 minutes"
			}
			p.waking = nil
			return PowerView{State: "asleep", WakeErr: msg, Source: "wake"}
		default:
			return PowerView{State: "waking", WakeStep: w.step, Source: "wake"}
		}
	}

	if p.tsKnown {
		switch {
		case p.online && fresh:
			return PowerView{State: "awake", Since: &snap.Power.Since, Source: "tailscale+agent"}
		case p.online:
			return PowerView{State: "unreachable", Source: "tailscale"}
		default:
			since := p.lastSeen
			if snap != nil && snap.TakenAt.After(since) {
				since = snap.TakenAt
			}
			return PowerView{State: "asleep", Since: nonZero(since), Source: "tailscale"}
		}
	}
	if fresh {
		return PowerView{State: "awake", Since: &snap.Power.Since, Source: "agent"}
	}
	var since *time.Time
	if snap != nil {
		since = &snap.TakenAt
	}
	return PowerView{State: "asleep", Since: since, Source: "agent"}
}

func nonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Wake runs the configured wake command (cloud64-wake on the admin box) in
// the background. It reports false if a wake is already running. onFail, if
// set, is called with the error when the command fails.
func (p *Power) Wake(onFail func(error)) bool {
	p.mu.Lock()
	if p.waking != nil {
		p.mu.Unlock()
		return false
	}
	w := &wakeState{started: time.Now(), step: 1}
	p.waking = w
	p.mu.Unlock()

	if len(p.wakeCmd) == 0 {
		// Development: pretend the packet went out.
		go func() {
			time.Sleep(2 * time.Second)
			p.mu.Lock()
			w.step = 2
			p.mu.Unlock()
		}()
		return true
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), wakeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, p.wakeCmd[0], p.wakeCmd[1:]...).CombinedOutput()
		p.mu.Lock()
		if err != nil {
			slog.Warn("wake command failed", "err", err, "output", strings.TrimSpace(string(out)))
			w.err = "The wake command failed (" + err.Error() + ")"
		} else if w.step < 2 {
			w.step = 2
		}
		p.mu.Unlock()
		if err != nil && onFail != nil {
			onFail(err)
		}
	}()
	return true
}
