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
	"sync/atomic"
	"testing"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
)

// fakeHelper writes a script that logs its arguments and prints an empty
// result, standing in for `sudo cirrusux-helper`.
func fakeHelper(t *testing.T) (argv []string, calls func() []string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "helper")
	// It keeps a copy of an app request, as the real helper reads it then.
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\n"+
		"[ \"$1\" = app-job ] && cp \"$REQS/$2.json\" "+filepath.Join(dir, "request")+"\n"+
		"echo '{\"output\":[\"ok\"]}'\n"), 0o755)
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
		if strings.HasSuffix(r.URL.Path, "/logs") {
			if r.URL.Query().Get("follow") != "" || r.URL.Query().Get("tail") != "50" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, l := range []string{"2026-11-02T12:00:00Z starting\n", "2026-11-02T12:00:01Z ready\n"} {
				hdr := []byte{1, 0, 0, 0, 0, 0, 0, byte(len(l))}
				w.Write(append(hdr, l...))
			}
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
	a := New(Config{Token: "t"}, &Collector{Helper: helper, DockerSocket: sock, features: []string{helperapi.ModStayAwake}})
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

// countingListener counts the connections a server accepts.
type countingListener struct {
	net.Listener
	n atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

// Every collection used to build a new client and leave its connection
// open; one client now serves them all over a reused connection.
func TestDockerClientReused(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	cl := &countingListener{Listener: ln}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			io.WriteString(w, `[{"Id":"0123456789abcdef","Names":["/kavita"],"Image":"kavita:1","State":"running","Status":"Up 1 hour"}]`)
			return
		}
		io.WriteString(w, `{"State":{"StartedAt":"2026-11-01T20:20:00Z"}}`)
	})}
	go srv.Serve(cl)
	defer srv.Close()

	c := &Collector{DockerSocket: socket}
	for range 5 {
		if _, err := c.containers(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := cl.n.Load(); n != 1 {
		t.Errorf("5 collections opened %d connections, want 1", n)
	}
}

// A result that can't be reported at first is retried until it arrives.
func TestResultReportRetried(t *testing.T) {
	saved := resultRetries
	resultRetries = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { resultRetries = saved }()

	var tries atomic.Int32
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tries.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer gw.Close()
	a := New(Config{Token: "t", GatewayURL: gw.URL + "/agent/snapshot"}, &Collector{})
	a.report(context.Background(), result{ID: "c1", OK: true})
	if n := tries.Load(); n != 3 {
		t.Errorf("tries = %d, want 3", n)
	}

	// It gives up after the last retry rather than looping forever.
	tries.Store(-100)
	a.report(context.Background(), result{ID: "c2", OK: true})
	if n := tries.Load(); n != -100+int32(len(resultRetries))+1 {
		t.Errorf("tries when the gateway never answers = %d", n+100)
	}
}

// Without a sleep module there is nothing to keep awake.
func TestNoNudgeWithoutSleep(t *testing.T) {
	a, calls, _ := testAgent(t)
	a.collector.features = nil
	if res := a.run(context.Background(), command{Action: "container_restart", Args: map[string]string{"name": "kavita"}}); !res.OK {
		t.Fatal(res.Error)
	}
	if len(calls()) != 0 {
		t.Errorf("helper calls = %v", calls())
	}
}

// An app request goes to the helper as a file, written back from its parsed
// form, and the helper is then asked to take it.
func TestAppRequest(t *testing.T) {
	a, calls, _ := testAgent(t)
	requestsDir = t.TempDir()
	t.Setenv("REQS", requestsDir)
	t.Cleanup(func() { requestsDir = helperapi.RequestsDir })
	c := command{ID: "0123456789abcdef", Action: "app_install", Args: map[string]string{
		"app": "kavita", "options": `{"media":{"books":"/srv/media/books"}}`}}
	if res := a.run(context.Background(), c); !res.OK {
		t.Fatalf("install: %s", res.Error)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(a.collector.Helper[0]), "request"))
	if err != nil || !strings.Contains(string(b), `"books":"/srv/media/books"`) {
		t.Errorf("request file = %s, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(requestsDir, "0123456789abcdef.json")); err == nil {
		t.Error("request left behind for a later job")
	}
	if got := calls(); len(got) != 1 || got[0] != "app-job_0123456789abcdef" {
		t.Errorf("helper calls = %v", got)
	}
	for _, opts := range []string{`{"privileged":true}`, `{}} {"job":"x"`, `{}, "action":"remove"`} {
		bad := command{ID: "0123456789abcdee", Action: "app_install", Args: map[string]string{"app": "kavita", "options": opts}}
		if res := a.run(context.Background(), bad); res.OK {
			t.Errorf("options %s accepted", opts)
		}
	}

	// A CirrusStore app's source and signed definition go on unchanged.
	signed := `{"data":"eyJzb3VyY2UiOiJleHRyYSJ9","sig":"c2ln"}`
	c = command{ID: "0123456789abcdea", Action: "app_install", Args: map[string]string{"app": "music-box", "store": "extra", "signed": signed}}
	if res := a.run(context.Background(), c); !res.OK {
		t.Fatalf("signed install: %s", res.Error)
	}
	b, _ = os.ReadFile(filepath.Join(filepath.Dir(a.collector.Helper[0]), "request"))
	if !strings.Contains(string(b), `"source":"extra","definition":`+signed) {
		t.Errorf("request file = %s", b)
	}
	bad := command{ID: "0123456789abcdeb", Action: "app_install", Args: map[string]string{"app": "music-box", "store": "extra", "signed": "not json"}}
	if res := a.run(context.Background(), bad); res.OK {
		t.Error("a damaged definition was passed on")
	}
}

func TestLogs(t *testing.T) {
	a, calls, _ := testAgent(t)
	got, err := a.logs(context.Background(), "container:kavita", "50")
	if err != nil || strings.Join(got, "|") != "2026-11-02T12:00:00Z starting|2026-11-02T12:00:01Z ready" {
		t.Errorf("container logs = %q, %v", got, err)
	}
	if _, err := a.logs(context.Background(), "container:not-there", "50"); err == nil {
		t.Error("logs of an unknown container")
	}
	if _, err := a.logs(context.Background(), "unit:sshd.service", "50"); err == nil {
		t.Error("journal of a unit that isn't CirrusUX's")
	}
	if _, err := a.logs(context.Background(), "unit:cirrusux-backup.service", "50"); err != nil {
		t.Errorf("unit logs: %v", err)
	}
	if c := calls(); len(c) != 1 || c[0] != "journal_cirrusux-backup.service" {
		t.Errorf("helper calls = %v", c)
	}
	if string(demux([]byte("plain tty output\n"))) != "plain tty output\n" {
		t.Error("a TTY container's plain output was mangled")
	}
}

// Every action the agent lists is one execute() handles, so the list in each
// snapshot can't promise something the switch doesn't do.
func TestListedActionsAreHandled(t *testing.T) {
	a, _, _ := testAgent(t)
	for _, name := range Actions() {
		_, err := a.execute(context.Background(), command{ID: "t1", Action: name, Args: map[string]string{}})
		if err != nil && strings.Contains(err.Error(), "unknown action") {
			t.Errorf("%s is listed but execute() doesn't handle it", name)
		}
	}
	if _, err := a.execute(context.Background(), command{Action: "shell"}); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("an unlisted action should be unknown, got %v", err)
	}
}

func TestSnapshotCarriesProtocolAndActions(t *testing.T) {
	a, _, _ := testAgent(t)
	s, err := a.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Protocol != snapshot.Protocol || len(s.Actions) != len(Actions()) {
		t.Errorf("protocol %d, actions %v", s.Protocol, s.Actions)
	}
}
