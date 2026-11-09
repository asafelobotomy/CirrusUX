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
	f.calls = append(f.calls, "up "+p.Name+" "+p.App.Tag)
	return nil
}
func (f *fakeRuntime) Down(_ context.Context, id, _ string) error {
	f.calls = append(f.calls, "down "+id)
	return nil
}
func (f *fakeRuntime) RemoveImage(_ context.Context, ref string) error {
	f.calls = append(f.calls, "rmi "+ref)
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
	if strings.Join(rt.calls, ",") != "pull docker.io/deluan/navidrome:0.64.2@sha256:38dc2727bfcfd5ede290f8ada114fc90368146f265ae4701ddddbcbe2a44ee52,up navidrome 0.64.2" {
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
	want := "systemctl daemon-reload|systemctl restart cirrusux-uptime-kuma.service|systemctl stop cirrusux-uptime-kuma.service|systemctl daemon-reload"
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

const newDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// Done-when for Phase 6: an update backs up, moves, checks and records; a
// failed one puts the old version and its settings back by itself.
func TestUpdateAndRollback(t *testing.T) {
	e, rt, media := testEnv(t)
	ctx := context.Background()
	if err := e.Run(ctx, navidrome(media)); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(e.Host.AppsDir, "navidrome", "data")
	os.WriteFile(filepath.Join(data, "navidrome.db"), []byte("v1 library"), 0o644)

	up := Request{Job: "dddd1111", Action: "update", App: "navidrome", Tag: "0.65.0", Digest: newDigest}
	rt.calls = nil
	if err := e.Run(ctx, up); err != nil {
		t.Fatalf("update: %v", err)
	}
	if strings.Join(rt.calls, ",") != "pull docker.io/deluan/navidrome:0.65.0@"+newDigest+",up navidrome 0.65.0" {
		t.Errorf("calls = %v", rt.calls)
	}
	if s := status(t, e, "dddd1111"); s.Step != "Navidrome is updated" || !strings.Contains(s.Detail, "backup in") {
		t.Errorf("update status = %+v", s)
	}
	rec := Records(e.RecordsDir)[0]
	if rec.Tag != "0.65.0" || rec.Digest != newDigest || rec.PreviousTag != "0.64.2" || !strings.HasPrefix(rec.PreviousDigest, "sha256:38dc") || rec.Options.Media["music"] == "" {
		t.Errorf("record = %+v", rec)
	}
	backups, _ := filepath.Glob(filepath.Join(e.ArchiveDir, "navidrome-before-0.64.2-*", "data", "navidrome.db"))
	if len(backups) != 1 {
		t.Fatal("settings not backed up before the update")
	}
	if err := e.Run(ctx, Request{Job: "dddd2222", Action: "update", App: "navidrome", Tag: "0.65.0", Digest: newDigest}); err == nil {
		t.Error("updating to the version it already runs")
	}

	// The next version breaks its data and never answers: everything goes back.
	e.Healthy = func(context.Context, string) error { return errors.New("502") }
	e.HealthTimeout = 50 * time.Millisecond
	e.Runtime = &breakingRuntime{fakeRuntime: rt, data: data}
	rt.calls = nil
	err := e.Run(ctx, Request{Job: "eeee1111", Action: "update", App: "navidrome", Tag: "0.66.0", Digest: newDigest[:70] + "2"})
	if err == nil || !strings.Contains(err.Error(), "is back on 0.65.0") {
		t.Fatalf("failed update: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "navidrome.db")); string(b) != "v1 library" {
		t.Errorf("data not restored: %q", b)
	}
	if n := len(rt.calls); rt.calls[n-2] != "up navidrome 0.65.0" || rt.calls[n-1] != "rmi docker.io/deluan/navidrome@"+newDigest[:70]+"2" {
		t.Errorf("old version not restarted, or the broken one's image kept: %v", rt.calls)
	}
	if rec := Records(e.RecordsDir)[0]; rec.Tag != "0.65.0" {
		t.Errorf("record changed by a failed update: %+v", rec)
	}
	if s := status(t, e, "eeee1111"); s.State != "failed" {
		t.Errorf("status = %+v", s)
	}

	// Rolling back is an update to the previous version.
	e.Runtime, e.Healthy = rt, func(context.Context, string) error { return nil }
	back := Request{Job: "ffff1111", Action: "update", App: "navidrome", Tag: rec.PreviousTag, Digest: rec.PreviousDigest}
	if err := e.Run(ctx, back); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if rec := Records(e.RecordsDir)[0]; rec.Tag != "0.64.2" || rec.PreviousTag != "0.65.0" {
		t.Errorf("after rollback: %+v", rec)
	}

	// Removing it takes both images it kept, the current and the previous.
	rt.calls = nil
	if err := e.Run(ctx, Request{Job: "gggg1111", Action: "remove", App: "navidrome"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := strings.Join(rt.calls, ","); !strings.Contains(got, "rmi docker.io/deluan/navidrome@"+rec.PreviousDigest) || !strings.Contains(got, "rmi docker.io/deluan/navidrome@"+newDigest) {
		t.Errorf("images not removed with the app: %v", rt.calls)
	}
}

// breakingRuntime is an upgrade that ruins the app's data on start.
type breakingRuntime struct {
	*fakeRuntime
	data string
}

func (b *breakingRuntime) Up(ctx context.Context, p catalogue.Plan, dir string) error {
	if p.App.Tag == "0.66.0" {
		os.WriteFile(filepath.Join(b.data, "navidrome.db"), []byte("migrated by 0.66, broken"), 0o644)
	}
	return b.fakeRuntime.Up(ctx, p, dir)
}

func TestUpdateRequests(t *testing.T) {
	for _, bad := range []string{
		`{"job":"abcd1234","action":"update","app":"kavita","tag":"latest","digest":"` + newDigest + `"}`,
		`{"job":"abcd1234","action":"update","app":"kavita","tag":"0.9.2"}`,
		`{"job":"abcd1234","action":"install","app":"kavita","tag":"0.9.2","digest":"` + newDigest + `"}`,
	} {
		if _, err := ParseRequest([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// A second job for the same app waits for the first, then is checked
// against what it left.
func TestOneJobPerApp(t *testing.T) {
	e, _, media := testEnv(t)
	unlock, err := e.lockApp("navidrome")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background(), navidrome(media)) }()
	select {
	case err := <-done:
		t.Fatalf("ran while another job held the app: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("after the lock: %v", err)
	}
}
