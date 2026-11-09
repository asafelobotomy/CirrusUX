//go:build live

package appjob

// Real installs, updates and removals with rootless Podman, as the user
// running the test: user Quadlet units, ports on 127.0.0.1 only. They
// download Syncthing's image (two versions for the update test). Run them
// on purpose:
//
//	go test -tags live -run TestLivePodman -v ./internal/appjob

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/catalogue"
)

const (
	syncthingImage = "docker.io/syncthing/syncthing"
	// The catalogue's pin, and the release before it (both from Docker Hub).
	syncthing217 = "sha256:2b71da7c0d0a91a6ae5285ad079226fecc0a365d08b27720e1f7037b018f7546"
	syncthing216 = "sha256:4d0281bdcb4ed9baf503d251d0544ddd856f49fce3f114056257ae172b7fb670"
)

// liveEnv is a job runner for rootless Podman in a scratch folder. Cleanup
// stops the unit and removes the images and every file the container made.
func liveEnv(t *testing.T) (*Env, string) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman isn't installed")
	}
	home, _ := os.UserHomeDir()
	root := t.TempDir()
	media := filepath.Join(root, "media")
	os.MkdirAll(filepath.Join(media, "sync"), 0o755)
	cat, err := catalogue.Load()
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{
		Runtime:   Podman{Exec: RunCommand, UnitDir: filepath.Join(home, ".config/containers/systemd"), User: true},
		Catalogue: cat, Now: time.Now,
		Host: catalogue.Host{AppsDir: filepath.Join(root, "apps"), TZ: "UTC", UID: os.Getuid(), GID: os.Getgid(),
			MediaRoots: []string{media}, BindAddress: "127.0.0.1"},
		ArchiveDir: filepath.Join(root, "archive"), RecordsDir: filepath.Join(root, "records"), StatusDir: filepath.Join(root, "jobs"),
		PortFree: func(port int, proto string) bool {
			addr := "127.0.0.1:" + strconv.Itoa(port)
			if proto == "udp" {
				c, err := net.ListenPacket("udp", addr)
				if err == nil {
					c.Close()
				}
				return err == nil
			}
			l, err := net.Listen("tcp", addr)
			if err == nil {
				l.Close()
			}
			return err == nil
		},
		Healthy: func(ctx context.Context, url string) error {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
			}
			return err
		},
		HealthTimeout: 2 * time.Minute,
		// The container's files belong to its mapped user: copy them as it.
		Copy: func(src, dst string) error {
			if out, err := exec.Command("podman", "unshare", "cp", "-a", "--", src, dst).CombinedOutput(); err != nil {
				return &copyErr{string(out)}
			}
			return nil
		},
	}
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "--user", "stop", "cirrusux-syncthing.service").Run()
		os.Remove(filepath.Join(home, ".config/containers/systemd/cirrusux-syncthing.container"))
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		_ = exec.Command("podman", "unshare", "rm", "-rf", root).Run()
		for _, d := range []string{syncthing217, syncthing216} {
			_ = exec.Command("podman", "rmi", "-f", syncthingImage+"@"+d).Run()
		}
	})
	return env, media
}

type copyErr struct{ out string }

func (e *copyErr) Error() string { return "cp: " + e.out }

func syncthingRequest(job, media string) Request {
	return Request{Job: job, Action: "install", App: "syncthing", Options: catalogue.Options{
		Ports: map[string]int{"web": 18384, "sync": 22001, "sync-quic": 22001, "discovery": 21028},
		Media: map[string]string{"sync": filepath.Join(media, "sync")},
	}}
}

// podmanPS is what Podman runs now: image and labels of the syncthing container.
func podmanPS() string {
	out, _ := exec.Command("podman", "ps", "--filter", "name=^syncthing$", "--format", "{{.Image}} {{.Ports}} {{.Labels}}").Output()
	return string(out)
}

func TestLivePodman(t *testing.T) {
	env, media := liveEnv(t)
	ctx := context.Background()
	start := time.Now()
	if err := env.Run(ctx, syncthingRequest("1111aaaa", media)); err != nil {
		t.Fatalf("install: %v\nstatus: %+v", err, Statuses(env.StatusDir))
	}
	t.Logf("installed in %s", time.Since(start).Round(time.Second))
	out := podmanPS()
	// Pulled as tag@digest, Podman reports the digest only: the pin holds.
	if !strings.Contains(out, syncthingImage+"@"+syncthing217[:19]) || !strings.Contains(out, "127.0.0.1:18384->8384/tcp") {
		t.Errorf("podman ps:\n%s", out)
	}
	if !strings.Contains(out, "io.cirrusux.app=syncthing") || !strings.Contains(out, "io.cirrusux.version=2.1.7") {
		t.Errorf("label missing:\n%s", out)
	}
	if recs := Records(env.RecordsDir); len(recs) != 1 || recs[0].MainPort != 18384 {
		t.Errorf("records = %+v", recs)
	}

	if err := env.Run(ctx, Request{Job: "2222bbbb", Action: "remove", App: "syncthing"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if out, _ := exec.Command("podman", "ps", "-a", "--format", "{{.Names}}").Output(); strings.Contains(string(out), "syncthing") {
		t.Errorf("container still there:\n%s", out)
	}
	if a, _ := filepath.Glob(filepath.Join(env.ArchiveDir, "syncthing-*", "data")); len(a) != 1 {
		t.Error("folder not archived")
	}
}

// The Phase 6 done-when, for real: install the release before the pin,
// update to the pin with the data intact and backed up, then roll back.
func TestLivePodmanUpdate(t *testing.T) {
	env, media := liveEnv(t)
	ctx := context.Background()
	for i, a := range env.Catalogue {
		if a.ID == "syncthing" {
			env.Catalogue[i] = a.WithPin("2.1.6", syncthing216)
		}
	}
	if err := env.Run(ctx, syncthingRequest("3333cccc", media)); err != nil {
		t.Fatalf("install 2.1.6: %v", err)
	}
	if out := podmanPS(); !strings.Contains(out, "io.cirrusux.version=2.1.6") {
		t.Fatalf("not on 2.1.6:\n%s", out)
	}
	// Syncthing wrote its settings; this marker must survive the update.
	data := filepath.Join(env.Host.AppsDir, "syncthing", "data")
	marker := filepath.Join(data, "cirrusux-marker")
	if out, err := exec.Command("podman", "unshare", "sh", "-c", "echo before-update > "+marker).CombinedOutput(); err != nil {
		t.Fatalf("marker: %v %s", err, out)
	}

	start := time.Now()
	if err := env.Run(ctx, Request{Job: "4444dddd", Action: "update", App: "syncthing", Tag: "2.1.7", Digest: syncthing217}); err != nil {
		t.Fatalf("update: %v\nstatus: %+v", err, Statuses(env.StatusDir))
	}
	t.Logf("updated in %s", time.Since(start).Round(time.Second))
	if out := podmanPS(); !strings.Contains(out, "io.cirrusux.version=2.1.7") || !strings.Contains(out, syncthing217[:19]) {
		t.Errorf("not on 2.1.7:\n%s", out)
	}
	if b, err := exec.Command("podman", "unshare", "cat", marker).Output(); err != nil || strings.TrimSpace(string(b)) != "before-update" {
		t.Errorf("data lost in the update: %q %v", b, err)
	}
	// The backup's files belong to the container's user too: look as it.
	if out, err := exec.Command("podman", "unshare", "sh", "-c", "cat "+env.ArchiveDir+"/syncthing-before-2.1.6-*/data/cirrusux-marker").Output(); err != nil || strings.TrimSpace(string(out)) != "before-update" {
		t.Errorf("no backup of the data before the update: %q %v", out, err)
	}
	rec := Records(env.RecordsDir)[0]
	if rec.Tag != "2.1.7" || rec.PreviousTag != "2.1.6" || rec.PreviousDigest != syncthing216 {
		t.Errorf("record = %+v", rec)
	}

	// Rolling back is an update to the previous version.
	if err := env.Run(ctx, Request{Job: "5555eeee", Action: "update", App: "syncthing", Tag: rec.PreviousTag, Digest: rec.PreviousDigest}); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if out := podmanPS(); !strings.Contains(out, "io.cirrusux.version=2.1.6") {
		t.Errorf("not back on 2.1.6:\n%s", out)
	}
	if err := env.Run(ctx, Request{Job: "6666ffff", Action: "remove", App: "syncthing"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, s := range Statuses(env.StatusDir) {
		t.Logf("job %s %-7s %s — %s", s.Job, s.Action, s.State, s.Step)
	}
}
