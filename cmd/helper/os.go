package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/osupdate"
)

// osJob starts an OS check or upgrade as a background job
// (cirrusux-job-<id>.service), which keeps a sleeping server awake.
func osJob(ctx context.Context, what string) (helperapi.Result, error) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	job := hex.EncodeToString(b)
	self, _ := os.Executable()
	unit := "cirrusux-job-" + job
	if out, err := exec.CommandContext(ctx, "systemd-run", "--quiet", "--collect", "--unit="+unit,
		"--property=TimeoutStartSec=90min", self, helperapi.CmdOSRun, what, job).CombinedOutput(); err != nil {
		return helperapi.Result{}, fmt.Errorf("starting the job: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return helperapi.Result{Output: []string{"os " + what + ": started as " + unit}}, nil
}

// osRun is the job itself, started by systemd-run as root. Its progress
// goes to a status file like an app job's.
func osRun(what, job string) (helperapi.Result, error) {
	st := appjob.Status{Job: job, Action: "os-" + what, App: "os", Name: "Operating system", State: "running", Started: time.Now()}
	save := func() {
		b, _ := jsonMarshal(st)
		_ = os.MkdirAll(helperapi.JobsDir, 0o755)
		tmp := filepath.Join(helperapi.JobsDir, "."+job)
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, filepath.Join(helperapi.JobsDir, job+".json"))
		}
	}
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
	if res.RebootRequired {
		st.Detail = "a restart is needed to finish"
	}
	save()
	return helperapi.Result{Output: []string{st.Step}}, err
}

func aptExec(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return string(out), fmt.Errorf("%s %s: %v: %s", name, args[0], err, lines[len(lines)-1])
	}
	return string(out), nil
}

var jsonMarshal = func(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
