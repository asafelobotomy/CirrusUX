package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/assistant"
	"cirrusux/internal/snapshot"
)

var actor = map[string]string{"Tailscale-User-Login": "owner@example.com", "X-Cirrus-Request": "1"}

func awakeSnap() snapshot.Snapshot {
	return snapshot.Snapshot{
		Host: "atlas", TakenAt: time.Now(),
		Containers: []snapshot.Container{{Name: "kavita", Health: "running"}, {Name: "nginx_server", Health: "exited"}},
	}
}

func TestActionValidation(t *testing.T) {
	snap := awakeSnap()
	ok := []ActionReq{
		{Action: "stay_awake", Args: map[string]string{"minutes": "120"}},
		{Action: "allow_sleep"},
		{Action: "container_restart", Args: map[string]string{"name": "kavita"}},
		{Action: "backup_run", Args: map[string]string{"name": "ignored"}},
	}
	bad := []ActionReq{
		{Action: "stay_awake", Args: map[string]string{"minutes": "9999"}},
		{Action: "container_restart", Args: map[string]string{"name": "../etc"}},
		{Action: "container_stop", Args: map[string]string{"name": "unknown"}},
		{Action: "rm_rf"},
	}
	for _, a := range ok {
		if _, _, err := a.Validate(&snap); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if c, _, _ := (ActionReq{Action: "allow_sleep", Args: map[string]string{"path": "/etc"}}).Validate(&snap); c.Args != nil {
		t.Errorf("unknown args should be dropped: %+v", c)
	}
	for _, a := range bad {
		if _, _, err := a.Validate(&snap); err == nil {
			t.Errorf("%+v should be rejected", a)
		}
	}
}

func TestActionNeedsAwake(t *testing.T) {
	_, h := newTestServer(t)
	w := do(h, "POST", "/api/actions", `{"action":"backup_run"}`, actor)
	if w.Code != http.StatusConflict {
		t.Errorf("asleep: %d %s", w.Code, w.Body)
	}
}

func TestActionRoundTrip(t *testing.T) {
	s, h := newTestServer(t)
	if err := s.store.SaveSnapshot(awakeSnap()); err != nil {
		t.Fatal(err)
	}
	w := do(h, "POST", "/api/actions", `{"action":"stay_awake","args":{"minutes":"30"}}`, actor)
	if w.Code != http.StatusAccepted || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	agent := map[string]string{"Authorization": "Bearer " + token}
	if w := do(h, "GET", "/agent/commands", "", map[string]string{"Authorization": "Bearer nope"}); w.Code != http.StatusUnauthorized {
		t.Errorf("commands with bad token: %d", w.Code)
	}
	w = do(h, "GET", "/agent/commands", "", agent)
	var cmd struct {
		ID     string
		Action string
		Args   map[string]string
	}
	_ = json.Unmarshal(w.Body.Bytes(), &cmd)
	if cmd.ID != created.ID || cmd.Action != "stay_awake" || cmd.Args["minutes"] != "30" {
		t.Fatalf("agent got %+v (%s)", cmd, w.Body)
	}
	if w := do(h, "POST", "/agent/results", `{"id":"`+cmd.ID+`","ok":true,"output":["held"]}`, agent); w.Code != http.StatusNoContent {
		t.Fatalf("result: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/agent/results", `{"id":"`+cmd.ID+`","ok":true}`, agent); w.Code != http.StatusNotFound {
		t.Errorf("second result for the same command: %d", w.Code)
	}

	w = do(h, "GET", "/api/actions/"+created.ID, "", owner)
	var got Command
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.State != "done" || got.Undo == nil || got.Undo.Action != "allow_sleep" {
		t.Errorf("command = %+v", got)
	}
	audit := s.store.RecentAudit(5)
	if len(audit) != 1 || audit[0].What != "Kept atlas awake for 30 min" || audit[0].UndoAction == nil || audit[0].Who != "owner@example.com" {
		t.Errorf("audit = %+v", audit)
	}
}

func TestReadOnlyActionNotAudited(t *testing.T) {
	s, h := newTestServer(t)
	_ = s.store.SaveSnapshot(awakeSnap())
	do(h, "POST", "/api/actions", `{"action":"egress_report"}`, actor)
	c := s.queue.Next(context.Background())
	if w := do(h, "POST", "/agent/results", `{"id":"`+c.ID+`","ok":true,"output":["x"]}`, map[string]string{"Authorization": "Bearer " + token}); w.Code != http.StatusNoContent {
		t.Fatalf("result: %d", w.Code)
	}
	if n := len(s.store.RecentAudit(5)); n != 0 {
		t.Errorf("read-only action audited %d times", n)
	}
}

func TestQueueExpiry(t *testing.T) {
	q := NewQueue()
	c := &Command{ActionReq: ActionReq{Action: "backup_run"}}
	q.Add(c)
	q.cmds[0].Created = time.Now().Add(-3 * time.Minute)
	got, _ := q.Get(c.ID)
	if got.State != "failed" || !strings.Contains(got.Error, "didn't pick it up") {
		t.Errorf("stale pending = %+v", got)
	}
}

func TestLongPollWaitsForWork(t *testing.T) {
	q := NewQueue()
	done := make(chan *Command)
	go func() { done <- q.Next(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	q.Add(&Command{ActionReq: ActionReq{Action: "allow_sleep"}})
	select {
	case c := <-done:
		if c == nil || c.State != "sent" {
			t.Errorf("got %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll didn't wake up for new work")
	}
}

func TestCheckProposal(t *testing.T) {
	s, _ := newTestServer(t)
	snap := awakeSnap()
	awake, asleep := PowerView{State: "awake"}, PowerView{State: "asleep"}
	if s.checkProposal(&assistant.Proposal{Action: "wake"}, awake, &snap) != nil {
		t.Error("wake proposed while awake")
	}
	if s.checkProposal(&assistant.Proposal{Action: "container_restart", Args: map[string]string{"name": "kavita"}}, asleep, &snap) != nil {
		t.Error("restart proposed while asleep")
	}
	if s.checkProposal(&assistant.Proposal{Action: "container_restart", Args: map[string]string{"name": "made-up"}}, awake, &snap) != nil {
		t.Error("restart of an unknown container proposed")
	}
	if s.checkProposal(&assistant.Proposal{Action: "container_restart", Args: map[string]string{"name": "kavita"}}, awake, &snap) == nil {
		t.Error("valid restart dropped")
	}
}

func TestDigest(t *testing.T) {
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	end := now.Add(-5 * time.Hour)
	snap := awakeSnap()
	snap.TakenAt = now.Add(-time.Hour)
	snap.Backups.LastRun = &snapshot.BackupRun{Start: end.Add(-time.Minute), End: end, OK: true}
	snap.Security = &snapshot.Security{Firewall: []snapshot.FirewallCheck{{Chain: "DOCKER-USER", OK: true}}}
	d := buildDigest(now, &snap, []AuditEntry{{At: now.Add(-time.Hour)}, {At: now.Add(-48 * time.Hour)}})
	all := strings.Join(d.Lines, "\n")
	for _, want := range []string{"Backup: OK", "1 of 2 not running well: nginx_server (exited)", "firewall rules intact", "last 24 h: 1"} {
		if !strings.Contains(all, want) {
			t.Errorf("digest missing %q:\n%s", want, all)
		}
	}
	if d.Day != "2026-11-02" {
		t.Errorf("day = %s", d.Day)
	}
}
