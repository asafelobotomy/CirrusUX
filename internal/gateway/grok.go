package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
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
- You cannot run anything yourself. When an action would help, call one propose_ tool; the owner sees a button and confirms it. Propose at most one action per answer, and only when it clearly helps.
- Waking is only possible while cloud64 sleeps; every other action only while it's awake.
- Updates and deleting things aren't available: describe them instead.
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
	reply.Proposal = s.checkProposal(reply.Proposal, view, snap)
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
	tools := []grok.Tool{
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
			return map[string]any{"totalGB": round1(float64(d.TotalBytes) / 1e9), "freeGB": round1(float64(d.FreeBytes) / 1e9), "usbLinkMbps": d.LinkMbps}
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
	}
	return append(tools, proposalTools(snap)...)
}

// proposalTools let Grok suggest an action; the owner confirms each one.
func proposalTools(snap *snapshot.Snapshot) []grok.Tool {
	label := map[string]any{"type": "string", "description": "Short button text, e.g. 'Restart Jellyfin'"}
	obj := func(props map[string]any, required ...string) map[string]any {
		props["label"] = label
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	var names []string
	if snap != nil {
		for _, c := range snap.Containers {
			names = append(names, c.Name)
		}
	}
	container := map[string]any{"name": map[string]any{"type": "string", "enum": names}}
	return []grok.Tool{
		{Name: "propose_wake", Action: "wake", Proposal: true, Description: "Propose waking cloud64.", Params: obj(map[string]any{})},
		{Name: "propose_stay_awake", Action: "stay_awake", Proposal: true, Description: "Propose keeping cloud64 awake.",
			Params: obj(map[string]any{"minutes": map[string]any{"type": "string", "enum": []string{"30", "120", "forever"}}}, "minutes")},
		{Name: "propose_allow_sleep", Action: "allow_sleep", Proposal: true, Description: "Propose removing a stay-awake hold.", Params: obj(map[string]any{})},
		{Name: "propose_restart_container", Action: "container_restart", Proposal: true, Description: "Propose restarting a container.", Params: obj(container, "name")},
		{Name: "propose_start_container", Action: "container_start", Proposal: true, Description: "Propose starting a stopped container.", Params: obj(container, "name")},
		{Name: "propose_run_backup", Action: "backup_run", Proposal: true, Description: "Propose running the system backup now.", Params: obj(map[string]any{})},
	}
}

// checkProposal drops a proposal that couldn't run right now, so the UI
// never shows a button that would fail.
func (s *Server) checkProposal(p *grok.Proposal, view PowerView, snap *snapshot.Snapshot) *grok.Proposal {
	if p == nil {
		return nil
	}
	if p.Action == "wake" {
		if view.State == "awake" {
			return nil
		}
		p.Args = nil
		return p
	}
	if view.State != "awake" {
		return nil
	}
	clean, _, err := ActionReq{Action: p.Action, Args: p.Args}.Validate(snap)
	if err != nil {
		return nil
	}
	p.Args = clean.Args
	return p
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func uk(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return ukStamp(*t)
}
