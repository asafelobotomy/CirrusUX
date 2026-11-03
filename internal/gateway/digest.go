package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cirrusux/internal/assistant"
	"cirrusux/internal/snapshot"
)

// The daily digest (PLAN §3.6): a morning summary built on the gateway from
// the cached snapshot and the audit log, so it never wakes a sleeping
// server. With CIRRUS_DIGEST_ASSISTANT=1 the assistant adds a short written
// summary on top of the plain facts; without it the digest sends nothing out.

const digestHour = 8 // local time (CIRRUS_TZ)

type Digest struct {
	Day     string    `json:"day"` // date in CIRRUS_TZ, 2006-01-02
	At      time.Time `json:"at"`
	Lines   []string  `json:"lines"`
	Summary string    `json:"summary,omitempty"` // from the assistant
}

func buildDigest(now time.Time, snap *snapshot.Snapshot, audit []AuditEntry) Digest {
	d := Digest{Day: now.In(zone).Format("2006-01-02"), At: now}
	add := func(format string, a ...any) { d.Lines = append(d.Lines, fmt.Sprintf(format, a...)) }
	if snap == nil {
		add("No report from the server yet.")
		return d
	}
	add("Data from %s.", stamp(snap.TakenAt))

	switch r := snap.Backups.LastRun; {
	case !slices.Contains(snap.Features, "backup"):
		// No backup module here: nothing to say.
	case r == nil:
		add("Backup: no run found in the journal.")
	case !r.OK:
		add("Backup: the last run FAILED (%s).", stamp(r.End))
	case now.Sub(r.End) > 26*time.Hour:
		add("Backup: last good run was %s, more than a day ago.", stamp(r.End))
	default:
		size := ""
		if len(snap.Backups.Files) > 0 {
			size = fmt.Sprintf(", %.1f GB", float64(snap.Backups.Files[0].Bytes)/1e9)
		}
		add("Backup: OK at %s%s.", stamp(r.End), size)
	}

	var down []string
	for _, c := range snap.Containers {
		if c.Health != "healthy" && c.Health != "running" {
			down = append(down, c.Name+" ("+c.Health+")")
		}
	}
	if len(down) == 0 {
		add("Containers: all %d running.", len(snap.Containers))
	} else {
		add("Containers: %d of %d not running well: %s.", len(down), len(snap.Containers), strings.Join(down, ", "))
	}

	var asleep time.Duration
	wakes := 0
	for i, seg := range snap.Timeline {
		end := snap.TakenAt
		if seg.To != nil {
			end = *seg.To
		}
		if seg.State == "asleep" {
			asleep += end.Sub(seg.From)
		} else if i > 0 && snap.Timeline[i-1].State == "asleep" {
			wakes++ // the day's first stretch awake isn't a wake
		}
	}
	if len(snap.Timeline) > 0 {
		plural := "s"
		if wakes == 1 {
			plural = ""
		}
		add("Sleep so far today: %s asleep, %d wake%s.", hoursMinutes(asleep), wakes, plural)
	}

	if sec := snap.Security; sec != nil {
		parts := []string{}
		if sec.ClamAV != nil {
			parts = append(parts, fmt.Sprintf("ClamAV %d infected (scan %s)", sec.ClamAV.Infected, stamp(sec.ClamAV.At)))
		}
		if len(sec.Firewall) > 0 {
			if bad := sec.FirewallFailing(); len(bad) > 0 {
				parts = append(parts, "firewall rules MISSING in "+strings.Join(bad, ", "))
			} else {
				parts = append(parts, "firewall rules intact")
			}
		}
		if sec.Banned != nil {
			parts = append(parts, fmt.Sprintf("%d banned by fail2ban", *sec.Banned))
		}
		add("Security: %s.", strings.Join(parts, "; "))
	}

	if dk := snap.Disk; dk != nil {
		line := fmt.Sprintf("Disk: %.0f GB free of %.1f TB", float64(dk.FreeBytes)/1e9, float64(dk.TotalBytes)/1e12)
		if dk.LinkMbps > 0 && dk.LinkMbps <= 480 {
			line += ", on a slow USB 2 link"
		}
		add("%s.", line)
	}

	n := 0
	for _, e := range audit {
		if now.Sub(e.At) <= 24*time.Hour {
			n++
		}
	}
	add("Panel actions in the last 24 h: %d.", n)
	if len(snap.Errors) > 0 {
		add("The agent couldn't collect %d section(s): %s.", len(snap.Errors), strings.Join(snap.Errors, "; "))
	}
	return d
}

// RunDigest builds the digest once a day after digestHour, in CIRRUS_TZ.
func (s *Server) RunDigest(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		now := time.Now()
		// Wait for data: a digest made before the first snapshot would say
		// nothing and then stand for the whole day.
		if now.In(zone).Hour() >= digestHour && s.store.Snapshot() != nil && s.store.Digest().Day != now.In(zone).Format("2006-01-02") {
			s.makeDigest(ctx, now)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) makeDigest(ctx context.Context, now time.Time) Digest {
	d := buildDigest(now, s.store.Snapshot(), s.store.RecentAudit(100))
	if s.cfg.DigestAssistant && s.assistant != nil && s.store.AssistantUsage(0) < s.cfg.AssistantDailyTokens {
		gctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		reply, err := s.assistant.Ask(gctx, s.systemPrompt(), []assistant.Turn{{Role: "user", Text: "Write this morning's digest for " + s.cfg.ServerName + " in two or three sentences: anything that needs attention first, then what's fine. Use only these facts:\n- " + strings.Join(d.Lines, "\n- ")}}, nil)
		cancel()
		s.store.AssistantUsage(reply.TokensIn + reply.TokensOut)
		if err != nil {
			slog.Warn("digest summary", "err", err)
		} else {
			d.Summary = reply.Text
		}
	}
	if err := s.store.SaveDigest(d); err != nil {
		slog.Error("save digest", "err", err)
	}
	return d
}

func (s *Server) getDigest(w http.ResponseWriter, r *http.Request) {
	d := s.store.Digest()
	if d.Day == "" {
		// Before the first morning run: the facts now, without the assistant.
		d = buildDigest(time.Now(), s.store.Snapshot(), s.store.RecentAudit(100))
	}
	writeJSON(w, d)
}

func (s *Store) Digest() Digest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var d Digest
	_ = readFileJSON(filepath.Join(s.dir, "digest.json"), &d)
	return d
}

func (s *Store) SaveDigest(d Digest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileJSON(filepath.Join(s.dir, "digest.json"), d)
}

// hoursMinutes formats 13h38m as "13 h 38 min".
func hoursMinutes(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}
