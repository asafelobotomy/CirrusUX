package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/agent"
	"cirrusux/internal/appjob"
	"cirrusux/internal/selfupgrade"
	"cirrusux/internal/snapshot"
)

// github stands in for the releases API and counts the requests it gets.
func github(t *testing.T, s *Server, status int, body string) *int {
	t.Helper()
	n := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	s.releaseURL, s.storeClient = ts.URL, ts.Client()
	return &n
}

// managed lays out a release install as install.sh does and points the
// gateway at it.
func managed(t *testing.T, s *Server, current, previous string) {
	t.Helper()
	dir := t.TempDir()
	for _, v := range []string{current, previous} {
		if v != "" {
			if err := os.MkdirAll(filepath.Join(dir, "opt", "releases", v), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = os.Symlink("releases/"+current, filepath.Join(dir, "opt", "current"))
	if previous != "" {
		_ = os.Symlink("releases/"+previous, filepath.Join(dir, "opt", "previous"))
	}
	s.setupLink = filepath.Join(dir, "cirrusux-setup")
	_ = os.Symlink(filepath.Join(dir, "opt", "releases", current, "deploy", "host", "cirrusux-setup"), s.setupLink)
	s.jobsDir = filepath.Join(dir, "jobs")
}

// fakeHelper is a stand-in root helper that records how it was called.
func fakeHelper(t *testing.T, s *Server, exit int) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "cirrusux-helper")
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n"
	if exit == 0 {
		script += `echo '{"output":["cirrusux upgrade: started as cirrusux-job-1"]}'` + "\n"
	} else {
		script += "echo 'sudo: a password is required' >&2\nexit 1\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.selfCmd = []string{bin}
	return calls
}

func TestReleaseCheckKeepsLatest(t *testing.T) {
	s, h := newTestServer(t)
	n := github(t, s, 200, `{"tag_name":"v9.9.9","html_url":"https://evil.example/x","published_at":"2026-11-11T17:59:18Z"}`)
	w := do(h, "POST", "/api/release/check", "", actor)
	var v ReleaseView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || *n != 1 || v.Latest != "v9.9.9" || v.Error != "" || v.Checked.IsZero() {
		t.Fatalf("check: %d, %d request(s), %+v", w.Code, *n, v)
	}
	// The link is built here, never taken from the answer.
	if v.URL != "https://github.com/"+releaseRepo+"/releases/tag/v9.9.9" {
		t.Errorf("url = %q", v.URL)
	}
	// Reading it back asks nobody.
	w = do(h, "GET", "/api/release", "", owner)
	if _ = json.Unmarshal(w.Body.Bytes(), &v); *n != 1 || v.Latest != "v9.9.9" {
		t.Errorf("get: %d request(s), %+v", *n, v)
	}

	// A failed check keeps the release already found and says why.
	github(t, s, 503, "busy")
	_, err := s.checkRelease(context.Background())
	if r := s.store.Release(); err == nil || r.Latest != "v9.9.9" || !strings.Contains(r.Error, "503") {
		t.Errorf("after a failure: err=%v release=%+v", err, r)
	}
}

func TestReleaseCheckRefusesOddTags(t *testing.T) {
	for _, body := range []string{`{"tag_name":"latest"}`, `{"tag_name":"v1.2.3 -y"}`, `{"tag_name":"v1.2.3-rc1"}`, `{}`, `not json`} {
		s, _ := newTestServer(t)
		github(t, s, 200, body)
		if r, err := s.checkRelease(context.Background()); err == nil || r.Latest != "" {
			t.Errorf("%s: err=%v latest=%q", body, err, r.Latest)
		}
	}
}

func TestReleaseDue(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now()
	if !s.releaseDue(now) {
		t.Error("never checked: a check is due")
	}
	_ = s.store.SaveRelease(Release{Checked: now.Add(-2 * time.Hour), Latest: "v1.0.0"})
	if s.releaseDue(now) {
		t.Error("checked 2 h ago: not due")
	}
	_ = s.store.SaveRelease(Release{Checked: now.Add(-4 * time.Hour), Error: "x"})
	if !s.releaseDue(now) {
		t.Error("a failed check is tried again after 3 h")
	}
	_ = s.store.SaveRelease(Release{Checked: now.Add(-25 * time.Hour), Latest: "v1.0.0"})
	if !s.releaseDue(now) {
		t.Error("checked yesterday: due")
	}
}

// The daily check stays off when it is switched off, and in development.
func TestReleaseCheckCanBeSwitchedOff(t *testing.T) {
	for _, set := range []func(*Config){func(c *Config) { c.UpdateCheck = false }, func(c *Config) { c.UpdateCheck, c.Dev = true, true }} {
		s, _ := newTestServer(t)
		set(&s.cfg)
		n := github(t, s, 200, `{"tag_name":"v9.9.9"}`)
		done := make(chan struct{})
		go func() { s.RunReleaseCheck(context.Background()); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("the loop kept running")
		}
		if *n != 0 {
			t.Errorf("%d request(s) went out", *n)
		}
	}
	setEnv(t, map[string]string{"CIRRUS_UPDATE_CHECK": "off"})
	if c, err := ConfigFromEnv(); err != nil || c.UpdateCheck {
		t.Errorf("off: %v %v", c.UpdateCheck, err)
	}
	setEnv(t, map[string]string{"CIRRUS_UPDATE_CHECK": ""})
	if c, err := ConfigFromEnv(); err != nil || !c.UpdateCheck {
		t.Errorf("default: %v %v", c.UpdateCheck, err)
	}
	setEnv(t, map[string]string{"CIRRUS_UPDATE_CHECK": "hourly"})
	if _, err := ConfigFromEnv(); err == nil {
		t.Error("hourly should be refused")
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		tag, have string
		want      bool
	}{
		{"v0.9.0", "v0.8.1", true}, {"v0.9.0", "0.8.1", true}, {"v0.8.1", "v0.8.1", false},
		{"v0.8.0", "v0.8.1", false}, {"v0.10.0", "v0.9.9", true}, {"v0.9.0", "dev", false}, {"v0.9.0", "", false},
	} {
		if got := newer(tc.tag, tc.have); got != tc.want {
			t.Errorf("newer(%q, %q) = %v", tc.tag, tc.have, got)
		}
	}
}

// serverSnap is an awake server on the given release that knows every action.
func serverSnap(current, previous string) snapshot.Snapshot {
	snap := awakeSnap()
	snap.Protocol, snap.Actions = snapshot.Protocol, agent.Actions()
	snap.Install = &selfupgrade.Info{Managed: true, Current: current, Previous: previous}
	return snap
}

func postAct(h http.Handler, body string) *httptest.ResponseRecorder {
	return do(h, "POST", "/api/actions", body, actor)
}

func TestSelfUpgradeOnlyToTheReleaseFound(t *testing.T) {
	s, h := newTestServer(t)
	_ = s.store.SaveSnapshot(serverSnap("v0.8.1", ""))
	up := func(v string) *httptest.ResponseRecorder {
		return postAct(h, `{"action":"self_upgrade","args":{"version":"`+v+`"}}`)
	}
	if w := up("v0.9.0"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "check for a new version first") {
		t.Errorf("before any check: %d %s", w.Code, w.Body)
	}
	_ = s.store.SaveRelease(Release{Checked: time.Now(), Latest: "v0.9.0"})
	if w := up("v0.9.1"); w.Code != http.StatusConflict {
		t.Errorf("a release the check didn't find: %d %s", w.Code, w.Body)
	}
	if w := up("0.9.0; reboot"); w.Code != http.StatusBadRequest {
		t.Errorf("not a release tag: %d %s", w.Code, w.Body)
	}
	if w := postAct(h, `{"action":"self_rollback"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no earlier release") {
		t.Errorf("rollback with nothing before: %d %s", w.Code, w.Body)
	}
	w := up("v0.9.0")
	if w.Code != http.StatusAccepted {
		t.Fatalf("the offered release: %d %s", w.Code, w.Body)
	}
	// It reaches the agent with the version, and its undo is the rollback.
	c := s.queue.Next(context.Background())
	if c == nil || c.Action != "self_upgrade" || c.Args["version"] != "v0.9.0" || c.Undo == nil || c.Undo.Action != "self_rollback" {
		t.Fatalf("queued: %+v", c)
	}
	if c.Label != "Started upgrading CirrusUX on atlas to v0.9.0" {
		t.Errorf("label = %q", c.Label)
	}

	// Already there, or a job already running: refused.
	_ = s.store.SaveSnapshot(serverSnap("v0.9.0", "v0.8.1"))
	if w := up("v0.9.0"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already runs v0.9.0") {
		t.Errorf("already on it: %d %s", w.Code, w.Body)
	}
	busy := serverSnap("v0.8.1", "")
	busy.Jobs = []snapshot.Job{{ID: "aaaa1111", Action: "self-upgrade", State: "running"}}
	_ = s.store.SaveSnapshot(busy)
	if w := up("v0.9.0"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already running") {
		t.Errorf("a job is running: %d %s", w.Code, w.Body)
	}
}

func TestSelfUpgradeNeedsAReleaseInstall(t *testing.T) {
	s, h := newTestServer(t)
	snap := serverSnap("", "")
	snap.Install = &selfupgrade.Info{}
	_ = s.store.SaveSnapshot(snap)
	_ = s.store.SaveRelease(Release{Checked: time.Now(), Latest: "v0.9.0"})
	w := postAct(h, `{"action":"self_upgrade","args":{"version":"v0.9.0"}}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "upgraded by hand") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

// A protocol mismatch switches other actions off, but not the upgrade that
// ends it; an agent from before panel upgrades is told to use the terminal.
func TestSelfUpgradeWorksAcrossAMismatch(t *testing.T) {
	s, h := newTestServer(t)
	s.protocol = snapshot.Protocol + 1 // the agent is older
	_ = s.store.SaveRelease(Release{Checked: time.Now(), Latest: "v0.9.0"})
	_ = s.store.SaveSnapshot(serverSnap("v0.8.1", ""))
	if w := postAct(h, `{"action":"allow_sleep"}`); w.Code != http.StatusConflict {
		t.Fatalf("other actions should be off: %d", w.Code)
	}
	if w := postAct(h, `{"action":"self_upgrade","args":{"version":"v0.9.0"}}`); w.Code != http.StatusAccepted {
		t.Errorf("upgrade across a mismatch: %d %s", w.Code, w.Body)
	}
	old := serverSnap("v0.8.1", "")
	old.Actions = []string{"allow_sleep", "stay_awake"}
	_ = s.store.SaveSnapshot(old)
	w := postAct(h, `{"action":"self_upgrade","args":{"version":"v0.9.0"}}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "too old to be upgraded from the panel") {
		t.Errorf("an agent without the action: %d %s", w.Code, w.Body)
	}
}

func TestGatewayUpgradeRunsTheHelperHere(t *testing.T) {
	s, h := newTestServer(t) // a split setup; the server is asleep
	managed(t, s, "v0.8.1", "")
	calls := fakeHelper(t, s, 0)
	_ = s.store.SaveRelease(Release{Checked: time.Now(), Latest: "v0.9.0"})

	var v ReleaseView
	_ = json.Unmarshal(do(h, "GET", "/api/release", "", owner).Body.Bytes(), &v)
	if !v.Gateway.CanUpgrade || v.Gateway.Current != "v0.8.1" {
		t.Fatalf("view: %+v", v.Gateway)
	}
	if code, c := postGit(t, h, ActionReq{Action: "gateway_rollback"}); code != http.StatusConflict {
		t.Errorf("rollback with nothing before: %d %+v", code, c)
	}
	code, c := postGit(t, h, ActionReq{Action: "gateway_upgrade", Args: map[string]string{"version": "v0.9.0"}})
	if code != http.StatusAccepted || c.State != "done" || c.Undo == nil || c.Undo.Action != "gateway_rollback" {
		t.Fatalf("upgrade: %d %+v", code, c)
	}
	if b, _ := os.ReadFile(calls); strings.TrimSpace(string(b)) != "self-upgrade v0.9.0" {
		t.Errorf("the helper was called with %q", b)
	}
	if a := lastAudit(s); a.What != "Started upgrading CirrusUX on gw to v0.9.0" || a.Undo == "" {
		t.Errorf("audit: %+v", a)
	}

	// A running job there holds the next one back.
	now := time.Now()
	_ = os.MkdirAll(s.jobsDir, 0o755)
	b, _ := json.Marshal(appjob.Status{Job: "aaaa1111", Action: "self-upgrade", State: "running", Started: now})
	_ = os.WriteFile(filepath.Join(s.jobsDir, "aaaa1111.json"), b, 0o644)
	if code, c := postGit(t, h, ActionReq{Action: "gateway_upgrade", Args: map[string]string{"version": "v0.9.0"}}); code != http.StatusConflict || !strings.Contains(c.Error, "already running") {
		t.Errorf("while a job runs: %d %+v", code, c)
	}
}

func TestGatewayUpgradeFailuresAreSaid(t *testing.T) {
	s, h := newTestServer(t)
	managed(t, s, "v0.8.1", "v0.8.0")
	fakeHelper(t, s, 1)
	code, c := postGit(t, h, ActionReq{Action: "gateway_rollback"})
	if code != http.StatusAccepted || c.State != "failed" || !strings.Contains(c.Error, "a password is required") {
		t.Errorf("sudo refused: %d %+v", code, c)
	}
	if a := lastAudit(s); !strings.HasSuffix(a.What, "(failed)") {
		t.Errorf("audit: %+v", a)
	}

	// No helper on the machine, or a single setup: nothing is run.
	s.selfCmd = []string{filepath.Join(t.TempDir(), "missing")}
	if code, c := postGit(t, h, ActionReq{Action: "gateway_rollback"}); code != http.StatusConflict || !strings.Contains(c.Error, "no upgrade helper") {
		t.Errorf("no helper: %d %+v", code, c)
	}
	s.cfg.Setup = "single"
	if code, c := postGit(t, h, ActionReq{Action: "gateway_rollback"}); code != http.StatusConflict || !strings.Contains(c.Error, "upgrades it too") {
		t.Errorf("single setup: %d %+v", code, c)
	}
}
