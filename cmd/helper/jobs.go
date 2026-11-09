package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
)

// newJobID is a random job ID, as helperapi.JobRe expects.
func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// launchJob starts `cirrusux-helper args...` as the transient unit
// cirrusux-job-<job>.service, with the given unit properties (a time
// limit, at least). The idle check counts these units as busy, so a job
// keeps a sleeping server awake until it's done.
func launchJob(ctx context.Context, job string, props []string, args ...string) (string, error) {
	appjob.PruneStatuses(helperapi.JobsDir, 7*24*time.Hour, time.Now())
	self, _ := os.Executable()
	unit := "cirrusux-job-" + job
	argv := []string{"--quiet", "--collect", "--unit=" + unit}
	for _, p := range props {
		argv = append(argv, "--property="+p)
	}
	argv = append(append(argv, self), args...)
	if out, err := exec.CommandContext(ctx, "systemd-run", argv...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("starting the job: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return unit, nil
}

// saveStatus writes a job's progress where the agent reads it.
func saveStatus(st appjob.Status) {
	_ = fsutil.WriteJSON(filepath.Join(helperapi.JobsDir, st.Job+".json"), st, 0o644)
}
