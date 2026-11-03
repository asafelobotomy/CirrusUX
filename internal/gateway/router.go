package gateway

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

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
