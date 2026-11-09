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

	"cirrusux/internal/assistant"
	"cirrusux/internal/snapshot"
)

// systemPrompt tells the model what it is looking at. The server's own
// description comes from CIRRUS_SERVER_ABOUT.
func (s *Server) systemPrompt() string {
	c := s.cfg
	name := c.ServerName
	var b strings.Builder
	fmt.Fprintf(&b, "You are the assistant inside CirrusUX, the web panel for the server %q.", name)
	if c.ServerAbout != "" {
		b.WriteString(" " + c.ServerAbout)
	}
	if c.Split() {
		fmt.Fprintf(&b, " %s sleeps when idle; the panel runs on another machine and keeps its last report.", name)
	}
	fmt.Fprintf(&b, "\n\nYou answer from the panel's cached data, gathered by tools.")
	if c.Split() {
		b.WriteString(" Asking you never wakes the server.")
	}
	fmt.Fprintf(&b, " Times are in %s.\n\nRules:\n", c.TZName)
	b.WriteString("- Use the tools to check facts before answering. Say how old the data is when it isn't fresh.\n")
	b.WriteString("- You cannot run anything yourself. When an action would help, call one propose_ tool; the owner sees a button and confirms it. Propose at most one action per answer, and only when it clearly helps.\n")
	if c.Split() {
		fmt.Fprintf(&b, "- Waking is only possible while %s sleeps; every other action only while it's awake.\n", name)
	}
	b.WriteString("- Updates and deleting things aren't available: describe them instead.\n")
	b.WriteString("- Never ask for or repeat passwords, keys or tokens. Some values in tool results are masked on purpose.\n")
	b.WriteString("- Be brief and concrete: a few sentences, no headings.")
	return b.String()
}

func (s *Server) askAssistant(w http.ResponseWriter, r *http.Request) {
	if s.assistant == nil {
		http.Error(w, "The assistant is not set up on the gateway (ASSISTANT_MODEL and, for hosted providers, ASSISTANT_API_KEY)", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Messages []assistant.Turn `json:"messages"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil || len(body.Messages) == 0 {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if len(body.Messages) > 20 {
		body.Messages = body.Messages[len(body.Messages)-20:]
	}

	prompt := 0
	for _, t := range body.Messages {
		prompt += len(t.Text)
	}
	// Reserve an estimate up front so parallel questions can't all pass the
	// check; settle with the real count afterwards.
	reserved := prompt/charsPerToken + replyAllowance
	if !s.store.ReserveAssistant(reserved, s.cfg.AssistantDailyTokens) {
		http.Error(w, s.cfg.Assistant.Name+"'s daily token budget is used up", http.StatusTooManyRequests)
		return
	}

	snap := s.store.Snapshot()
	view := s.power.View(time.Now(), snap)
	system := s.systemPrompt() + "\n\nNow: " + s.stamp(time.Now()) + ". Server state: " + view.State + "."
	if snap != nil {
		system += " Last snapshot: " + s.stamp(snap.TakenAt) + "."
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	reply, err := s.assistant.Ask(ctx, system, body.Messages, s.assistantTools(snap, view))
	s.store.AssistantUsage(settleDelta(reserved, reply, len(system)+prompt))
	if err != nil {
		slog.Warn("assistant", "err", err)
		http.Error(w, s.cfg.Assistant.Name+" could not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	reply.Proposal = s.checkProposal(reply.Proposal, view, snap)
	writeJSON(w, map[string]any{"text": reply.Text, "tools": reply.Tools, "proposal": reply.Proposal, "cached": view.State != "awake"})
}

const (
	charsPerToken  = 4    // rough, for providers that report no usage
	replyAllowance = 1000 // tokens held back for the answer and tool rounds
)

// settleDelta is what to add to today's count once the real use is known,
// after reserved was already counted. A provider that reports no usage is
// estimated from the characters sent and received.
func settleDelta(reserved int, reply assistant.Reply, sentChars int) int {
	used := reply.TokensIn + reply.TokensOut
	if used == 0 {
		used = (sentChars + len(reply.Text)) / charsPerToken
	}
	return used - reserved
}

// assistantTools exposes the cached snapshot in small, summarised pieces.
func (s *Server) assistantTools(snap *snapshot.Snapshot, view PowerView) []assistant.Tool {
	none := func(f func() any) func(json.RawMessage) (any, error) {
		return func(json.RawMessage) (any, error) {
			if snap == nil {
				return nil, fmt.Errorf("no data from the server yet")
			}
			return f(), nil
		}
	}
	tools := []assistant.Tool{
		{Name: "power_status", Description: "Awake/asleep, since when, what keeps it awake, sleep settings and next scheduled wakes.", Run: none(func() any {
			p := snap.Power
			out := map[string]any{
				"state": view.State, "since": s.stampOf(view.Since), "snapshotTaken": s.stamp(snap.TakenAt),
				"activityReasons": p.Reasons, "lastActivity": s.stampOf(p.LastActivity),
				"idleMinutesBeforeSleep": p.IdleMin, "graceMinutesAfterWake": p.GraceMin,
				"stayAwakeForever": p.StayAwakeForever, "stayAwakeUntil": s.stampOf(p.StayAwakeUntil),
			}
			var wakes []string
			for _, w := range p.NextWakes {
				wakes = append(wakes, s.stamp(w.At)+" "+strings.TrimSuffix(w.Unit, ".timer"))
			}
			out["nextScheduledWakes"] = wakes
			return out
		})},
		{Name: "wake_history", Description: "Today's awake and asleep periods with wake reasons.", Run: none(func() any {
			var segs []string
			for _, seg := range snap.Timeline {
				to := "now"
				if seg.To != nil {
					to = s.stamp(*seg.To)
				}
				segs = append(segs, fmt.Sprintf("%s %s → %s %s", seg.State, s.stamp(seg.From), to, seg.Reason))
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
		{Name: "backups", Description: "System backups: the last run's result and the archive files.", Run: none(func() any {
			out := map[string]any{}
			if r := snap.Backups.LastRun; r != nil {
				out["lastRun"] = map[string]any{"start": s.stamp(r.Start), "end": s.stamp(r.End), "ok": r.OK, "summary": r.Summary}
			}
			var files []string
			for _, f := range snap.Backups.Files {
				files = append(files, fmt.Sprintf("%s %.1f GB encrypted=%v", s.stamp(f.At), float64(f.Bytes)/1e9, f.Encrypted))
			}
			out["files"] = files
			out["note"] = "Backups are encrypted; the private key isn't on the server. Media folders aren't in them unless BACKUP_PATHS says so."
			return out
		})},
		{Name: "disk_usage", Description: "The watched disk's size and free space, and its USB link speed when it's on USB (0 otherwise).", Run: none(func() any {
			if snap.Disk == nil {
				return "no disk data"
			}
			d := snap.Disk
			return map[string]any{"totalGB": round1(float64(d.TotalBytes) / 1e9), "freeGB": round1(float64(d.FreeBytes) / 1e9), "usbLinkMbps": d.LinkMbps}
		})},
		{Name: "security_status", Description: "Virus scan, firewall rule checks, fail2ban bans.", Run: none(func() any {
			if snap.Security == nil {
				return "no security data"
			}
			sec := snap.Security
			out := map[string]any{"firewallChecks": sec.Firewall}
			if sec.Banned != nil {
				out["fail2banBanned"] = *sec.Banned
			}
			if sec.ClamAV != nil {
				out["clamav"] = map[string]any{"finished": s.stamp(sec.ClamAV.At), "files": sec.ClamAV.Files, "infected": sec.ClamAV.Infected}
			}
			return out
		})},
		{Name: "changelog_recent", Description: "Recent actions taken through the panel.", Run: func(json.RawMessage) (any, error) {
			var out []string
			for _, e := range s.store.RecentAudit(15) {
				out = append(out, s.stamp(e.At)+" "+e.What)
			}
			return out, nil
		}},
	}
	return append(tools, proposalTools(snap)...)
}

// proposalTools let the assistant suggest an action; the owner confirms each one.
func proposalTools(snap *snapshot.Snapshot) []assistant.Tool {
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
	return []assistant.Tool{
		{Name: "propose_wake", Action: "wake", Proposal: true, Description: "Propose waking the server.", Params: obj(map[string]any{})},
		{Name: "propose_stay_awake", Action: "stay_awake", Proposal: true, Description: "Propose keeping the server awake.",
			Params: obj(map[string]any{"minutes": map[string]any{"type": "string", "enum": []string{"30", "120", "forever"}}}, "minutes")},
		{Name: "propose_allow_sleep", Action: "allow_sleep", Proposal: true, Description: "Propose removing a stay-awake hold.", Params: obj(map[string]any{})},
		{Name: "propose_restart_container", Action: "container_restart", Proposal: true, Description: "Propose restarting a container.", Params: obj(container, "name")},
		{Name: "propose_start_container", Action: "container_start", Proposal: true, Description: "Propose starting a stopped container.", Params: obj(container, "name")},
		{Name: "propose_run_backup", Action: "backup_run", Proposal: true, Description: "Propose running the system backup now.", Params: obj(map[string]any{})},
	}
}

// checkProposal drops a proposal that couldn't run right now, so the UI
// never shows a button that would fail.
func (s *Server) checkProposal(p *assistant.Proposal, view PowerView, snap *snapshot.Snapshot) *assistant.Proposal {
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
	if s.agentRefuses(clean.Action, snap) != "" {
		return nil
	}
	p.Args = clean.Args
	return p
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func (s *Server) stampOf(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return s.stamp(*t)
}
