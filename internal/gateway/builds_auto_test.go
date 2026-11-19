package gateway

import (
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

// pushRepo is a gateway with a repository called notes, and a clone of it
// to push from.
type pushRepo struct {
	t    *testing.T
	s    *Server
	h    http.Handler
	work string
}

func newPushRepo(t *testing.T) *pushRepo {
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
	return &pushRepo{t: t, s: s, h: h, work: work}
}

// push commits files and pushes them to branch.
func (p *pushRepo) push(branch string, files map[string]string) {
	p.t.Helper()
	runGit(p.t, p.work, "checkout", "-q", "-B", branch)
	for name, body := range files {
		if i := strings.LastIndex(name, "/"); i > 0 {
			os.MkdirAll(p.work+"/"+name[:i], 0o755)
		}
		writeFile(p.t, p.work+"/"+name, body)
	}
	runGit(p.t, p.work, "add", "-A")
	runGit(p.t, p.work, "commit", "-q", "-m", "change on "+branch)
	runGit(p.t, p.work, "push", "-q", "origin", "HEAD:"+branch)
}

func (p *pushRepo) setBuildSettings(onPush, wake string) {
	p.t.Helper()
	body := `{"action":"git_build_settings","args":{"name":"notes","on_push":"` + onPush + `","wake":"` + wake + `"}}`
	w := do(p.h, "POST", "/api/actions", body, actor)
	if w.Code != http.StatusAccepted {
		p.t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	var c struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &c)
	waitFor(p.t, "the settings action", func() bool {
		var got Command
		json.Unmarshal(do(p.h, "GET", "/api/actions/"+c.ID, "", owner).Body.Bytes(), &got)
		return got.State == "done"
	})
}

func (p *pushRepo) builds() []BuildRun {
	var list []BuildRun
	getJSON(p.t, p.h, "/api/git/repos/notes/builds", &list)
	return list
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(25 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (p *pushRepo) wake() {
	snap := awakeSnap()
	snap.Features = []string{helperapi.ModBuilds}
	if err := p.s.store.SaveSnapshot(snap); err != nil {
		p.t.Fatal(err)
	}
}

// settle is long enough for a push's builds to appear, if they will.
func settle() { time.Sleep(400 * time.Millisecond) }

func TestNoBuildsOnPushUnlessTurnedOn(t *testing.T) {
	p := newPushRepo(t)
	p.push("main", map[string]string{build.File: testPipeline})
	settle()
	if n := len(p.builds()); n != 0 {
		t.Fatalf("%d builds without the setting", n)
	}
}

func TestBuildOnPushWaitsForTheServer(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	var set BuildSettings
	if getJSON(t, p.h, "/api/git/repos/notes/build-settings", &set); !set.OnPush || set.Wake {
		t.Fatalf("settings %+v", set)
	}
	p.push("main", map[string]string{build.File: testPipeline})
	waitFor(t, "the build", func() bool { return len(p.builds()) == 1 })
	b := p.builds()[0]
	if !b.Auto || b.State != build.Queued || b.Ref != "main" || b.DispatchedAt != nil {
		t.Fatalf("%+v", b)
	}
	// Asleep: nothing for the agent to do yet.
	if w := do(p.h, "GET", "/agent/builds/"+b.ID, "", agentHdr); w.Code != http.StatusOK {
		t.Fatalf("the agent can't read a waiting build: %d", w.Code)
	}

	// The server wakes and reports: the build goes to it, once.
	p.wake()
	p.s.dispatchBuilds()
	p.s.dispatchBuilds()
	got, _ := p.s.builds.get(b.ID)
	if got.DispatchedAt == nil {
		t.Fatal("not dispatched")
	}
	w := do(p.h, "GET", "/agent/commands", "", agentHdr)
	if !strings.Contains(w.Body.String(), `"build_run"`) || !strings.Contains(w.Body.String(), b.ID) {
		t.Fatalf("agent got %s", w.Body)
	}
	if n := pendingCount(p.s.queue); n != 0 {
		t.Fatalf("%d more commands: the build was sent twice", n)
	}
	if label := buildLabel(&got); !strings.HasSuffix(label, "(main) after a push") {
		t.Fatalf("label %q", label)
	}
}

func TestPushBuildsOnlyBranchesThePipelineNames(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	pipeline := "branches:\n  - main\n  - release/*\n" + testPipeline
	p.push("main", map[string]string{build.File: pipeline})
	p.push("wip", map[string]string{"a.txt": "x"})
	p.push("release/1.0", map[string]string{"b.txt": "y"})
	waitFor(t, "two builds", func() bool { return len(p.builds()) == 2 })
	settle()
	refs := map[string]bool{}
	for _, b := range p.builds() {
		refs[b.Ref] = true
	}
	if len(refs) != 2 || !refs["main"] || !refs["release/1.0"] {
		t.Fatalf("built %v", refs)
	}
}

func TestNewerPushReplacesAWaitingBuild(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	p.push("main", map[string]string{build.File: testPipeline})
	waitFor(t, "the first build", func() bool { return len(p.builds()) == 1 })
	p.push("main", map[string]string{"a.txt": "x"})
	waitFor(t, "the second build", func() bool { return len(p.builds()) == 2 })
	list := p.builds() // newest first
	if list[0].State != build.Queued || list[1].State != build.Cancelled || !strings.Contains(list[1].Error, "newer push") {
		t.Fatalf("%+v / %+v", list[0], list[1])
	}
}

func TestBrokenPipelineOnPushShowsAsAFailedBuild(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	p.push("main", map[string]string{build.File: "steps:\n  - name: x\n    image: alpine\n    privileged: true\n    commands:\n      - id\n"})
	waitFor(t, "the build", func() bool { return len(p.builds()) == 1 })
	b := p.builds()[0]
	if b.State != build.Failed || !strings.Contains(b.Error, "privileged") || b.Finished == nil {
		t.Fatalf("%+v", b)
	}
}

func TestWaitingBuildFailsWhenTheAwakeServerCantBuild(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	p.push("main", map[string]string{build.File: testPipeline})
	waitFor(t, "the build", func() bool { return len(p.builds()) == 1 })
	snap := awakeSnap() // awake, but no builds module
	p.s.store.SaveSnapshot(snap)
	p.s.dispatchBuilds()
	if b := p.builds()[0]; b.State != build.Failed || !strings.Contains(b.Error, "BUILDS=yes") {
		t.Fatalf("%+v", b)
	}
}

func TestWaitingBuildExpiresAfterADay(t *testing.T) {
	r := &BuildRun{State: build.Queued, Auto: true, Created: time.Now().Add(-2 * time.Hour)}
	if expire(r, time.Now()) {
		t.Fatal("a build waiting for a sleeping server expired after 2 hours")
	}
	r.Created = time.Now().Add(-25 * time.Hour)
	if !expire(r, time.Now()) || r.State != build.Failed {
		t.Fatalf("%+v", r)
	}
	// Once the server has been told, it's the usual five minutes.
	told := time.Now().Add(-10 * time.Minute)
	r = &BuildRun{State: build.Queued, Auto: true, Created: time.Now().Add(-2 * time.Hour), DispatchedAt: &told}
	if !expire(r, time.Now()) {
		t.Fatal("a dispatched build the server never started didn't expire")
	}
}

func TestBuildSettingsRulesAndRename(t *testing.T) {
	p := newPushRepo(t)
	for _, body := range []string{
		`{"action":"git_build_settings","args":{"name":"notes","on_push":"maybe","wake":"no"}}`,
		`{"action":"git_build_settings","args":{"name":"notes","on_push":"no","wake":"yes"}}`,
		`{"action":"git_build_settings","args":{"name":"../x","on_push":"yes","wake":"no"}}`,
	} {
		if w := do(p.h, "POST", "/api/actions", body, actor); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	p.setBuildSettings("yes", "yes")
	p.s.buildSettings.rename("notes", "journal")
	if got := p.s.buildSettings.get("journal"); !got.OnPush || !got.Wake {
		t.Fatalf("after a rename %+v", got)
	}
	if got := p.s.buildSettings.get("notes"); got != (BuildSettings{}) {
		t.Fatalf("old name kept %+v", got)
	}
	// They survive a restart.
	again := openBuildSettings(p.s.buildSettings.path)
	if got := again.get("journal"); !got.OnPush {
		t.Fatalf("after a restart %+v", got)
	}
}

func pendingCount(q *Queue) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, c := range q.cmds {
		if c.State == "pending" {
			n++
		}
	}
	return n
}
