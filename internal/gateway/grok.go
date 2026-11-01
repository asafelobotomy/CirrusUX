package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cirrusux/internal/grok"
	"cirrusux/internal/snapshot"
)

const systemPrompt = `You are the assistant inside CirrusUX, the web panel for the cloud64 home server. cloud64 is a low-power Debian media server (Jellyfin, Navidrome, Audiobookshelf, Kavita, tinyMediaManager, Picard) that sleeps after 30 idle minutes and only wakes from a magic packet or its RTC alarm.

You answer from the panel's cached data, gathered by tools. Asking you never wakes the server. Times are UK time (Europe/London).

Rules:
- Use the tools to check facts before answering. Say how old the data is when the server is asleep.
- You cannot run anything yourself. If waking the server would help, call propose_wake; the owner confirms it in the panel.
- Other actions (restarts, backups, updates) arrive in a later version: describe them, don't pretend to do them.
- Never ask for or repeat passwords, keys or tokens. Some values in tool results are masked on purpose.
- Be brief and concrete: a few sentences, no headings.`

func (s *Server) askGrok(w http.ResponseWriter, r *http.Request) {
	if s.grok == nil {
		http.Error(w, "Grok is not configured on the gateway (XAI_API_KEY and XAI_MODEL)", http.StatusServiceUnavailable)
		return
	}
	if used := s.store.GrokUsage(0); used >= s.cfg.GrokDailyTokens {
		http.Error(w, "Grok's daily token budget is used up", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Messages []grok.Turn `json:"messages"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil || len(body.Messages) == 0 {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if len(body.Messages) > 20 {
		body.Messages = body.Messages[len(body.Messages)-20:]
	}

	snap := s.store.Snapshot()
	view := s.power.View(time.Now(), snap)
	system := systemPrompt + "\n\nNow: " + ukStamp(time.Now()) + ". Server state: " + view.State + "."
	if snap != nil {
		system += " Last snapshot: " + ukStamp(snap.TakenAt) + "."
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	reply, err := s.grok.Ask(ctx, system, body.Messages, s.grokTools(snap, view))
	s.store.GrokUsage(reply.TokensIn + reply.TokensOut)
	if err != nil {
		slog.Warn("grok", "err", err)
		http.Error(w, "Grok could not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	if reply.Proposal != nil && view.State == "awake" {
		reply.Proposal = nil // nothing to wake
	}
	writeJSON(w, map[string]any{"text": reply.Text, "tools": reply.Tools, "proposal": reply.Proposal, "cached": view.State != "awake"})
}

// grokTools exposes the cached snapshot in small, summarised pieces.
func (s *Server) grokTools(snap *snapshot.Snapshot, view PowerView) []grok.Tool {
	none := func(f func() any) func(json.RawMessage) (any, error) {
		return func(json.RawMessage) (any, error) {
			if snap == nil {
				return nil, fmt.Errorf("no data from cloud64 yet")
			}
			return f(), nil
		}
	}
	return []grok.Tool{
		{Name: "power_status", Description: "Awake/asleep, since when, what keeps it awake, sleep settings and next scheduled wakes.", Run: none(func() any {
			p := snap.Power
			out := map[string]any{
				"state": view.State, "since": uk(view.Since), "snapshotTaken": ukStamp(snap.TakenAt),
				"activityReasons": p.Reasons, "lastActivity": uk(p.LastActivity),
				"idleMinutesBeforeSleep": p.IdleMin, "graceMinutesAfterWake": p.GraceMin,
				"stayAwakeForever": p.StayAwakeForever, "stayAwakeUntil": uk(p.StayAwakeUntil),
			}
			var wakes []string
			for _, w := range p.NextWakes {
				wakes = append(wakes, ukStamp(w.At)+" "+strings.TrimSuffix(w.Unit, ".timer"))
			}
			out["nextScheduledWakes"] = wakes
			return out
		})},
		{Name: "wake_history", Description: "Today's awake and asleep periods with wake reasons.", Run: none(func() any {
			var segs []string
			for _, seg := range snap.Timeline {
				to := "now"
				if seg.To != nil {
					to = ukStamp(*seg.To)
				}
				segs = append(segs, fmt.Sprintf("%s %s → %s %s", seg.State, ukStamp(seg.From), to, seg.Reason))
			}
			return segs
		})},
		{Name: "containers", Description: "Docker containers: name, pinned image tag, health, uptime, ports.", Run: none(func() any {
			var out []map[string]any
			for _, c := range snap.Containers {
				out = append(out, map[string]any{"name": c.Name, "image": c.Image + ":" + c.Tag, "health": c.Health, "status": c.Status, "ports": c.Ports})
			}
			return out
		})},
		{Name: "backups", Description: "Nightly system backup: last run result and the archive files.", Run: none(func() any {
			out := map[string]any{}
			if r := snap.Backups.LastRun; r != nil {
				out["lastRun"] = map[string]any{"start": ukStamp(r.Start), "end": ukStamp(r.End), "ok": r.OK, "summary": r.Summary}
			}
			var files []string
			for _, f := range snap.Backups.Files {
				files = append(files, fmt.Sprintf("%s %.1f GB encrypted=%v", ukStamp(f.At), float64(f.Bytes)/1e9, f.Encrypted))
			}
			out["files"] = files
			out["note"] = "Backups sit on the same USB disk as the media; media itself is not backed up."
			return out
		})},
		{Name: "disk_usage", Description: "Media disk size, free space and USB link speed.", Run: none(func() any {
			if snap.Disk == nil {
				return "no disk data"
			}
			d := snap.Disk
			return map[string]any{"totalGB": d.TotalBytes / 1e9, "freeGB": d.FreeBytes / 1e9, "usbLinkMbps": d.LinkMbps}
		})},
		{Name: "security_status", Description: "ClamAV last scan, firewall lockdown, fail2ban bans.", Run: none(func() any {
			if snap.Security == nil {
				return "no security data"
			}
			sec := snap.Security
			out := map[string]any{"firewallLockdownIntact": sec.Lockdown && sec.PanelDrop, "fail2banBanned": sec.Banned}
			if sec.ClamAV != nil {
				out["clamav"] = map[string]any{"finished": ukStamp(sec.ClamAV.At), "files": sec.ClamAV.Files, "infected": sec.ClamAV.Infected}
			}
			return out
		})},
		{Name: "changelog_recent", Description: "Recent actions taken through the panel.", Run: func(json.RawMessage) (any, error) {
			var out []string
			for _, e := range s.store.RecentAudit(15) {
				out = append(out, ukStamp(e.At)+" "+e.What)
			}
			return out, nil
		}},
		{Name: "propose_wake", Description: "Propose waking cloud64. The owner confirms it in the panel.", Proposal: true,
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"label": map[string]any{"type": "string", "description": "Button text, e.g. 'Wake cloud64'"},
			}}},
	}
}

func uk(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return ukStamp(*t)
}
