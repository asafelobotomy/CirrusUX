package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/execx"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/selfupgrade"
	appversion "cirrusux/internal/version"
)

// CirrusUX's own upgrade and rollback, started from the panel. Both run
// `cirrusux-setup`, the same command the owner would type, as a background
// job: applying a release restarts the agent (and the gateway), so the job
// must outlive whoever asked for it.

// setupLink is selfupgrade.SetupLink; tests point it elsewhere.
var setupLink = selfupgrade.SetupLink

// selfJob starts an upgrade to version, or a rollback (version "-").
func selfJob(ctx context.Context, what, version string) (helperapi.Result, error) {
	info := selfupgrade.Read(setupLink)
	if !info.Managed {
		return helperapi.Result{}, errors.New("CirrusUX here wasn't installed from a release, so it is upgraded by hand (docs/INSTALL.md, Upgrading)")
	}
	// Only forwards: whoever can reach this rule must not be able to put
	// an older release, with bugs since fixed, back in place. Going back
	// one step is rollback's job, to the release that ran here before.
	if what == "upgrade" && !olderThan(info.Current, version) {
		return helperapi.Result{}, errors.New("this machine runs " + info.Current + ": an upgrade only goes to a newer release")
	}
	if what == "rollback" && info.Previous == "" {
		return helperapi.Result{}, errors.New("there's no earlier release to go back to")
	}
	job := newJobID()
	unit, err := launchJob(ctx, job, []string{"RuntimeMaxSec=30min"}, helperapi.CmdSelfRun, what, job, version)
	if err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{"cirrusux " + what + ": started as " + unit}}, nil
}

// olderThan reports whether release tag have is older than tag want.
func olderThan(have, want string) bool {
	h, ok1 := appversion.ParseNumber(have)
	w, ok2 := appversion.ParseNumber(want)
	if !ok1 || !ok2 {
		return false
	}
	for i := range h {
		if h[i] != w[i] {
			return h[i] < w[i]
		}
	}
	return false
}

// selfArgs is cirrusux-setup's command line for the job.
func selfArgs(what, version string) []string {
	if what == "rollback" {
		return []string{"rollback", "-y"}
	}
	return []string{"upgrade", version, "-y"}
}

// selfRun is the job itself, started by systemd-run as root. setup prints
// what it changes; the end of that is kept with the job.
func selfRun(what, job, version string) (helperapi.Result, error) {
	before := selfupgrade.Read(setupLink)
	st := appjob.Status{Job: job, Action: "self-" + what, App: "cirrusux", Name: "CirrusUX", State: "running", Started: time.Now()}
	st.Step = "Downloading and checking " + version
	if what == "rollback" {
		st.Step = "Going back to " + before.Previous
	}
	_ = os.MkdirAll(helperapi.JobsDir, 0o755)
	saveStatus(st)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	out, err := execx.Run(ctx, execx.Options{Combined: true, MaxOutput: 1 << 20}, setupLink, selfArgs(what, version)...)
	lines := lastLines(string(out), 3)
	after := selfupgrade.Read(setupLink)
	t := time.Now()
	st.Finished = &t
	if err != nil {
		st.State, st.Error = "failed", err.Error()
		if len(lines) > 0 {
			st.Error = lines[len(lines)-1]
		}
		st.Step = "Still on " + after.Current
		if after.Current != before.Current {
			// The release switched but applying it stopped part-way.
			st.Step = "Switched to " + after.Current + ", but applying it failed: run sudo cirrusux-setup apply"
		}
	} else {
		st.State, st.Step = "done", "Now on "+after.Current
		if after.Current == before.Current {
			st.Step = "Already on " + after.Current
		}
	}
	st.Detail = strings.Join(lines, " · ")
	saveStatus(st)
	return helperapi.Result{Output: []string{st.Step}}, err
}

// lastLines is the last n non-empty lines of s.
func lastLines(s string, n int) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
