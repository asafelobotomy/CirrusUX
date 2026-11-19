package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/execx"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
)

// Builds (docs/BUILDS.md). The agent leaves <job>.json (the request) and
// <job>.tar.gz (the source) in its requests folder; build-job checks the
// request again, moves both into a folder of root's own, and starts the
// build as a job of its own (build-run). Each step then runs in a fresh
// container with fixed settings: no privileges, its own network kept off
// the local one, limits on memory, CPU and processes.

// buildsDir is helperapi.BuildsDir; tests point it elsewhere.
var buildsDir = helperapi.BuildsDir

const maxSourceGz = 1 << 30

func buildCommand(ctx context.Context, c conf, sub, job string) (helperapi.Result, error) {
	switch sub {
	case helperapi.CmdBuildJob:
		return buildJob(ctx, c, job)
	case helperapi.CmdBuildCancel:
		return buildCancel(ctx, job)
	}
	return buildRun(c, job)
}

func buildJob(ctx context.Context, c conf, job string) (helperapi.Result, error) {
	raw, err := readAgentFile(helperapi.RequestsDir, job+".json", 8<<20)
	if err != nil {
		return helperapi.Result{}, err
	}
	req, err := build.ParseRequest(raw)
	if err != nil {
		removeAgentFile(helperapi.RequestsDir, job+".tar.gz")
		return helperapi.Result{}, err
	}
	if req.Job != job {
		removeAgentFile(helperapi.RequestsDir, job+".tar.gz")
		return helperapi.Result{}, errors.New("the request is for another build")
	}
	if req.Pipeline.Minutes > c.Build.maxMinutes() {
		removeAgentFile(helperapi.RequestsDir, job+".tar.gz")
		return helperapi.Result{}, fmt.Errorf("the pipeline asks for %d minutes; this server allows up to %d (BUILD_MAX_MINUTES)", req.Pipeline.Minutes, c.Build.maxMinutes())
	}
	pruneBuilds(buildsDir, 7*24*time.Hour, time.Now())
	if err := os.MkdirAll(buildsDir, 0o755); err != nil {
		return helperapi.Result{}, err
	}
	dir := filepath.Join(buildsDir, job)
	if err := os.Mkdir(dir, 0o755); err != nil {
		removeAgentFile(helperapi.RequestsDir, job+".tar.gz")
		return helperapi.Result{}, fmt.Errorf("build %s: %v", job, err)
	}
	if err := copyAgentFile(helperapi.RequestsDir, job+".tar.gz", filepath.Join(dir, "source.tar.gz"), maxSourceGz); err != nil {
		os.RemoveAll(dir)
		return helperapi.Result{}, err
	}
	if err := fsutil.WriteJSON(filepath.Join(dir, "request.json"), req, 0o600); err != nil {
		os.RemoveAll(dir)
		return helperapi.Result{}, err
	}
	saveBuildStatus(dir, build.NewStatus(req, time.Now()))
	props := []string{"RuntimeMaxSec=" + strconv.Itoa(req.Pipeline.Minutes+15) + "min", "KillMode=mixed", "TimeoutStopSec=60"}
	unit, err := launchJob(ctx, job, props, helperapi.CmdBuildRun, job)
	if err != nil {
		st := build.NewStatus(req, time.Now())
		finishStatus(&st, build.Failed, err.Error())
		saveBuildStatus(dir, st)
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{"build " + job + ": started as " + unit}}, nil
}

// buildCancel stops a running build. Its job sees the signal, removes the
// step's container and records the build as cancelled.
func buildCancel(ctx context.Context, job string) (helperapi.Result, error) {
	if fi, err := os.Lstat(filepath.Join(buildsDir, job)); err != nil || !fi.IsDir() {
		return helperapi.Result{}, fmt.Errorf("no build %s on this server", job)
	}
	out, err := execx.Run(ctx, execx.Options{Combined: true}, "systemctl", "stop", "--no-block", "cirrusux-job-"+job+".service")
	if err != nil {
		return helperapi.Result{}, fmt.Errorf("stopping build %s: %v: %s", job, err, out)
	}
	return helperapi.Result{Output: []string{"build " + job + ": stopping"}}, nil
}

func saveBuildStatus(dir string, st build.Status) {
	_ = fsutil.WriteJSON(filepath.Join(dir, "status.json"), st, 0o644)
}

func finishStatus(st *build.Status, state, msg string) {
	now := time.Now()
	st.State, st.Error, st.Finished = state, msg, &now
	for i := range st.Steps {
		s := &st.Steps[i]
		switch s.State {
		case build.Waiting:
			s.State = build.Skipped
		case build.Running:
			s.State, s.Finished = state, &now
		}
	}
}

// pruneBuilds removes builds older than keep, with their logs.
func pruneBuilds(dir string, keep time.Duration, now time.Time) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		fi, err := e.Info()
		if err == nil && e.IsDir() && helperapi.JobRe.MatchString(e.Name()) && now.Sub(fi.ModTime()) > keep {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// openAgentFile opens name in the agent's folder dir through an os.Root at
// its parent, as readRequest does: the agent owns that folder and could
// swap a file for a link, which the os.Root keeps from leading anywhere
// else. Only a regular file up to limit bytes is read.
func openAgentFile(dir, name string, limit int64) (*os.File, func(), error) {
	root, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return nil, nil, err
	}
	rel := filepath.Join(filepath.Base(dir), name)
	done := func() { root.Remove(rel); root.Close() }
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		done()
		return nil, nil, fmt.Errorf("%s: %v", name, err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() > limit {
		f.Close()
		done()
		return nil, nil, fmt.Errorf("%s isn't a regular file of up to %d MB", name, limit>>20)
	}
	return f, func() { f.Close(); done() }, nil
}

func readAgentFile(dir, name string, limit int64) ([]byte, error) {
	f, done, err := openAgentFile(dir, name, limit)
	if err != nil {
		return nil, err
	}
	defer done()
	return io.ReadAll(io.LimitReader(f, limit))
}

func copyAgentFile(dir, name, to string, limit int64) error {
	f, done, err := openAgentFile(dir, name, limit)
	if err != nil {
		return err
	}
	defer done()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(f, limit))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func removeAgentFile(dir, name string) {
	if root, err := os.OpenRoot(filepath.Dir(dir)); err == nil {
		root.Remove(filepath.Join(filepath.Base(dir), name))
		root.Close()
	}
}
