package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/execx"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/osupdate"
)

// osJob starts an OS check or upgrade as a background job
// (cirrusux-job-<id>.service), which keeps a sleeping server awake.
func osJob(ctx context.Context, what string) (helperapi.Result, error) {
	job := newJobID()
	unit, err := launchJob(ctx, job, []string{"RuntimeMaxSec=90min"}, helperapi.CmdOSRun, what, job)
	if err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{"os " + what + ": started as " + unit}}, nil
}

// osRun is the job itself, started by systemd-run as root. Its progress
// goes to a status file like an app job's.
func osRun(what, job string) (helperapi.Result, error) {
	st := appjob.Status{Job: job, Action: "os-" + what, App: "os", Name: "Operating system", State: "running", Started: time.Now()}
	save := func() { saveStatus(st) }
	r := &osupdate.Runner{
		Exec: aptExec, Now: time.Now,
		Progress:   func(s string) { st.Step = s; save() },
		ResultFile: helperapi.OSUpdatesFile, RebootFile: helperapi.RebootRequiredFile,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Minute)
	defer cancel()
	var res osupdate.Result
	var err error
	if what == "upgrade" {
		res, err = r.Upgrade(ctx)
	} else {
		res, err = r.Check(ctx)
	}
	t := time.Now()
	st.Finished = &t
	switch {
	case err != nil:
		st.State, st.Error = "failed", err.Error()
	case what == "upgrade":
		st.State, st.Step = "done", fmt.Sprintf("Installed %d package update(s)", res.UpgradedCount)
	default:
		st.State, st.Step = "done", fmt.Sprintf("%d package update(s) available", len(res.Packages))
	}
	if what == "upgrade" && err == nil && len(res.Changes) > 0 {
		st.Detail = osupdate.Summary(res.Changes, 25)
	}
	if res.RebootRequired {
		st.Detail = strings.TrimSpace(st.Detail + " — a restart is needed to finish")
	}
	save()
	return helperapi.Result{Output: []string{st.Step}}, err
}

func aptExec(ctx context.Context, env []string, name string, args ...string) (string, error) {
	out, err := execx.Run(ctx, execx.Options{Combined: true, Env: env, MaxOutput: 8 << 20}, name, args...)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return string(out), fmt.Errorf("%s %s: %v: %s", name, args[0], err, lines[len(lines)-1])
	}
	return string(out), nil
}
