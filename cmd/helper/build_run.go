package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/helperapi"
)

// maxBuildLog is how much of a build's output is kept.
const maxBuildLog = 4 << 20

// buildCmd runs a command with its output going to out, and returns its
// exit code; err is set only when it couldn't run or was stopped. Tests
// replace it.
var buildCmd = func(ctx context.Context, out io.Writer, name string, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 10 * time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return exit.ExitCode(), nil
	}
	return -1, err
}

// chownTree gives the workspace to the build user; tests, which can't,
// replace it.
var chownTree = build.ChownTree

// buildRun is the build itself, run by systemd as cirrusux-job-<job>.
func buildRun(c conf, job string) (helperapi.Result, error) {
	dir := filepath.Join(buildsDir, job)
	raw, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		return helperapi.Result{}, err
	}
	req, err := build.ParseRequest(raw)
	if err != nil {
		return helperapi.Result{}, err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return helperapi.Result{}, err
	}
	defer logf.Close()
	b := &builder{c: c, req: req, dir: dir, log: &cappedLog{w: logf, left: maxBuildLog}}
	b.st = build.NewStatus(req, time.Now())
	b.save()

	stopped, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(stopped, time.Duration(req.Pipeline.Minutes)*time.Minute)
	defer cancel()

	err = b.run(ctx)
	defer b.cleanup()
	switch {
	case stopped.Err() != nil:
		b.logf("\nCancelled.\n")
		finishStatus(&b.st, build.Cancelled, "Cancelled")
	case ctx.Err() != nil:
		msg := fmt.Sprintf("Stopped after the pipeline's time limit (%d min)", req.Pipeline.Minutes)
		b.logf("\n%s.\n", msg)
		finishStatus(&b.st, build.Failed, msg)
	case err != nil:
		b.logf("\n%v\n", err)
		finishStatus(&b.st, build.Failed, err.Error())
	default:
		finishStatus(&b.st, build.Passed, "")
	}
	b.save()
	return helperapi.Result{Output: []string{"build " + job + ": " + b.st.State}}, nil
}

type builder struct {
	c   conf
	req build.Request
	dir string
	log *cappedLog
	st  build.Status
}

func (b *builder) save()                        { saveBuildStatus(b.dir, b.st) }
func (b *builder) logf(format string, a ...any) { fmt.Fprintf(b.log, format, a...) }
func (b *builder) workspace() string            { return filepath.Join(b.dir, "workspace") }
func (b *builder) rt() string                   { return b.c.ContainerRuntime }
func (b *builder) cmd(ctx context.Context, args ...string) (int, error) {
	return buildCmd(ctx, b.log, b.rt(), args...)
}

func (b *builder) run(ctx context.Context) error {
	b.logf("Building %s at %s (%s)\n", b.req.Repo, b.req.Commit[:12], b.req.Ref)
	if err := b.prepare(ctx); err != nil {
		return err
	}
	for i, s := range b.req.Pipeline.Steps {
		if err := b.step(ctx, i, s); err != nil {
			return err
		}
	}
	return nil
}

// prepare sets up the network and its firewall, then unpacks the source
// for the build user.
func (b *builder) prepare(ctx context.Context) error {
	if b.rt() == "" {
		return errors.New("builds need CONTAINER_RUNTIME in helper.env")
	}
	rules := filepath.Join(b.dir, "firewall.nft")
	if err := os.WriteFile(rules, []byte(build.Firewall()), 0o600); err != nil {
		return err
	}
	if code, err := buildCmd(ctx, b.log, "nft", "-f", rules); err != nil || code != 0 {
		return errors.New("couldn't load the firewall rules that keep builds off your network (is nftables installed?), so nothing ran")
	}
	if code, _ := buildCmd(ctx, io.Discard, b.rt(), "network", "inspect", build.Network); code != 0 {
		if code, err := b.cmd(ctx, build.NetworkArgs(b.rt(), b.c.Build.subnet())...); err != nil || code != 0 {
			return fmt.Errorf("couldn't create the %s network", build.Network)
		}
	}
	ws := b.workspace()
	if err := os.Mkdir(ws, 0o700); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(b.dir, "source.tar.gz"))
	if err != nil {
		return err
	}
	err = build.Extract(f, ws)
	f.Close()
	if err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(ws, ".cirrus-home"), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return chownTree(ws, build.UID, build.UID)
}

func (b *builder) step(ctx context.Context, i int, s build.Step) error {
	now := time.Now()
	ss := &b.st.Steps[i]
	ss.State, ss.Started = build.Running, &now
	b.save()
	b.logf("\n── Step %d of %d: %s (%s) ──\n", i+1, len(b.req.Pipeline.Steps), s.Name, s.Image)
	if code, err := b.cmd(ctx, "pull", s.Image); err != nil || code != 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return b.stepFailed(i, -1, fmt.Sprintf("Step %s: couldn't pull %s", s.Name, s.Image))
	}
	code, err := b.cmd(ctx, build.RunArgs(b.req, i, b.workspace(), b.c.Build.limits())...)
	if ctx.Err() != nil || err != nil {
		// The runtime keeps a container going when its client is stopped.
		rm, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		buildCmd(rm, io.Discard, b.rt(), "rm", "-f", build.ContainerName(b.req.Job, i))
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return b.stepFailed(i, -1, fmt.Sprintf("Step %s: %v", s.Name, err))
	}
	if code != 0 {
		return b.stepFailed(i, code, fmt.Sprintf("Step %s failed (exit code %d)", s.Name, code))
	}
	end := time.Now()
	ss.State, ss.Finished, ss.Exit = build.Passed, &end, &code
	b.save()
	return nil
}

func (b *builder) stepFailed(i, code int, msg string) error {
	end := time.Now()
	ss := &b.st.Steps[i]
	ss.State, ss.Finished = build.Failed, &end
	if code >= 0 {
		ss.Exit = &code
	}
	b.save()
	return errors.New(msg)
}

// cleanup removes the workspace and source; the status and log stay for a
// week (pruneBuilds).
func (b *builder) cleanup() {
	os.RemoveAll(b.workspace())
	os.Remove(filepath.Join(b.dir, "source.tar.gz"))
}

// cappedLog keeps the first maxBuildLog bytes of output and says once
// that the rest was dropped.
type cappedLog struct {
	mu   sync.Mutex
	w    io.Writer
	left int
	cut  bool
}

func (l *cappedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case len(p) <= l.left:
		l.left -= len(p)
		return l.w.Write(p)
	case !l.cut:
		l.cut = true
		l.w.Write(p[:l.left])
		l.left = 0
		io.WriteString(l.w, "\n[The log is cut here at "+fmt.Sprint(maxBuildLog>>20)+" MB; the build carries on.]\n")
	}
	return len(p), nil
}
