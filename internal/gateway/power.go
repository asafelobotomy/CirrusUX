package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/snapshot"
)

// zone is the time zone every stamp the gateway writes uses (CIRRUS_TZ).
// NewServer sets it once at start-up.
var zone = time.UTC

// stamp formats t as "2006-01-02 15:04 GMT" in the configured zone.
func stamp(t time.Time) string {
	l := t.In(zone)
	return l.Format("2006-01-02 15:04 ") + zoneLabel(l)
}

// zoneLabel is the zone's abbreviation (GMT, BST, CET), or its offset when
// the zone has none.
func zoneLabel(t time.Time) string {
	name, _ := t.Zone()
	return name
}

const (
	// A snapshot older than this means the agent is not reporting.
	snapshotFresh = 7 * time.Minute
	// Give up waiting for a wake after this long (a wake command may poll ~6 min).
	wakeTimeout = 7 * time.Minute
	tsPoll      = 20 * time.Second
)

// Power tracks whether the server is awake without ever contacting it:
// the local tailscaled's view of the peer plus the age of the last
// snapshot (PLAN §3.1). In the single setup the gateway runs on the server
// itself, so there is no asleep: only awake, or not reporting.
type Power struct {
	opt PowerOptions

	mu       sync.Mutex
	notice   string // a problem on the gateway's side, shown in the UI
	tsKnown  bool
	online   bool
	lastSeen time.Time
	waking   *wakeState

	// The router's view (PowerCheckCmd): a second opinion, and the first
	// sign of a wake, before Tailscale or the agent are back.
	routerOnline bool
	routerAt     time.Time // when it last answered online or offline
}

// PowerOptions come from the gateway's config.
type PowerOptions struct {
	Server, Gateway string // display names
	Split           bool
	Peer            string // Tailscale name of the server; "" skips Tailscale
	Wake            string // wol, command or none
	WakeMAC         net.HardwareAddr
	WakeBroadcast   string
	WakeCmd         []string
	Dev             bool // pretend wakes work, skip Tailscale
	// CheckCmd asks the router whether the server is on the network; it
	// prints "online" or "offline". Optional.
	CheckCmd   []string
	CheckEvery time.Duration
}

func PowerFromConfig(c Config) PowerOptions {
	return PowerOptions{
		Server: c.ServerName, Gateway: c.GatewayName, Split: c.Split(), Peer: c.TailscalePeer,
		Wake: c.Wake, WakeMAC: c.WakeMAC, WakeBroadcast: c.WakeBroadcast, WakeCmd: c.WakeCmd, Dev: c.Dev,
		CheckCmd: c.PowerCheckCmd, CheckEvery: c.PowerCheckEvery,
	}
}

type wakeState struct {
	started time.Time
	step    int // 1 signal sent, 2 network up, 3 Tailscale connected
	err     string
}

type PowerView struct {
	State    string     `json:"state"` // awake, asleep, waking, unreachable
	Since    *time.Time `json:"since,omitempty"`
	WakeStep int        `json:"wakeStep,omitempty"`
	WakeErr  string     `json:"wakeError,omitempty"`
	// What the state is based on, for the UI's "how do we know" hint.
	Source string `json:"source"`
	// Set when the gateway can't trust its own view, e.g. it lost its
	// connection to Tailscale's coordination server.
	Notice string `json:"notice,omitempty"`
}

func NewPower(opt PowerOptions) *Power {
	return &Power{opt: opt}
}

// CanWake reports whether a wake method is set up.
func (p *Power) CanWake() bool { return p.opt.Split && (p.opt.Wake != "none" || p.opt.Dev) }

func (p *Power) Run(ctx context.Context) {
	if p.opt.Split && !p.opt.Dev && len(p.opt.CheckCmd) > 0 {
		go p.runRouter(ctx)
	}
	if !p.opt.Split || p.opt.Dev || p.opt.Peer == "" {
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

func (p *Power) runRouter(ctx context.Context) {
	every := p.opt.CheckEvery
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		p.pollRouter(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pollRouter runs the check command once. Anything but "online" or
// "offline" leaves the router's view as unknown.
func (p *Power) pollRouter(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.opt.CheckCmd[0], p.opt.CheckCmd[1:]...).Output()
	word := strings.ToLower(strings.TrimSpace(firstLine(string(out))))
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil || (word != "online" && word != "offline") {
		slog.Warn("router check", "err", err, "said", word)
		return
	}
	p.routerOnline, p.routerAt = word == "online", time.Now()
	if p.waking != nil && p.routerOnline && p.waking.step < 2 {
		p.waking.step = 2 // the network link is up
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// routerSays is the router's view when it's recent enough to trust.
func (p *Power) routerSays(now time.Time) (online, known bool) {
	every := p.opt.CheckEvery
	if every <= 0 {
		every = time.Minute
	}
	if p.routerAt.IsZero() || now.Sub(p.routerAt) > 3*every {
		return false, false
	}
	return p.routerOnline, true
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
	st := parseStatus(out, p.opt.Peer)
	if !st.selfOK {
		// The gateway's own tailscaled has dropped off the coordination
		// server. Peer states are stale then: fall back to the snapshot's
		// age rather than calling the server asleep.
		p.tsKnown = false
		p.notice = p.opt.Gateway + " has lost its connection to Tailscale. Reports from " + p.opt.Server + " can't arrive until it reconnects."
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
	if p.notice != "" {
		v.Notice = p.notice
	}
	return v
}

func (p *Power) view(now time.Time, snap *snapshot.Snapshot) PowerView {
	fresh := snap != nil && now.Sub(snap.TakenAt) < snapshotFresh
	if !p.opt.Split {
		if fresh {
			return PowerView{State: "awake", Since: &snap.Power.Since, Source: "agent"}
		}
		return PowerView{State: "unreachable", Source: "agent"}
	}

	if w := p.waking; w != nil {
		reported := snap != nil && snap.TakenAt.After(w.started)
		switch {
		case reported:
			p.waking = nil
		case w.err != "" || now.Sub(w.started) > wakeTimeout:
			msg := w.err
			if msg == "" {
				msg = p.opt.Server + " did not report back within 7 minutes"
			}
			p.waking = nil
			return PowerView{State: "asleep", WakeErr: msg, Source: "wake"}
		default:
			return PowerView{State: "waking", WakeStep: w.step, Source: "wake"}
		}
	}

	router, routerKnown := p.routerSays(now)
	if p.tsKnown {
		switch {
		case p.online && fresh:
			return PowerView{State: "awake", Since: &snap.Power.Since, Source: "tailscale+agent"}
		case p.online:
			return PowerView{State: "unreachable", Source: "tailscale"}
		case routerKnown && router:
			// On the network, but not on the tailnet: Tailscale is still
			// starting after a wake, or has stopped.
			return PowerView{State: "unreachable", Source: "router",
				Notice: "The router sees " + p.opt.Server + " on the network, but Tailscale doesn't. If it just woke, Tailscale is still starting."}
		default:
			since := p.lastSeen
			if snap != nil && snap.TakenAt.After(since) {
				since = snap.TakenAt
			}
			return PowerView{State: "asleep", Since: nonZero(since), Source: "tailscale"}
		}
	}
	if routerKnown {
		switch {
		case router && fresh:
			return PowerView{State: "awake", Since: &snap.Power.Since, Source: "router+agent"}
		case router:
			return PowerView{State: "unreachable", Source: "router"}
		default:
			var since *time.Time
			if snap != nil {
				since = &snap.TakenAt
			}
			return PowerView{State: "asleep", Since: since, Source: "router"}
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

// Wake sends the configured wake in the background: a magic packet, or
// the wake command. It reports false if a wake is already running. onFail,
// if set, is called with the error when the wake can't be sent.
func (p *Power) Wake(onFail func(error)) bool {
	p.mu.Lock()
	if p.waking != nil {
		p.mu.Unlock()
		return false
	}
	w := &wakeState{started: time.Now(), step: 1}
	p.waking = w
	p.mu.Unlock()

	fail := func(err error) {
		p.mu.Lock()
		w.err = "The wake didn't go out (" + err.Error() + ")"
		p.mu.Unlock()
		if onFail != nil {
			onFail(err)
		}
	}
	switch {
	case p.opt.Wake == "wol":
		go func() {
			if err := sendMagicPacket(p.opt.WakeMAC, p.opt.WakeBroadcast); err != nil {
				slog.Warn("magic packet", "err", err)
				fail(err)
			}
		}()
	case p.opt.Wake == "command":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), wakeTimeout)
			defer cancel()
			out, err := exec.CommandContext(ctx, p.opt.WakeCmd[0], p.opt.WakeCmd[1:]...).CombinedOutput()
			if err != nil {
				slog.Warn("wake command failed", "err", err, "output", strings.TrimSpace(string(out)))
				fail(err)
				return
			}
			p.mu.Lock()
			if w.step < 2 {
				w.step = 2
			}
			p.mu.Unlock()
		}()
	default:
		// Development: pretend the packet went out.
		go func() {
			time.Sleep(2 * time.Second)
			p.mu.Lock()
			w.step = 2
			p.mu.Unlock()
		}()
	}
	return true
}

// sendMagicPacket broadcasts a Wake-on-LAN packet: six 0xFF bytes, then the
// MAC address 16 times. It goes out three times, as single UDP packets can
// be lost. Go sets SO_BROADCAST on UDP sockets, so a broadcast address works.
func sendMagicPacket(mac net.HardwareAddr, addr string) error {
	pkt := bytes.Repeat([]byte{0xff}, 6)
	for range 16 {
		pkt = append(pkt, mac...)
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	for range 3 {
		if _, err := conn.Write(pkt); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
