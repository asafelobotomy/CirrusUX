package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cirrusux/internal/build"
)

const deployPipeline = "cache: v1\nsteps:\n  - name: deploy\n    image: alpine:3\n    secrets:\n      - DEPLOY_TOKEN\n    commands:\n      - ./deploy.sh\n"

const tokenValue = "tok-abcdef123456"

func putSecret(h http.Handler, key, value string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"value": value})
	return do(h, "PUT", "/api/git/repos/notes/secrets/"+key, string(b), actor)
}

func TestSecretsAreWriteOnly(t *testing.T) {
	s, h, _ := buildServer(t, deployPipeline)
	if w := putSecret(h, "DEPLOY_TOKEN", tokenValue); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	var list []SecretInfo
	getJSON(t, h, "/api/git/repos/notes/secrets", &list)
	if len(list) != 1 || list[0].Name != "DEPLOY_TOKEN" || list[0].Updated.IsZero() {
		t.Fatalf("list %+v", list)
	}
	if raw := do(h, "GET", "/api/git/repos/notes/secrets", "", owner).Body.String(); strings.Contains(raw, tokenValue) {
		t.Fatal("the list shows the value")
	}
	// Kept on disk for the gateway only, and not in the audit log.
	fi, err := os.Stat(filepath.Join(s.cfg.DataDir, "build-secrets.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %v %v", fi, err)
	}
	putSecret(h, "DEPLOY_TOKEN", "a-new-value-1234")
	audit := s.store.RecentAudit(2)
	if len(audit) != 2 || audit[0].What != "Replaced secret DEPLOY_TOKEN for builds of notes" || audit[1].What != "Set secret DEPLOY_TOKEN for builds of notes" {
		t.Fatalf("audit %+v", audit)
	}
	for _, e := range audit {
		if raw, _ := json.Marshal(e); strings.Contains(string(raw), tokenValue) || strings.Contains(string(raw), "a-new-value") {
			t.Fatalf("the audit log holds a value: %s", raw)
		}
	}
	if w := do(h, "DELETE", "/api/git/repos/notes/secrets/DEPLOY_TOKEN", "", actor); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := do(h, "DELETE", "/api/git/repos/notes/secrets/DEPLOY_TOKEN", "", actor); w.Code != http.StatusNotFound {
		t.Fatalf("delete twice: %d", w.Code)
	}
}

func TestSecretRules(t *testing.T) {
	_, h, _ := buildServer(t, deployPipeline)
	for key, value := range map[string]string{"lower": tokenValue, "PATH": tokenValue, "CIRRUS_X": tokenValue, "DEPLOY_TOKEN": "short", "OK_NAME": "has\x00nul-1234"} {
		if w := putSecret(h, key, value); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", key, w.Code)
		}
	}
	if w := do(h, "PUT", "/api/git/repos/missing/secrets/A_B", `{"value":"abcdefghij"}`, actor); w.Code != http.StatusNotFound {
		t.Errorf("unknown repository: %d", w.Code)
	}
	if w := do(h, "PUT", "/api/git/repos/notes/secrets/A_B", `{"value":"abcdefghij"}`, owner); w.Code != http.StatusForbidden {
		t.Errorf("without the request header: %d", w.Code)
	}
}

func TestBuildWithSecretsAndCache(t *testing.T) {
	_, h, _ := buildServer(t, deployPipeline)
	if w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor); w.Code != 400 || !strings.Contains(w.Body.String(), "DEPLOY_TOKEN isn't set") {
		t.Fatalf("without the secret: %d %s", w.Code, w.Body)
	}
	putSecret(h, "DEPLOY_TOKEN", tokenValue)
	w := do(h, "POST", "/api/git/repos/notes/builds", `{"ref":"main"}`, actor)
	if w.Code != http.StatusAccepted || strings.Contains(w.Body.String(), tokenValue) {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var run BuildRun
	json.Unmarshal(w.Body.Bytes(), &run)

	// The server gets the value, and the cache key for a build by hand.
	w = do(h, "GET", "/agent/builds/"+run.ID, "", agentHdr)
	req, err := build.ParseRequest(w.Body.Bytes())
	if err != nil || req.Secrets["DEPLOY_TOKEN"] != tokenValue || req.CacheKey != "manual-v1" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("request %+v %v %v", req, err, w.Header())
	}
	// Nothing the panel can read has it.
	for _, path := range []string{"/api/git/builds/" + run.ID, "/api/git/repos/notes/builds", "/api/changelog", "/api/actions"} {
		if body := do(h, "GET", path, "", owner).Body.String(); strings.Contains(body, tokenValue) {
			t.Errorf("%s shows the secret", path)
		}
	}
	// Changed since: the server gets the new value, not the one from the start.
	putSecret(h, "DEPLOY_TOKEN", "rotated-value-99")
	req, _ = build.ParseRequest(do(h, "GET", "/agent/builds/"+run.ID, "", agentHdr).Body.Bytes())
	if req.Secrets["DEPLOY_TOKEN"] != "rotated-value-99" {
		t.Fatalf("%+v", req.Secrets)
	}
	// Deleted before the server asked: the build fails with the reason.
	do(h, "DELETE", "/api/git/repos/notes/secrets/DEPLOY_TOKEN", "", actor)
	if w := do(h, "GET", "/agent/builds/"+run.ID, "", agentHdr); w.Code != http.StatusGone || strings.Contains(w.Body.String(), "rotated") {
		t.Fatalf("after the secret was deleted: %d %s", w.Code, w.Body)
	}
	var got BuildRun
	getJSON(t, h, "/api/git/builds/"+run.ID, &got)
	if got.State != build.Failed || !strings.Contains(got.Error, "DEPLOY_TOKEN isn't set") {
		t.Fatalf("%+v", got)
	}
}

func TestPushBuildsNeverGetSecrets(t *testing.T) {
	p := newPushRepo(t)
	p.setBuildSettings("yes", "no")
	// A pipeline with a cache but no secrets: it builds, with its own cache.
	p.push("main", map[string]string{build.File: "cache: true\n" + testPipeline})
	waitFor(t, "the build", func() bool { return len(p.builds()) == 1 })
	b := p.builds()[0]
	req, err := build.ParseRequest(do(p.h, "GET", "/agent/builds/"+b.ID, "", agentHdr).Body.Bytes())
	if err != nil || req.CacheKey != "push-default" || len(req.Secrets) != 0 {
		t.Fatalf("%+v %v", req, err)
	}
	// One that wants a secret: the push shows why it didn't run.
	b2 := "tok-" + strings.Repeat("z", 12)
	do(p.h, "PUT", "/api/git/repos/notes/secrets/DEPLOY_TOKEN", `{"value":"`+b2+`"}`, actor)
	p.push("deploy", map[string]string{build.File: deployPipeline})
	waitFor(t, "the second build", func() bool { return len(p.builds()) == 2 })
	for _, got := range p.builds() {
		if got.Ref != "deploy" {
			continue
		}
		if got.State != build.Failed || !strings.Contains(got.Error, "never gets") || strings.Contains(got.Error, b2) {
			t.Fatalf("%+v", got)
		}
		return
	}
	t.Fatal("no build for the deploy branch")
}

func TestSecretsFollowRenameAndPurge(t *testing.T) {
	st := openSecrets(filepath.Join(t.TempDir(), "s.json"))
	st.set("notes", "A_B", "abcdefghij")
	st.rename("notes", "journal")
	if v, missing := st.values("journal", []string{"A_B"}); len(missing) != 0 || v["A_B"] != "abcdefghij" {
		t.Fatalf("%v %v", v, missing)
	}
	if _, missing := st.values("notes", []string{"A_B"}); len(missing) != 1 {
		t.Fatal("the old name kept it")
	}
	again := openSecrets(st.path)
	if v, _ := again.values("journal", []string{"A_B"}); v["A_B"] != "abcdefghij" {
		t.Fatal("lost on restart")
	}
	again.drop("journal")
	if len(again.list("journal")) != 0 {
		t.Fatal("not dropped")
	}
}
