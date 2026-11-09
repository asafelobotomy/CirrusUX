package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// postGit sends an action and waits for it to finish.
func postGit(t *testing.T, h http.Handler, req ActionReq) (int, Command) {
	t.Helper()
	b, _ := json.Marshal(req)
	w := do(h, "POST", "/api/actions", string(b), actor)
	if w.Code != http.StatusAccepted {
		return w.Code, Command{Error: strings.TrimSpace(w.Body.String())}
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	return w.Code, waitAction(t, h, created.ID)
}

func waitAction(t *testing.T, h http.Handler, id string) Command {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var c Command
		w := do(h, "GET", "/api/actions/"+id, "", actor)
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		if c.State != "sent" && c.State != "pending" || time.Now().After(deadline) {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func devMode(c *Config) { c.Dev = true }

func lastAudit(s *Server) AuditEntry {
	a := s.store.RecentAudit(1)
	if len(a) == 0 {
		return AuditEntry{}
	}
	return a[0]
}

func TestGitActionsAuditAndUndo(t *testing.T) {
	s, h := newGitServer(t)
	if code, c := postGit(t, h, ActionReq{Action: "git_create", Args: map[string]string{"name": "notes", "description": "Notes"}}); code != http.StatusAccepted || c.State != "done" {
		t.Fatalf("create: %d %+v", code, c)
	}
	e := lastAudit(s)
	if e.What != "Created repository notes" || e.Who != "owner@example.com" || e.UndoAction == nil ||
		e.UndoAction.Action != "git_delete" || e.UndoAction.Args["name"] != "notes" ||
		e.Undo != "Deleted repository notes (kept in Deleted repositories)" {
		t.Fatalf("audit: %+v", e)
	}
	if _, c := postGit(t, h, *e.UndoAction); c.State != "done" {
		t.Fatalf("undo create: %+v", c)
	}
	if list, _ := s.git.List(t.Context()); len(list) != 0 {
		t.Errorf("still there: %+v", list)
	}
	e = lastAudit(s)
	if e.UndoAction == nil || e.UndoAction.Action != "git_restore" || e.UndoAction.Args["trash"] == "" {
		t.Fatalf("delete's undo: %+v", e)
	}
	if _, c := postGit(t, h, *e.UndoAction); c.State != "done" || !s.git.Exists("notes") {
		t.Fatalf("restore: %+v", c)
	}
	if e := lastAudit(s); e.What != "Restored repository notes" || e.UndoAction.Action != "git_delete" {
		t.Errorf("restore audit: %+v", e)
	}

	postGit(t, h, ActionReq{Action: "git_rename", Args: map[string]string{"name": "notes", "to": "diary"}})
	if e := lastAudit(s); e.What != "Renamed repository notes to diary" || e.UndoAction.Args["name"] != "diary" || e.UndoAction.Args["to"] != "notes" {
		t.Errorf("rename audit: %+v", e)
	}
	postGit(t, h, ActionReq{Action: "git_describe", Args: map[string]string{"name": "diary", "description": "New"}})
	if e := lastAudit(s); e.What != "Changed the description of diary" || e.UndoAction.Args["description"] != "Notes" {
		t.Errorf("describe audit: %+v", e)
	}
}

func TestGitActionsNoWakeNeeded(t *testing.T) {
	_, h := newGitServer(t) // split, no snapshot: asleep
	if code, c := postGit(t, h, ActionReq{Action: "git_create", Args: map[string]string{"name": "notes"}}); code != http.StatusAccepted || c.State != "done" {
		t.Errorf("git while asleep: %d %+v", code, c)
	}
	if code, _ := postGit(t, h, ActionReq{Action: "container_start", Args: map[string]string{"name": "x"}}); code == http.StatusAccepted {
		t.Error("a server action was accepted while asleep")
	}
}

func TestGitActionFailures(t *testing.T) {
	s, h := newGitServer(t)
	_, c := postGit(t, h, ActionReq{Action: "git_rename", Args: map[string]string{"name": "nope", "to": "x"}})
	if c.State != "failed" || c.Error != "There's no repository called nope" {
		t.Errorf("rename missing: %+v", c)
	}
	if e := lastAudit(s); e.What != "Renamed repository nope to x (failed)" || e.UndoAction != nil {
		t.Errorf("failed audit: %+v", e)
	}
	if code, _ := postGit(t, h, ActionReq{Action: "git_create", Args: map[string]string{"name": "../x"}}); code != http.StatusBadRequest {
		t.Errorf("bad name: %d", code)
	}
	s.git = nil
	if code, c := postGit(t, h, ActionReq{Action: "git_create", Args: map[string]string{"name": "x"}}); code != http.StatusNotFound || c.Error != "Git hosting is off" {
		t.Errorf("off: %d %+v", code, c)
	}
}

func TestGitBranchDeleteUndo(t *testing.T) {
	s, h := newGitServer(t, devMode) // the test's git signs in as dev
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=main", "clone", "-q", srv.URL+"/git/notes.git", ".")
	runGit(t, work, "commit", "-q", "--allow-empty", "-m", "One")
	runGit(t, work, "push", "-q", "origin", "HEAD:main", "HEAD:dev", "HEAD:refs/tags/v1")
	sha := runGit(t, work, "rev-parse", "HEAD")

	_, c := postGit(t, h, ActionReq{Action: "git_branch_delete", Args: map[string]string{"name": "notes", "branch": "dev"}})
	e := lastAudit(s)
	if c.State != "done" || e.What != "Deleted branch dev from notes" || e.UndoAction == nil ||
		e.UndoAction.Action != "git_ref_restore" || e.UndoAction.Args["ref"] != "refs/heads/dev" || e.UndoAction.Args["sha"] != sha {
		t.Fatalf("delete: %+v %+v", c, e)
	}
	if _, c := postGit(t, h, *e.UndoAction); c.State != "done" {
		t.Fatalf("restore: %+v", c)
	}
	if e := lastAudit(s); e.What != "Restored dev in notes" || e.UndoAction.Action != "git_branch_delete" || e.UndoAction.Args["branch"] != "dev" {
		t.Errorf("restore audit: %+v", e)
	}
	postGit(t, h, ActionReq{Action: "git_tag_delete", Args: map[string]string{"name": "notes", "tag": "v1"}})
	if e := lastAudit(s); e.What != "Deleted tag v1 from notes" || e.UndoAction.Args["ref"] != "refs/tags/v1" {
		t.Errorf("tag delete audit: %+v", e)
	}
	postGit(t, h, ActionReq{Action: "git_default_branch", Args: map[string]string{"name": "notes", "branch": "dev"}})
	if e := lastAudit(s); e.What != "Made dev the default branch of notes" || e.UndoAction.Args["branch"] != "main" {
		t.Errorf("default branch audit: %+v", e)
	}
}

func TestGitImport(t *testing.T) {
	src, srcH := newGitServer(t)
	if err := src.git.Create(t.Context(), "src", ""); err != nil {
		t.Fatal(err)
	}
	secret, _, _ := src.gitTokens.Create("owner@example.com", "import", "read-write")
	plain := httptest.NewServer(srcH)
	defer plain.Close()
	withToken := func(base string) string {
		return strings.Replace(base, "://", "://git:"+secret+"@", 1) + "/git/src.git"
	}
	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=main", "clone", "-q", withToken(plain.URL), ".")
	runGit(t, work, "commit", "-q", "--allow-empty", "-m", "One")
	runGit(t, work, "push", "-q", "origin", "HEAD:main")
	tls := httptest.NewTLSServer(srcH)
	defer tls.Close()

	s, h := newGitServer(t)
	// The test server is on loopback with its own certificate.
	s.git.InsecureTLS = true
	u := withToken(tls.URL)
	_, c := postGit(t, h, ActionReq{Action: "git_import", Args: map[string]string{"name": "refused", "url": u}})
	if c.State != "failed" || !strings.Contains(c.Error, "CIRRUS_GIT_IMPORT_PRIVATE") {
		t.Errorf("loopback import: %+v", c)
	}
	s.git.AllowPrivateImport = true
	_, c = postGit(t, h, ActionReq{Action: "git_import", Args: map[string]string{"name": "copy", "url": u}})
	if c.State != "done" {
		t.Fatalf("import: %+v", c)
	}
	if r, err := s.git.Info(t.Context(), "copy"); err != nil || len(r.Branches) != 1 || r.Branches[0].Name != "main" {
		t.Errorf("imported: %+v %v", r, err)
	}
	e := lastAudit(s)
	if strings.Contains(e.What, secret) ||
		!strings.Contains(e.What, "as repository copy") || e.UndoAction.Action != "git_delete" {
		t.Errorf("import audit: %+v", e)
	}
	if strings.Contains(c.Label, secret) || strings.Contains(c.Args["url"], secret) {
		t.Errorf("command shows the token: %+v", c)
	}
	_, c = postGit(t, h, ActionReq{Action: "git_import", Args: map[string]string{"name": "other", "url": "http://192.0.2.1/x.git"}})
	if c.State != "failed" || c.Error != "Only https:// addresses can be imported" {
		t.Errorf("http import: %+v", c)
	}
}

func TestLocalCommandsNotSentToAgent(t *testing.T) {
	_, h := newGitServer(t)
	postGit(t, h, ActionReq{Action: "git_create", Args: map[string]string{"name": "notes"}})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", "/agent/commands", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("agent got %d %s", w.Code, w.Body)
	}
}
