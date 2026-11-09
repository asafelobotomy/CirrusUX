package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/assistant"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/storage"
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
	snap.Features = []string{"backup", "sleep"}
	snap.Backups.LastRun = &snapshot.BackupRun{Start: end.Add(-time.Minute), End: end, OK: true}
	snap.Security = &snapshot.Security{Firewall: []snapshot.FirewallCheck{{Chain: "DOCKER-USER", OK: true}}}
	// Awake from midnight, asleep, then woken once: one wake, not two.
	at := func(h int) *time.Time { t := now.Add(time.Duration(h-8) * time.Hour); return &t }
	snap.Timeline = []snapshot.Segment{{State: "awake", From: *at(0), To: at(1)}, {State: "asleep", From: *at(1), To: at(6)}, {State: "awake", From: *at(6)}}
	d := buildDigest(time.UTC, now, &snap, []AuditEntry{{At: now.Add(-time.Hour)}, {At: now.Add(-48 * time.Hour)}})
	all := strings.Join(d.Lines, "\n")
	for _, want := range []string{"Backup: OK", "1 of 2 not running well: nginx_server (exited)", "firewall rules intact", "last 24 h: 1", "5 h 0 min asleep, 1 wake."} {
		if !strings.Contains(all, want) {
			t.Errorf("digest missing %q:\n%s", want, all)
		}
	}
	// No backup module: no backup line at all.
	snap.Features = nil
	if all := strings.Join(buildDigest(time.UTC, now, &snap, nil).Lines, "\n"); strings.Contains(all, "Backup") {
		t.Errorf("backup line without the backup module:\n%s", all)
	}
	if d.Day != "2026-11-02" {
		t.Errorf("day = %s", d.Day)
	}
}

func TestAppActions(t *testing.T) {
	snap := awakeSnap()
	ok := ActionReq{Action: "app_install", Args: map[string]string{"app": "navidrome", "options": `{"media":{"music":"/srv/media/music"}}`}}
	if c, spec, err := ok.Validate(&snap); err != nil || spec.label(c.Args) != "Started installing Navidrome" || spec.undo(c.Args).Action != "app_remove" {
		t.Errorf("install: %v", err)
	}
	for name, req := range map[string]ActionReq{
		"bad app id":           {Action: "app_install", Args: map[string]string{"app": "../etc"}},
		"bad source":           {Action: "app_install", Args: map[string]string{"app": "navidrome", "store": "../x"}},
		"unknown option field": {Action: "app_install", Args: map[string]string{"app": "navidrome", "options": `{"volumes":["/:/host"]}`}},
		"remove not installed": {Action: "app_remove", Args: map[string]string{"app": "navidrome"}},
		"options carry a key":  {Action: "app_install", Args: map[string]string{"app": "navidrome", "options": `{}, "action":"remove"`}},
		"trailing data":        {Action: "app_install", Args: map[string]string{"app": "navidrome", "options": `{} {}`}},
	} {
		if _, _, err := req.Validate(&snap); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	snap.Apps = []snapshot.App{{ID: "navidrome", Name: "Navidrome"}}
	if _, _, err := ok.Validate(&snap); err == nil {
		t.Error("installing an installed app")
	}
	if _, _, err := (ActionReq{Action: "app_remove", Args: map[string]string{"app": "navidrome"}}).Validate(&snap); err != nil {
		t.Errorf("remove: %v", err)
	}

	s, h := newTestServer(t)
	snap.Apps = nil
	_ = s.store.SaveSnapshot(snap)
	if w := do(h, "POST", "/api/actions", `{"action":"app_install","args":{"app":"bitcoin-miner"}}`, actor); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no app") {
		t.Errorf("unknown app: %d %s", w.Code, w.Body)
	}
	w := do(h, "GET", "/api/catalogue", "", owner)
	var apps []struct{ ID, Tag, Digest string }
	_ = json.Unmarshal(w.Body.Bytes(), &apps)
	if w.Code != http.StatusOK || len(apps) < 9 || apps[0].Digest == "" {
		t.Errorf("catalogue: %d %d", w.Code, len(apps))
	}
}

func TestAppPreview(t *testing.T) {
	s, h := newTestServer(t)
	post := func(body string) map[string]any {
		w := do(h, "POST", "/api/apps/preview", body, actor)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		out["code"] = float64(w.Code)
		return out
	}
	if got := post(`{"app":"kavita"}`); got["code"] != float64(http.StatusConflict) {
		t.Errorf("before the server reports its settings: %v", got)
	}
	snap := awakeSnap()
	snap.Containers[0].Ports = []int{5000}
	snap.AppsHost = &helperapi.AppsInfo{Runtime: "podman", AppsDir: "/srv/cirrusux/apps", MediaRoots: []string{"/srv/media"}, TZ: "UTC", UID: 990, GID: 990}
	_ = s.store.SaveSnapshot(snap)
	got := post(`{"app":"kavita","options":{"media":{"books":"/srv/media/books"}}}`)
	if got["file"] != "/etc/containers/systemd/cirrusux-kavita.container" || !strings.Contains(got["content"].(string), "Volume=/srv/media/books:/books:ro") {
		t.Errorf("preview = %v", got)
	}
	if w, _ := got["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "5000 is already used by kavita") {
		t.Errorf("warnings = %v", got["warnings"])
	}
	if got := post(`{"app":"kavita","options":{"media":{"books":"/etc"}}}`); !strings.Contains(got["error"].(string), "isn't inside the media folders") {
		t.Errorf("bad folder: %v", got)
	}
	if got := post(`{"app":"kavita","options":{},"extra":1}`); got["code"] != float64(http.StatusBadRequest) {
		t.Errorf("unknown field: %v", got)
	}
}

func TestOSAndLogActions(t *testing.T) {
	snap := awakeSnap()
	for name, req := range map[string]ActionReq{
		"os check":       {Action: "os_check"},
		"os upgrade":     {Action: "os_upgrade"},
		"container logs": {Action: "logs", Args: map[string]string{"source": "container:kavita", "lines": "200"}},
		"unit journal":   {Action: "logs", Args: map[string]string{"source": "unit:cirrusux-backup.service"}},
	} {
		if _, _, err := req.Validate(&snap); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, req := range map[string]ActionReq{
		"unknown container": {Action: "logs", Args: map[string]string{"source": "container:mystery"}},
		"other unit":        {Action: "logs", Args: map[string]string{"source": "unit:sshd.service"}},
		"sneaky unit":       {Action: "logs", Args: map[string]string{"source": "unit:cirrusux-x.service --all"}},
		"too many lines":    {Action: "logs", Args: map[string]string{"source": "container:kavita", "lines": "100000"}},
		"no kind":           {Action: "logs", Args: map[string]string{"source": "/var/log/syslog"}},
	} {
		if _, _, err := req.Validate(&snap); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if !actionSpecs["os_check"].readOnly || !actionSpecs["logs"].readOnly || actionSpecs["os_upgrade"].readOnly || actionSpecs["os_upgrade"].undo != nil {
		t.Error("audit and undo flags")
	}
}

// Deleting from the archive needs the entry the server reported, and its
// name typed again.
func TestArchiveDelete(t *testing.T) {
	snap := awakeSnap()
	del := func(name, confirm string) error {
		_, _, err := ActionReq{Action: "archive_delete", Args: map[string]string{"name": name, "confirm": confirm}}.Validate(&snap)
		return err
	}
	if del("navidrome-20261101-120000", "navidrome-20261101-120000") == nil {
		t.Error("deleted with no archive reported")
	}
	snap.Storage = &storage.Result{Archive: []storage.Entry{{Name: "navidrome-20261101-120000", Kind: "removed-app"}}}
	if del("navidrome-20261101-120000", "") == nil || del("navidrome-20261101-120000", "navidrome") == nil {
		t.Error("deleted without the name typed exactly")
	}
	if del("../../etc", "../../etc") == nil {
		t.Error("deleted something not in the archive")
	}
	if err := del("navidrome-20261101-120000", "navidrome-20261101-120000"); err != nil {
		t.Errorf("proper delete refused: %v", err)
	}
	if actionSpecs["archive_delete"].undo != nil || actionSpecs["archive_delete"].readOnly {
		t.Error("a delete must be audited and has no undo")
	}
}
