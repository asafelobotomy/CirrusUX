package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
)

const buildJob = "0123456789abcdef"

func TestStartAndWatchBuild(t *testing.T) {
	p, _ := build.Parse([]byte("steps:\n  - name: t\n    image: alpine\n    commands:\n      - true\n"))
	req := build.Request{Job: buildJob, Repo: "notes", Ref: "main", Commit: strings.Repeat("b", 40), Pipeline: p}
	var mu sync.Mutex
	var gotLog []byte
	var lastState string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/agent/builds/" + buildJob:
			json.NewEncoder(w).Encode(req)
		case "/agent/builds/" + buildJob + "/source":
			io.WriteString(w, "fake tar.gz")
		case "/agent/builds/" + buildJob + "/progress":
			var p progress
			json.NewDecoder(r.Body).Decode(&p)
			mu.Lock()
			defer mu.Unlock()
			if p.From == int64(len(gotLog)) {
				gotLog = append(gotLog, p.Log...)
			}
			lastState = p.Status.State
			json.NewEncoder(w).Encode(progressReply{Size: int64(len(gotLog))})
		default:
			w.WriteHeader(404)
		}
	}))
	defer gw.Close()

	requestsDir, buildsDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { requestsDir, buildsDir = helperapi.RequestsDir, helperapi.BuildsDir })
	a := New(Config{GatewayURL: gw.URL + "/agent/snapshot", Token: "tok"}, &Collector{})

	// The helper finds both files, and the build ends at once.
	helper := func(sub string, args ...string) ([]string, error) {
		if sub != helperapi.CmdBuildJob || args[0] != buildJob {
			t.Fatalf("helper %s %v", sub, args)
		}
		raw, err := os.ReadFile(filepath.Join(requestsDir, buildJob+".json"))
		if r, perr := build.ParseRequest(raw); err != nil || perr != nil || r.Commit != req.Commit {
			t.Fatalf("request %v %v", err, perr)
		}
		if src, _ := os.ReadFile(filepath.Join(requestsDir, buildJob+".tar.gz")); string(src) != "fake tar.gz" {
			t.Fatalf("source %q", src)
		}
		dir := filepath.Join(buildsDir, buildJob)
		os.Mkdir(dir, 0o755)
		st := build.NewStatus(req, time.Now())
		now := time.Now()
		st.State, st.Finished = build.Passed, &now
		fsutil.WriteJSON(filepath.Join(dir, "status.json"), st, 0o644)
		os.WriteFile(filepath.Join(dir, "log"), []byte("all of the log\n"), 0o644)
		return []string{"started"}, nil
	}
	if _, err := a.startBuild(t.Context(), buildJob, helper); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(requestsDir); len(ents) != 0 {
		t.Errorf("left in the requests folder: %v", ents)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := string(gotLog) == "all of the log\n" && lastState == build.Passed
		mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gateway got %q, %s", gotLog, lastState)
}

func TestStartBuildRefusesBadID(t *testing.T) {
	a := New(Config{GatewayURL: "http://127.0.0.1:1/agent/snapshot"}, &Collector{})
	if _, err := a.startBuild(t.Context(), "../x", nil); err == nil {
		t.Fatal("accepted")
	}
}
