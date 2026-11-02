package appjob

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cirrusux/internal/catalogue"
)

// Exec runs a command; progress, if set, gets each output line. Tests
// replace it.
type Exec func(ctx context.Context, progress func(string), name string, args ...string) error

// RunCommand is the real Exec.
func RunCommand(ctx context.Context, progress func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var last []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			last = append(last, line)
			if len(last) > 5 {
				last = last[1:]
			}
			if progress != nil {
				progress(line)
			}
		}
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.Join(last, " / "))
	}
	return nil
}

// Docker runs apps with docker compose; the compose file lives in the
// app's folder, so the app is one `docker compose -p <id>` project.
type Docker struct{ Exec Exec }

func (Docker) Name() string { return "docker" }

func (d Docker) Pull(ctx context.Context, ref string, progress func(string)) error {
	return d.Exec(ctx, progress, "docker", "pull", ref)
}

func (d Docker) Up(ctx context.Context, p catalogue.Plan, appDir string) error {
	file := filepath.Join(appDir, "compose.yml")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(p.Compose()), 0o644); err != nil {
		return err
	}
	return d.Exec(ctx, nil, "docker", "compose", "-p", p.Name, "-f", file, "up", "-d")
}

func (d Docker) Down(ctx context.Context, appID, appDir string) error {
	file := filepath.Join(appDir, "compose.yml")
	if _, err := os.Stat(file); err != nil {
		return nil // never got that far
	}
	return d.Exec(ctx, nil, "docker", "compose", "-p", appID, "-f", file, "down")
}

// Podman runs apps as Quadlet units: systemd starts them at boot and
// restarts them, and `systemctl` stops them.
type Podman struct {
	Exec Exec
	// UnitDir is /etc/containers/systemd, or ~/.config/containers/systemd
	// for rootless Podman.
	UnitDir string
	// User runs rootless: user units, `systemctl --user`.
	User bool
}

func (p Podman) systemctl(ctx context.Context, args ...string) error {
	if p.User {
		args = append([]string{"--user"}, args...)
	}
	return p.Exec(ctx, nil, "systemctl", args...)
}

func (Podman) Name() string { return "podman" }

func (p Podman) Pull(ctx context.Context, ref string, progress func(string)) error {
	return p.Exec(ctx, progress, "podman", "pull", ref)
}

func (p Podman) unit(id string) string  { return filepath.Join(p.UnitDir, "cirrusux-"+id+".container") }
func (Podman) service(id string) string { return "cirrusux-" + id + ".service" }

func (p Podman) Up(ctx context.Context, plan catalogue.Plan, appDir string) error {
	if err := os.MkdirAll(p.UnitDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.unit(plan.Name), []byte(plan.Quadlet()), 0o644); err != nil {
		return err
	}
	// A copy beside the app's data, for the owner and the backups.
	_ = os.WriteFile(filepath.Join(appDir, plan.QuadletName()), []byte(plan.Quadlet()), 0o644)
	if err := p.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	// restart: starts it if stopped, and moves a running app to the new unit.
	return p.systemctl(ctx, "restart", p.service(plan.Name))
}

func (p Podman) Down(ctx context.Context, appID, _ string) error {
	if _, err := os.Stat(p.unit(appID)); err != nil {
		return nil
	}
	if err := p.systemctl(ctx, "stop", p.service(appID)); err != nil {
		return err
	}
	if err := os.Remove(p.unit(appID)); err != nil {
		return err
	}
	return p.systemctl(ctx, "daemon-reload")
}

// CopyTree copies a folder, keeping owners, modes and times (cp -a).
func CopyTree(src, dst string) error {
	if out, err := exec.Command("cp", "-a", "--", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cp: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
