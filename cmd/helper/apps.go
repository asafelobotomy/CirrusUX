package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
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
	archive := c.AppsArchive
	if archive == "" {
		archive = filepath.Join(filepath.Dir(c.AppsDir), "archive")
	}
	return &appjob.Env{
		Runtime: rt, Catalogue: cat, Now: time.Now,
		Host: catalogue.Host{
			AppsDir: c.AppsDir, TZ: localZone(), UID: uid, GID: gid,
			Groups: hostGroups("/etc/group"), MediaRoots: c.MediaRoots, BindAddress: c.AppsBind,
		},
		ArchiveDir: archive, RecordsDir: helperapi.AppRecordsDir, StatusDir: helperapi.JobsDir,
		PortFree: portFree, Healthy: healthy, HealthTimeout: 3 * time.Minute, Chown: os.Chown,
	}, nil
}

// appsInfo reports the facts installs use; groups only those the
// catalogue's devices ask for.
func appsInfo(c conf) (helperapi.AppsInfo, error) {
	env, err := appEnv(c)
	if err != nil {
		return helperapi.AppsInfo{}, err
	}
	h := env.Host
	groups := map[string]int{}
	for _, a := range env.Catalogue {
		for _, d := range a.Devices {
			for _, g := range d.HostGroups {
				if gid, ok := h.Groups[g]; ok {
					groups[g] = gid
				}
			}
		}
	}
	return helperapi.AppsInfo{Runtime: env.Runtime.Name(), AppsDir: h.AppsDir, MediaRoots: h.MediaRoots, Bind: h.BindAddress, TZ: h.TZ, UID: h.UID, GID: h.GID, Groups: groups}, nil
}

// appJob takes the oldest request the agent left, checks it in full, and
// starts it as a background job (cirrusux-job-<id>.service), which the idle
// checker counts as busy. A request that fails the check never runs.
func appJob(ctx context.Context, c conf) (helperapi.Result, error) {
	env, err := appEnv(c)
	if err != nil {
		return helperapi.Result{}, err
	}
	name, b, err := oldestRequest(helperapi.RequestsDir)
	if err != nil {
		return helperapi.Result{}, err
	}
	// Whatever happens next, the request has been read: it goes.
	defer os.Remove(name)
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
	self, _ := os.Executable()
	unit := "cirrusux-job-" + req.Job
	if out, err := exec.CommandContext(ctx, "systemd-run", "--quiet", "--collect", "--unit="+unit,
		"--property=TimeoutStartSec=45min", self, helperapi.CmdAppRun, req.Job).CombinedOutput(); err != nil {
		return helperapi.Result{}, fmt.Errorf("starting the job: %v: %s", err, strings.TrimSpace(string(out)))
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

// oldestRequest reads the oldest *.json in dir, refusing symlinks and
// anything that isn't a small regular file: the agent writes this folder,
// and root reads it.
func oldestRequest(dir string) (string, []byte, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) == 0 {
		return "", nil, errors.New("no request waiting")
	}
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Lstat(files[i])
		b, _ := os.Lstat(files[j])
		return a != nil && b != nil && a.ModTime().Before(b.ModTime())
	})
	name := files[0]
	f, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		os.Remove(name)
		return name, nil, fmt.Errorf("request: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 16<<10 {
		return name, nil, errors.New("request isn't a small regular file")
	}
	b := make([]byte, fi.Size())
	_, err = f.Read(b)
	return name, b, err
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
