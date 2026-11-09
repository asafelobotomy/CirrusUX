package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/appjob"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/osupdate"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/storage"
	"cirrusux/internal/sysinfo"
)

// Collector gathers one snapshot. Every source is read locally: files,
// unix sockets and short commands. Nothing opens a TCP connection to a
// port the idle check watches (PLAN §2).
type Collector struct {
	// IdleConf is the idle checker's config (IDLE_MIN, GRACE_MIN,
	// WAKE_TIMERS), read without root. Optional.
	IdleConf string
	// PortNames label the idle checker's port numbers, e.g. 8080 → "Proxy".
	PortNames map[string]string
	// DiskMount is the file system the disk widget shows; "" skips it.
	DiskMount string
	// WakeIface names the interface the server is woken on; "" uses the
	// IPv4 default route's.
	WakeIface string
	// RoutePath and NetSysPath default to /proc/net/route and
	// /sys/class/net; tests point them at fakes.
	RoutePath, NetSysPath string
	DockerSocket          string // the filtered socket, /run/cirrusux/docker.sock
	// Helper is the argv prefix for the root helper, e.g.
	// ["sudo", "-n", "/usr/local/sbin/cirrusux-helper"]. Empty skips it.
	Helper []string

	temps      []float64
	dockerC    *http.Client // one client, so its connections are reused
	dockerOnce sync.Once    // collection and actions ask for it at once

	mu       sync.Mutex
	features []string // from the last collection, for the command loop
}

// has reports whether the last collection found the module.
func (c *Collector) has(mod string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Contains(c.features, mod)
}

const maxTemps = 24

func (c *Collector) Collect(ctx context.Context) snapshot.Snapshot {
	now := time.Now()
	host, _ := os.Hostname()
	s := snapshot.Snapshot{Host: host, TakenAt: now, Features: []string{}, Containers: []snapshot.Container{}, Timeline: []snapshot.Segment{}, Backups: snapshot.Backups{Files: []snapshot.BackupFile{}}}
	errf := func(format string, a ...any) { s.Errors = append(s.Errors, fmt.Sprintf(format, a...)) }

	s.BootAt = bootTime()
	s.WakeIface, s.WakeMAC = sysinfo.WakeNIC(c.WakeIface, cmp.Or(c.RoutePath, sysinfo.RoutePath), cmp.Or(c.NetSysPath, sysinfo.NetSysPath))
	s.Power = snapshot.Power{Since: s.BootAt, Reasons: []string{}, NextWakes: []snapshot.Wake{}}

	// Which modules the helper has set up; without a helper, none.
	if len(c.Helper) > 0 {
		var f helperapi.Features
		if err := c.helper(ctx, helperapi.CmdFeatures, &f); err != nil {
			errf("helper: %v", err)
		} else {
			s.Features = append(s.Features, f.Modules...)
		}
	}
	c.mu.Lock()
	c.features = s.Features
	c.mu.Unlock()
	has := func(mod string) bool { return slices.Contains(s.Features, mod) }

	if has(helperapi.ModSleep) {
		c.collectSleep(ctx, &s, errf)
	}

	if cs, err := c.containers(ctx); err != nil {
		errf("containers: %v", err)
	} else {
		s.Containers = cs
	}

	if has(helperapi.ModBackup) {
		var bl helperapi.BackupList
		if err := c.helper(ctx, helperapi.CmdBackupList, &bl); err != nil {
			errf("backup list: %v", err)
		}
		for _, f := range bl.Files {
			s.Backups.Files = append(s.Backups.Files, snapshot.BackupFile{
				Name: f.Name, At: time.Unix(f.MTime, 0), Bytes: f.Bytes, Encrypted: strings.HasSuffix(f.Name, ".age"),
			})
		}
		sort.Slice(s.Backups.Files, func(i, j int) bool { return s.Backups.Files[i].At.After(s.Backups.Files[j].At) })
		var bj helperapi.BackupLast
		if err := c.helper(ctx, helperapi.CmdBackupLast, &bj); err != nil {
			errf("backup journal: %v", err)
		}
		s.Backups.LastRun = parseBackupJournal(bj.Lines, bj.Unit, now)
		if bj.Unit != "" {
			if w := nextWakes(ctx, []string{strings.TrimSuffix(bj.Unit, ".service") + ".timer"}, errf); len(w) > 0 {
				s.Backups.NextRun = &w[0].At
			}
		}
	}

	if c.DiskMount != "" {
		if d, err := c.disk(); err != nil {
			errf("disk: %v", err)
		} else {
			s.Disk = d
		}
	}

	if t, err := cpuTemp(); errors.Is(err, errNoSensor) {
		// A VM or a board without sensors: nothing to show, nothing wrong.
	} else if err != nil {
		errf("temperature: %v", err)
	} else {
		if len(c.temps) == maxTemps {
			c.temps = c.temps[1:]
		}
		c.temps = append(c.temps, t)
	}
	s.Temps = append([]float64{}, c.temps...)

	if has(helperapi.ModSecurity) || has(helperapi.ModAV) {
		s.Security = &snapshot.Security{Firewall: []snapshot.FirewallCheck{}}
	}
	if has(helperapi.ModSecurity) {
		var sec helperapi.SecurityStatus
		if err := c.helper(ctx, helperapi.CmdSecurityStatus, &sec); err != nil {
			errf("security: %v", err)
		} else {
			if sec.Fail2ban {
				n := sec.Banned
				s.Security.Banned = &n
			}
			for _, f := range sec.Firewall {
				s.Security.Firewall = append(s.Security.Firewall, snapshot.FirewallCheck{Chain: f.Chain, OK: f.OK})
			}
			for _, e := range sec.Errors {
				errf("security: %s", e)
			}
		}
	}
	if has(helperapi.ModApps) || has(helperapi.ModOS) || has(helperapi.ModStorage) {
		s.Jobs = recentJobs(helperapi.JobsDir, now)
	}
	if has(helperapi.ModStorage) {
		s.Storage = storage.Read(helperapi.StorageFile)
	}
	if has(helperapi.ModOS) {
		s.OSUpdates = osupdate.Read(helperapi.OSUpdatesFile, helperapi.RebootRequiredFile)
	}
	if has(helperapi.ModApps) {
		s.Apps = installedApps()
		var info helperapi.AppsInfo
		if err := c.helper(ctx, helperapi.CmdAppsInfo, &info); err != nil {
			errf("apps info: %v", err)
		} else {
			s.AppsHost = &info
		}
	}
	if has(helperapi.ModAV) {
		var av helperapi.Lines
		if err := c.helper(ctx, helperapi.CmdAVLast, &av); err != nil {
			errf("virus scan: %v", err)
		}
		s.Security.ClamAV = parseClamAV(av.Lines)
	}
	return s
}

// collectSleep reads the idle checker: its config, log and state files.
func (c *Collector) collectSleep(ctx context.Context, s *snapshot.Snapshot, errf func(string, ...any)) {
	now := s.TakenAt
	conf := map[string]string{}
	if c.IdleConf != "" {
		if b, err := os.ReadFile(c.IdleConf); err == nil {
			conf = parseConf(string(b))
		} else if !os.IsNotExist(err) {
			errf("idle config: %v", err)
		}
	}
	s.Power.IdleMin = confInt(conf, "IDLE_MIN", 30)
	s.Power.GraceMin = confInt(conf, "GRACE_MIN", 15)
	s.Power.NextWakes = nextWakes(ctx, strings.Fields(conf["WAKE_TIMERS"]), errf)

	var idleLog helperapi.Lines
	if err := c.helper(ctx, helperapi.CmdIdleLog, &idleLog); err != nil {
		errf("idle log: %v", err)
	}
	events := parseIdleLog(idleLog.Lines, now)
	s.Power.Since = lastResume(events, s.BootAt)
	s.Power.LastActivity = lastActivity(events)
	s.Timeline = timeline(events, now, s.BootAt)

	var idle helperapi.IdleStatus
	if err := c.helper(ctx, helperapi.CmdIdleStatus, &idle); err != nil {
		errf("idle status: %v", err)
		return
	}
	// The checker's own state is more precise than its log.
	if r := reasonsFromMsg(idle.State["last-msg"], c.PortNames); r != nil {
		s.Power.Reasons = r
	}
	if t := parseEpoch(idle.State["last-active"]); t != nil && (s.Power.LastActivity == nil || t.After(*s.Power.LastActivity)) {
		s.Power.LastActivity = t
	}
	if t := parseEpoch(idle.State["last-wake"]); t != nil && t.After(s.BootAt) && t.After(s.Power.Since) {
		s.Power.Since = *t
	}
	if idle.StayAwake != nil {
		s.Power.StayAwakeUntil, s.Power.StayAwakeForever = parseStayAwake(*idle.StayAwake)
	}
}

// appsAndJobs reads the app store's records and recent job statuses; the
// helper writes both readable, so no root is needed.
func installedApps() []snapshot.App {
	var apps []snapshot.App
	for _, r := range appjob.Records(helperapi.AppRecordsDir) {
		apps = append(apps, snapshot.App{ID: r.App, Name: r.Name, Tag: r.Tag, Digest: r.Digest, PreviousTag: r.PreviousTag, PreviousDigest: r.PreviousDigest, Runtime: r.Runtime, MainPort: r.MainPort, HTTPS: r.HTTPS, Installed: r.Installed, Source: r.Source, Serial: r.Serial})
	}
	return apps
}

// recentJobs: running ones, and the last day's (app store and OS updates).
func recentJobs(dir string, now time.Time) []snapshot.Job {
	var jobs []snapshot.Job
	for _, j := range appjob.Statuses(dir) {
		if j.State != "running" && now.Sub(j.Started) > 24*time.Hour || len(jobs) == 10 {
			continue
		}
		// A job killed on the way (out of memory, stopped, past its time
		// limit) never writes its end: once its unit is gone, it failed.
		if j.State == "running" && now.Sub(j.Started) > time.Minute && !jobUnitActive(j.Job) {
			j.State, j.Error = "failed", "the job stopped before it finished"
		}
		jobs = append(jobs, snapshot.Job{ID: j.Job, Action: j.Action, App: j.App, Name: j.Name, State: j.State, Step: j.Step, Detail: j.Detail, Error: j.Error, Started: j.Started, Finished: j.Finished})
	}
	return jobs
}

// jobUnitActive reports whether a job's systemd unit still runs; tests
// replace it.
var jobUnitActive = func(job string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", "cirrusux-job-"+job+".service").Run() == nil
}

func (c *Collector) helper(ctx context.Context, sub string, out any, args ...string) error {
	if len(c.Helper) == 0 {
		return fmt.Errorf("no helper configured")
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout(sub))
	defer cancel()
	argv := append(append(append([]string{}, c.Helper...), sub), args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(stderr.String()))
	}
	return json.Unmarshal(b, out)
}

// helperTimeout leaves the helper time to finish its own work first: it
// gives the egress report 150 s and everything else 30 s.
func helperTimeout(sub string) time.Duration {
	if sub == helperapi.CmdEgressReport {
		return 3 * time.Minute
	}
	return 40 * time.Second
}
