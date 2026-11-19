package gateway

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/helperapi"
)

const testPipeline = "steps:\n  - name: test\n    image: golang:1.25\n    commands:\n      - go test ./...\n"

// buildServer is a gateway with Git hosting, a repository holding a
// pipeline, and an awake server that has the builds module.
func buildServer(t *testing.T, pipeline string) (*Server, http.Handler, string) {
	t.Helper()
	s, h := newGitServer(t, devMode)
	if err := s.git.Create(t.Context(), "notes", ""); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	work := t.TempDir()
	runGit(t, work, "-c", "init.defaultBranch=main", "clone", "-q", srv.URL+"/git/notes.git", ".")
	runGit(t, work, "checkout", "-q", "-b", "main")
	writeFile(t, work+"/main.go", "package main")
	if pipeline != "" {
		os.Mkdir(work+"/.cirrus", 0o755)
		writeFile(t, work+"/.cirrus/build.yml", pipeline)
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-q", "-m", "First")
	runGit(t, work, "push", "-q", "origin", "HEAD:main")
	snap := awakeSnap()
	snap.Features = []string{helperapi.ModBuilds}
	if err := s.store.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	return s, h, runGit(t, work, "rev-parse", "HEAD")
}

var agentHdr = map[string]string{"Authorization": "Bearer " + token}

func TestBuildRoundTrip(t *testing.T) {
	s, h, sha := buildServer(t, testPipeline)
	w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor)
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var run BuildRun
	json.Unmarshal(w.Body.Bytes(), &run)
	if run.State != build.Queued || run.Commit != sha || run.Ref != "main" || len(run.Steps) != 1 || run.Pipeline != nil {
		t.Fatalf("run %+v", run)
	}

	// The agent is told to build it, and fetches what to build.
	w = do(h, "GET", "/agent/commands", "", agentHdr)
	var cmd struct {
		ID, Action string
		Args       map[string]string
	}
	json.Unmarshal(w.Body.Bytes(), &cmd)
	if cmd.Action != "build_run" || cmd.Args["run"] != run.ID {
		t.Fatalf("command %+v", cmd)
	}
	w = do(h, "GET", "/agent/builds/"+run.ID, "", agentHdr)
	req, err := build.ParseRequest(w.Body.Bytes())
	if err != nil || req.Commit != sha || req.Pipeline.Steps[0].Image != "golang:1.25" {
		t.Fatalf("request %+v %v (%s)", req, err, w.Body)
	}
	w = do(h, "GET", "/agent/builds/"+run.ID+"/source", "", agentHdr)
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	tr := tar.NewReader(zr)
	for hd, err := tr.Next(); err == nil; hd, err = tr.Next() {
		names = append(names, hd.Name)
	}
	if !strings.Contains(strings.Join(names, " "), "main.go") {
		t.Fatalf("source has %v", names)
	}
	if w := do(h, "GET", "/agent/builds/"+run.ID, "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without the agent's token: %d", w.Code)
	}

	// It reports progress: status and log, in order.
	st := build.Status{Job: run.ID, State: build.Running, Started: time.Now(),
		Steps: []build.StepStatus{{Name: "test", Image: "golang:1.25", State: build.Running}}}
	post := func(st build.Status, from int64, log string) map[string]any {
		b, _ := json.Marshal(map[string]any{"status": st, "from": from, "log": base64.StdEncoding.EncodeToString([]byte(log))})
		w := do(h, "POST", "/agent/builds/"+run.ID+"/progress", string(b), agentHdr)
		var r map[string]any
		json.Unmarshal(w.Body.Bytes(), &r)
		if w.Code != 200 {
			t.Fatalf("progress: %d %s", w.Code, w.Body)
		}
		return r
	}
	if r := post(st, -1, ""); r["size"] != 0.0 {
		t.Fatalf("first progress %v", r)
	}
	post(st, 0, "hello ")
	if r := post(st, 0, "hello "); r["size"] != 6.0 {
		t.Fatalf("a repeated chunk was added again: %v", r)
	}
	now := time.Now()
	zero := 0
	st.State, st.Finished = build.Passed, &now
	st.Steps[0].State, st.Steps[0].Exit = build.Passed, &zero
	post(st, 6, "world")

	var got BuildRun
	getJSON(t, h, "/api/git/builds/"+run.ID, &got)
	if got.State != build.Passed || got.LogSize != 11 || got.Steps[0].State != build.Passed {
		t.Fatalf("after progress %+v", got)
	}
	var log struct {
		Text string
		Next int64
	}
	getJSON(t, h, "/api/git/builds/"+run.ID+"/log?from=0", &log)
	if log.Text != "hello world" || log.Next != 11 {
		t.Fatalf("log %+v", log)
	}
	var list []BuildRun
	if getJSON(t, h, "/api/git/repos/notes/builds", &list); len(list) != 1 || list[0].ID != run.ID {
		t.Fatalf("list %+v", list)
	}
	if w := do(h, "POST", "/api/git/builds/"+run.ID+"/cancel", "", actor); w.Code != http.StatusConflict {
		t.Errorf("cancel a finished build: %d", w.Code)
	}

	// The start is in the Changelog once the agent reports it.
	do(h, "POST", "/agent/results", `{"id":"`+cmd.ID+`","ok":true,"output":["started"]}`, agentHdr)
	if a := s.store.RecentAudit(5); len(a) == 0 || !strings.HasPrefix(a[0].What, "Started a build of notes at "+sha[:8]) {
		t.Errorf("audit %+v", a)
	}
}

func TestBuildRefusals(t *testing.T) {
	_, h, _ := buildServer(t, "")
	if w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor); w.Code != 400 || !strings.Contains(w.Body.String(), ".cirrus/build.yml") {
		t.Errorf("no pipeline: %d %s", w.Code, w.Body)
	}
	s, h, _ := buildServer(t, "steps:\n  - name: x\n    image: alpine\n    privileged: true\n    commands:\n      - id\n")
	if w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor); w.Code != 400 || !strings.Contains(w.Body.String(), "privileged") {
		t.Errorf("bad pipeline: %d %s", w.Code, w.Body)
	}
	snap := awakeSnap()
	s.store.SaveSnapshot(snap)
	if w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor); w.Code != 409 || !strings.Contains(w.Body.String(), "BUILDS=yes") {
		t.Errorf("no builds module: %d %s", w.Code, w.Body)
	}
}

func TestBuildCancelQueued(t *testing.T) {
	s, h, _ := buildServer(t, testPipeline)
	w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor)
	var run BuildRun
	json.Unmarshal(w.Body.Bytes(), &run)
	if w := do(h, "POST", "/api/git/builds/"+run.ID+"/cancel", "", actor); w.Code != 200 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/agent/builds/"+run.ID, "", agentHdr); w.Code != http.StatusGone {
		t.Errorf("a cancelled build was handed out: %d", w.Code)
	}
	// The server started it anyway: it is told to stop, once.
	b, _ := json.Marshal(map[string]any{"status": build.Status{Job: run.ID, State: build.Running, Started: time.Now()}, "from": -1})
	w = do(h, "POST", "/agent/builds/"+run.ID+"/progress", string(b), agentHdr)
	if !strings.Contains(w.Body.String(), `"stop":true`) {
		t.Errorf("progress on a cancelled build: %s", w.Body)
	}
	do(h, "GET", "/agent/commands", "", agentHdr) // build_run
	w = do(h, "GET", "/agent/commands", "", agentHdr)
	if !strings.Contains(w.Body.String(), "build_cancel") {
		t.Errorf("no cancel sent: %s", w.Body)
	}
	if got, _ := s.builds.get(run.ID); got.State != build.Cancelled {
		t.Errorf("state %s", got.State)
	}
}

func TestBuildExpires(t *testing.T) {
	r := &BuildRun{State: build.Queued, Created: time.Now().Add(-10 * time.Minute), Steps: []build.StepStatus{{State: build.Waiting}}}
	if !expire(r, time.Now()) || r.State != build.Failed || r.Steps[0].State != build.Skipped {
		t.Fatalf("%+v", r)
	}
	r = &BuildRun{State: build.Running, Updated: time.Now()}
	if expire(r, time.Now()) {
		t.Fatal("a build reporting now expired")
	}
}

func TestBuildsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	b := openBuilds(dir)
	b.add(&BuildRun{ID: "0123456789abcdef", Repo: "notes", State: build.Passed, Created: time.Now()})
	if _, err := b.appendLog("0123456789abcdef", 0, []byte("log")); err != nil {
		t.Fatal(err)
	}
	again := openBuilds(dir)
	if r, ok := again.get("0123456789abcdef"); !ok || r.LogSize != 3 {
		t.Fatalf("%+v %v", r, ok)
	}
	for i := 0; i < keepBuilds+5; i++ {
		again.add(&BuildRun{ID: newBuildID(), Repo: "notes", State: build.Passed, Created: time.Now().Add(time.Duration(i) * time.Second)})
	}
	if n := len(again.list("notes")); n != keepBuilds {
		t.Fatalf("kept %d", n)
	}
}
