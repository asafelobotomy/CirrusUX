package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHelper writes a script that logs its arguments and prints an empty
// result, standing in for `sudo cirrusux-helper`.
func fakeHelper(t *testing.T) (argv []string, calls func() []string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "helper")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\necho '{\"output\":[\"ok\"]}'\n"), 0o755)
	return []string{script}, func() []string {
		b, _ := os.ReadFile(log)
		return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), " ", "_"))
	}
}

// fakeDocker serves a container list and records POSTs on a unix socket.
func fakeDocker(t *testing.T) (socket string, posts func() []string) {
	socket = filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			got = append(got, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/containers/json" {
			io.WriteString(w, `[{"Id":"0123456789abcdef","Names":["/kavita"],"Image":"kavita:1","State":"running","Status":"Up 1 hour"}]`)
			return
		}
		io.WriteString(w, `{"State":{"StartedAt":"2026-11-01T20:20:00Z"}}`)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return socket, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, got...) }
}

func testAgent(t *testing.T) (*Agent, func() []string, func() []string) {
	helper, calls := fakeHelper(t)
	sock, posts := fakeDocker(t)
	a := New(Config{Token: "t"}, &Collector{Helper: helper, DockerSocket: sock})
	return a, calls, posts
}

func TestExecuteValidates(t *testing.T) {
	a, calls, posts := testAgent(t)
	ctx := context.Background()
	bad := []command{
		{Action: "stay_awake", Args: map[string]string{"minutes": "100000"}},
		{Action: "container_restart", Args: map[string]string{"name": "not-there"}},
		{Action: "shell", Args: map[string]string{"cmd": "rm -rf /"}},
	}
	for _, c := range bad {
		if res := a.run(ctx, c); res.OK {
			t.Errorf("%+v should fail", c)
		}
	}
	if len(calls()) != 0 || len(posts()) != 0 {
		t.Errorf("rejected commands still reached the helper %v or docker %v", calls(), posts())
	}
}

func TestExecuteActions(t *testing.T) {
	a, calls, posts := testAgent(t)
	ctx := context.Background()
	for _, c := range []command{
		{Action: "stay_awake", Args: map[string]string{"minutes": "30"}},
		{Action: "container_restart", Args: map[string]string{"name": "kavita"}},
		{Action: "backup_run"},
	} {
		if res := a.run(ctx, c); !res.OK {
			t.Errorf("%+v failed: %s", c, res.Error)
		}
	}
	want := []string{"stay-awake_30", "nudge-awake", "nudge-awake", "backup-run"}
	if strings.Join(calls(), ",") != strings.Join(want, ",") {
		t.Errorf("helper calls = %v, want %v", calls(), want)
	}
	if p := posts(); len(p) != 1 || p[0] != "/containers/kavita/restart" {
		t.Errorf("docker posts = %v", p)
	}
}

// The full loop against a fake gateway: one command out, one result back.
func TestCommandLoop(t *testing.T) {
	a, _, _ := testAgent(t)
	var mu sync.Mutex
	served := false
	results := make(chan result, 1)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/agent/commands":
			mu.Lock()
			first := !served
			served = true
			mu.Unlock()
			if first {
				io.WriteString(w, `{"id":"c1","action":"allow_sleep"}`)
				return
			}
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		case "/agent/results":
			var res result
			json.NewDecoder(r.Body).Decode(&res)
			results <- res
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer gw.Close()
	a.cfg.GatewayURL = gw.URL + "/agent/snapshot"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.commandLoop(ctx)
	select {
	case res := <-results:
		if res.ID != "c1" || !res.OK {
			t.Errorf("result = %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no result reported")
	}
	select {
	case <-a.pushNow:
	case <-time.After(time.Second):
		t.Error("no immediate snapshot after the action")
	}
}
