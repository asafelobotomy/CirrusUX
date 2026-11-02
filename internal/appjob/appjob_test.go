package appjob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/catalogue"
)

type fakeRuntime struct {
	calls []string
	pull  error
}

func (f *fakeRuntime) Name() string { return "fake" }
func (f *fakeRuntime) Pull(_ context.Context, ref string, progress func(string)) error {
	f.calls = append(f.calls, "pull "+ref)
	progress("50% downloaded")
	return f.pull
}
func (f *fakeRuntime) Up(_ context.Context, p catalogue.Plan, _ string) error {
	f.calls = append(f.calls, "up "+p.Name)
	return nil
}
func (f *fakeRuntime) Down(_ context.Context, id, _ string) error {
	f.calls = append(f.calls, "down "+id)
	return nil
}

func testEnv(t *testing.T) (*Env, *fakeRuntime, string) {
	root := t.TempDir()
	media := filepath.Join(root, "media")
	os.MkdirAll(filepath.Join(media, "music"), 0o755)
	cat, err := catalogue.Load()
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{}
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	return &Env{
		Runtime: rt, Catalogue: cat, Now: func() time.Time { return now },
		Host:       catalogue.Host{AppsDir: filepath.Join(root, "apps"), TZ: "UTC", UID: 990, GID: 990, MediaRoots: []string{media}},
		ArchiveDir: filepath.Join(root, "archive"), RecordsDir: filepath.Join(root, "records"), StatusDir: filepath.Join(root, "jobs"),
		PortFree:      func(int, string) bool { return true },
		Healthy:       func(context.Context, string) error { return nil },
		HealthTimeout: 3 * time.Second,
	}, rt, media
}

func navidrome(media string) Request {
	return Request{Job: "aaaa1111", Action: "install", App: "navidrome", Options: catalogue.Options{Media: map[string]string{"music": filepath.Join(media, "music")}}}
}

func status(t *testing.T, e *Env, job string) Status {
	for _, s := range Statuses(e.StatusDir) {
		if s.Job == job {
			return s
		}
	}
	t.Fatalf("no status for %s", job)
	return Status{}
}

func TestInstallAndRemove(t *testing.T) {
	e, rt, media := testEnv(t)
	if err := e.Run(context.Background(), navidrome(media)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(rt.calls, ",") != "pull docker.io/deluan/navidrome:0.64.2@sha256:38dc2727bfcfd5ede290f8ada114fc90368146f265ae4701ddddbcbe2a44ee52,up navidrome" {
		t.Errorf("calls = %v", rt.calls)
	}
	if fi, err := os.Stat(filepath.Join(e.Host.AppsDir, "navidrome", "data")); err != nil || !fi.IsDir() {
		t.Error("data folder not created")
	}
	if _, err := os.Stat(filepath.Join(media, "music", "navidrome")); err == nil {
		t.Error("touched the media folder")
	}
	recs := Records(e.RecordsDir)
	if len(recs) != 1 || recs[0].MainPort != 4533 || recs[0].Runtime != "fake" {
		t.Errorf("records = %+v", recs)
	}
	if s := status(t, e, "aaaa1111"); s.State != "done" || s.Step != "Navidrome is installed" || s.Finished == nil {
		t.Errorf("status = %+v", s)
	}

	// A second install is refused.
	again := navidrome(media)
	again.Job = "aaaa2222"
	if err := e.Run(context.Background(), again); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Errorf("second install: %v", err)
	}

	rm := Request{Job: "bbbb1111", Action: "remove", App: "navidrome"}
	if err := e.Run(context.Background(), rm); err != nil {
		t.Fatal(err)
	}
	if len(Records(e.RecordsDir)) != 0 {
		t.Error("record not removed")
	}
	archived, _ := filepath.Glob(filepath.Join(e.ArchiveDir, "navidrome-*", "data"))
	rec, _ := filepath.Glob(filepath.Join(e.ArchiveDir, "navidrome-*", "cirrusux-record.json"))
	if len(archived) != 1 || len(rec) != 1 {
		t.Error("folder and record not archived")
	}
	if _, err := os.Stat(filepath.Join(e.Host.AppsDir, "navidrome")); err == nil {
		t.Error("app folder still in place")
	}
	if s := status(t, e, "bbbb1111"); s.State != "done" || !strings.Contains(s.Detail, "archived to") {
		t.Errorf("remove status = %+v", s)
	}
	if err := e.Run(context.Background(), Request{Job: "bbbb2222", Action: "remove", App: "navidrome"}); err == nil {
		t.Error("removing an app that isn't installed")
	}
}

func TestInstallFailures(t *testing.T) {
	e, rt, media := testEnv(t)

	e.PortFree = func(port int, _ string) bool { return port != 4533 }
	if err := e.Run(context.Background(), navidrome(media)); err == nil || !strings.Contains(err.Error(), "4533/tcp is already in use") {
		t.Errorf("busy port: %v", err)
	}
	if s := status(t, e, "aaaa1111"); s.State != "failed" {
		t.Errorf("status = %+v", s)
	}
	if len(rt.calls) != 0 {
		t.Error("a refused install still ran")
	}
	e.PortFree = func(int, string) bool { return true }

	r := navidrome(media)
	r.Options.Media["music"] = filepath.Join(media, "missing")
	if err := e.Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("missing media folder: %v", err)
	}

	// Started but never healthy: stopped again, nothing recorded.
	e.Healthy = func(context.Context, string) error { return errors.New("connection refused") }
	e.HealthTimeout = 100 * time.Millisecond
	r = navidrome(media)
	r.Job = "cccc1111"
	err := e.Run(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "didn't answer") {
		t.Fatalf("unhealthy: %v", err)
	}
	if rt.calls[len(rt.calls)-1] != "down navidrome" || len(Records(e.RecordsDir)) != 0 {
		t.Errorf("unhealthy app left running or recorded: %v", rt.calls)
	}

	rt.pull = errors.New("no space left")
	r.Job = "cccc2222"
	if err := e.Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "download") {
		t.Errorf("failed pull: %v", err)
	}
}

func TestParseRequest(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"job":"abcd1234","action":"install","app":"kavita","options":{"media":{"books":"/srv/media/books"}}}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"job":"abcd1234","action":"install","app":"kavita","options":{},"privileged":true}`,
		`{"job":"../../etc","action":"install","app":"kavita"}`,
		`{"job":"abcd1234","action":"exec","app":"kavita"}`,
		`{"job":"abcd1234","action":"install","app":"kavita","options":{"volumes":["/:/host"]}}`,
	} {
		if _, err := ParseRequest([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	e, _, _ := testEnv(t)
	if _, err := e.Check(Request{Job: "abcd1234", Action: "install", App: "not-an-app"}); err == nil {
		t.Error("unknown app accepted")
	}
}

func TestRuntimes(t *testing.T) {
	var ran []string
	fake := func(_ context.Context, _ func(string), name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	cat, _ := catalogue.Load()
	var kuma catalogue.App
	for _, a := range cat {
		if a.ID == "uptime-kuma" {
			kuma = a
		}
	}
	plan, err := catalogue.Resolve(kuma, catalogue.Options{}, catalogue.Host{AppsDir: "/srv/cirrusux/apps"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	p := Podman{Exec: fake, UnitDir: filepath.Join(dir, "systemd")}
	if err := p.Up(context.Background(), plan, filepath.Join(dir, "app")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "systemd", "cirrusux-uptime-kuma.container")); err != nil || !strings.Contains(string(b), "ContainerName=uptime-kuma") {
		t.Errorf("unit not written: %v", err)
	}
	if err := p.Down(context.Background(), "uptime-kuma", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "systemd", "cirrusux-uptime-kuma.container")); err == nil {
		t.Error("unit left behind")
	}
	want := "systemctl daemon-reload|systemctl start cirrusux-uptime-kuma.service|systemctl stop cirrusux-uptime-kuma.service|systemctl daemon-reload"
	if strings.Join(ran, "|") != want {
		t.Errorf("podman ran %v", ran)
	}

	ran = nil
	d := Docker{Exec: fake}
	appDir := filepath.Join(dir, "kuma")
	if err := d.Up(context.Background(), plan, appDir); err != nil {
		t.Fatal(err)
	}
	if err := d.Down(context.Background(), "uptime-kuma", appDir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(appDir, "compose.yml")
	if len(ran) != 2 || ran[0] != "docker compose -p uptime-kuma -f "+file+" up -d" || !strings.HasSuffix(ran[1], " down") {
		t.Errorf("docker ran %v", ran)
	}
}
