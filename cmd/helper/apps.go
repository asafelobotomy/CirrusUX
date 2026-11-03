package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/catalogue"
	"cirrusux/internal/helperapi"
)

// Saved, checked requests wait here for their job (root only).
const jobsQueue = "/var/lib/cirrusux/jobs"

// appEnv builds the job runner for this machine from the helper's config.
func appEnv(c conf) (*appjob.Env, error) {
	cat, err := catalogue.Load()
	if err != nil {
		return nil, err
	}
	u, err := user.Lookup(c.AppsUser)
	if err != nil {
		return nil, fmt.Errorf("APPS_USER %s: %v", c.AppsUser, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	var rt appjob.Runtime = appjob.Docker{Exec: appjob.RunCommand}
	if c.ContainerRuntime == "podman" {
		rt = appjob.Podman{Exec: appjob.RunCommand, UnitDir: "/etc/containers/systemd"}
	}
	archive := c.archiveDir()
	sources, _ := storeSources(c)
	return &appjob.Env{
		Runtime: rt, Catalogue: cat, Sources: sources, Now: time.Now,
		Host: catalogue.Host{
			AppsDir: c.AppsDir, TZ: localZone(), UID: uid, GID: gid,
			Groups: hostGroups("/etc/group"), MediaRoots: c.MediaRoots, BindAddress: c.AppsBind,
		},
		ArchiveDir: archive, RecordsDir: helperapi.AppRecordsDir, StatusDir: helperapi.JobsDir,
		PortFree: portFree, Healthy: healthy, HealthTimeout: 3 * time.Minute, Chown: os.Chown,
	}, nil
}

// appsInfo reports the facts installs use (groups only those apps' devices
// ask for) and the CirrusStore sources the helper trusts.
func appsInfo(c conf) (helperapi.AppsInfo, error) {
	env, err := appEnv(c)
	if err != nil {
		return helperapi.AppsInfo{}, err
	}
	h := env.Host
	groups := map[string]int{}
	// The device groups apps from any CirrusStore source may ask for, and
	// whatever the built-in ones name.
	for _, g := range []string{"render", "video", "audio", "dialout"} {
		if gid, ok := h.Groups[g]; ok {
			groups[g] = gid
		}
	}
	for _, a := range env.Catalogue {
		for _, d := range a.Devices {
			for _, g := range d.HostGroups {
				if gid, ok := h.Groups[g]; ok {
					groups[g] = gid
				}
			}
		}
	}
	_, sources := storeSources(c)
	return helperapi.AppsInfo{Runtime: env.Runtime.Name(), AppsDir: h.AppsDir, MediaRoots: h.MediaRoots, Bind: h.BindAddress, TZ: h.TZ, UID: h.UID, GID: h.GID, Groups: groups, Sources: sources}, nil
}

// appJob takes the oldest request the agent left, checks it in full, and
// starts it as a background job (cirrusux-job-<id>.service), which the idle
// checker counts as busy. A request that fails the check never runs.
func appJob(ctx context.Context, c conf, job string) (helperapi.Result, error) {
	env, err := appEnv(c)
	if err != nil {
		return helperapi.Result{}, err
	}
	b, err := readRequest(helperapi.RequestsDir, job)
	if err != nil {
		return helperapi.Result{}, err
	}
	req, err := appjob.ParseRequest(b)
	if err != nil {
		return helperapi.Result{}, fmt.Errorf("request: %v", err)
	}
	if _, err := env.Check(req); err != nil {
		_ = env.Run(ctx, req) // records the refusal in the job's status
		return helperapi.Result{}, err
	}
	if err := os.MkdirAll(jobsQueue, 0o700); err != nil {
		return helperapi.Result{}, err
	}
	saved := filepath.Join(jobsQueue, req.Job+".json")
	if err := os.WriteFile(saved, b, 0o600); err != nil {
		return helperapi.Result{}, err
	}
	unit, err := launchJob(ctx, req.Job, []string{"RuntimeMaxSec=45min"}, helperapi.CmdAppRun, req.Job)
	if err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{fmt.Sprintf("%s %s: started as %s", req.Action, req.App, unit)}}, nil
}

// appRun is the job itself, started by systemd-run as root.
func appRun(c conf, job string) (helperapi.Result, error) {
	env, err := appEnv(c)
	if err != nil {
		return helperapi.Result{}, err
	}
	saved := filepath.Join(jobsQueue, job+".json")
	b, err := os.ReadFile(saved)
	if err != nil {
		return helperapi.Result{}, err
	}
	req, err := appjob.ParseRequest(b)
	if err != nil || req.Job != job {
		return helperapi.Result{}, fmt.Errorf("saved request %s is damaged", job)
	}
	defer os.Remove(saved)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if err := env.Run(ctx, req); err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{req.Action + " " + req.App + ": done"}}, nil
}

// readRequest reads and removes the request <job>.json in dir. The agent
// writes that folder and root reads it, so everything goes through an
// os.Root opened at its parent (root-made, the agent can't replace it):
// a symlink there, for the folder or the file, can't lead root anywhere
// else, to read or to remove.
func readRequest(dir, job string) ([]byte, error) {
	if !helperapi.JobRe.MatchString(job) {
		return nil, errors.New("not a job ID")
	}
	root, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Join(filepath.Base(dir), job+".json")
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("request: %v", err)
	}
	// Whatever happens next, the request has been seen: it goes.
	defer root.Remove(name)
	if !fi.Mode().IsRegular() || fi.Size() > 16<<10 {
		return nil, errors.New("request isn't a small regular file")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("request: %v", err)
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 16<<10))
}

// portFree tries to bind the port: if it can, nothing else has it.
func portFree(port int, proto string) bool {
	addr := ":" + strconv.Itoa(port)
	if proto == "udp" {
		c, err := net.ListenPacket("udp", addr)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// healthy is one GET on the app's own address. Anything below 500 means
// it's answering (a login page or a redirect is fine).
func healthy(ctx context.Context, url string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		// Apps on 127.0.0.1 with their own certificate: only liveness is checked.
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // #nosec G402 — loopback health check
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// hostGroups reads group name → gid from /etc/group.
func hostGroups(p string) map[string]int {
	m := map[string]int{}
	f, err := os.Open(p)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Split(sc.Text(), ":")
		if len(fs) >= 3 {
			if gid, err := strconv.Atoi(fs[2]); err == nil {
				m[fs[0]] = gid
			}
		}
	}
	return m
}

// localZone is the machine's IANA zone name, for apps' TZ.
func localZone() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, tz, ok := strings.Cut(p, "zoneinfo/"); ok {
			return tz
		}
	}
	return "UTC"
}
