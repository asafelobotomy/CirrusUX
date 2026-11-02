//go:build live

package appjob

// A real install and removal with rootless Podman, as the user running the
// test: user Quadlet units, ports on 127.0.0.1 only. It downloads the
// catalogue's Syncthing image. Run it on purpose:
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

func TestLivePodman(t *testing.T) {
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
	}
	t.Cleanup(func() {
		// The container wrote files as its own mapped user: remove them as it.
		_ = exec.Command("podman", "unshare", "rm", "-rf", root).Run()
		_ = exec.Command("systemctl", "--user", "stop", "cirrusux-syncthing.service").Run()
		os.Remove(filepath.Join(home, ".config/containers/systemd/cirrusux-syncthing.container"))
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		_ = exec.Command("podman", "rmi", "-f", "docker.io/syncthing/syncthing@sha256:2b71da7c0d0a91a6ae5285ad079226fecc0a365d08b27720e1f7037b018f7546").Run()
	})

	ctx := context.Background()
	install := Request{Job: "1111aaaa", Action: "install", App: "syncthing", Options: catalogue.Options{
		Ports: map[string]int{"web": 18384, "sync": 22001, "sync-quic": 22001, "discovery": 21028},
		Media: map[string]string{"sync": filepath.Join(media, "sync")},
	}}
	start := time.Now()
	if err := env.Run(ctx, install); err != nil {
		t.Fatalf("install: %v\nstatus: %+v", err, Statuses(env.StatusDir))
	}
	t.Logf("installed in %s", time.Since(start).Round(time.Second))
	out, _ := exec.Command("podman", "ps", "--format", "{{.Names}} {{.Image}} {{.Ports}} {{.Labels}}").Output()
	// Pulled as tag@digest, Podman reports the digest only: the pin holds.
	if !strings.Contains(string(out), "syncthing docker.io/syncthing/syncthing@sha256:2b71da7c0d0a") || !strings.Contains(string(out), "127.0.0.1:18384->8384/tcp") {
		t.Errorf("podman ps:\n%s", out)
	}
	if !strings.Contains(string(out), "io.cirrusux.app=syncthing") || !strings.Contains(string(out), "io.cirrusux.version=2.1.7") {
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
	for _, s := range Statuses(env.StatusDir) {
		t.Logf("job %s %s: %s — %s", s.Job, s.Action, s.State, s.Step)
	}
}
