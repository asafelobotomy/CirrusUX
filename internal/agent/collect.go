package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
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
	DiskMount    string
	DockerSocket string // the filtered socket, /run/cirrusux/docker.sock
	// Helper is the argv prefix for the root helper, e.g.
	// ["sudo", "-n", "/usr/local/sbin/cirrusux-helper"]. Empty skips it.
	Helper []string

	temps   []float64
	dockerC *http.Client // one client, so its connections are reused

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
	}

	if c.DiskMount != "" {
		if d, err := c.disk(); err != nil {
			errf("disk: %v", err)
		} else {
			s.Disk = d
		}
	}

	if t, err := cpuTemp(); err != nil {
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
		} else {
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

// ---- Docker, through the filtered unix socket ----

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Ports  []struct {
		PublicPort int `json:"PublicPort"`
	} `json:"Ports"`
}

// helperTimeout leaves the helper time to finish its own work first: it
// gives the egress report 150 s and everything else 30 s.
func helperTimeout(sub string) time.Duration {
	if sub == helperapi.CmdEgressReport {
		return 3 * time.Minute
	}
	return 40 * time.Second
}

// docker returns the client for the filtered socket. It's created once:
// a new Transport per call would leave its idle connections open for good.
// Callers set deadlines through their context.
func (c *Collector) docker() *http.Client {
	if c.dockerC == nil {
		c.dockerC = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", c.DockerSocket)
				},
				MaxIdleConns:    2,
				IdleConnTimeout: 30 * time.Second,
			},
		}
	}
	return c.dockerC
}

func (c *Collector) containers(ctx context.Context) ([]snapshot.Container, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := c.docker()
	var list []dockerContainer
	if err := getJSON(ctx, client, "http://docker/containers/json?all=1", &list); err != nil {
		return nil, err
	}
	out := make([]snapshot.Container, 0, len(list))
	for _, d := range list {
		img, tag := splitImage(d.Image)
		ct := snapshot.Container{
			Name: strings.TrimPrefix(firstOr(d.Names, d.ID[:12]), "/"), Image: img, Tag: tag,
			State: d.State, Status: d.Status, Health: containerHealth(d.State, d.Status), Ports: []int{},
		}
		seen := map[int]bool{}
		for _, p := range d.Ports {
			if p.PublicPort != 0 && !seen[p.PublicPort] {
				seen[p.PublicPort] = true
				ct.Ports = append(ct.Ports, p.PublicPort)
			}
		}
		if d.State == "running" {
			var insp struct {
				State struct {
					StartedAt time.Time `json:"StartedAt"`
				} `json:"State"`
			}
			if err := getJSON(ctx, client, "http://docker/containers/"+d.ID+"/json", &insp); err == nil && !insp.State.StartedAt.IsZero() {
				t := insp.State.StartedAt
				ct.UpSince = &t
			}
		}
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// ---- systemd timers ----

func nextWakes(ctx context.Context, units []string, errf func(string, ...any)) []snapshot.Wake {
	wakes := []snapshot.Wake{}
	if len(units) == 0 {
		return wakes
	}
	out, err := exec.CommandContext(ctx, "systemctl", "list-timers", "--all", "--output=json").Output()
	if err != nil {
		errf("timers: %v", err)
		return wakes
	}
	var timers []struct {
		Next int64  `json:"next"` // µs since epoch
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(out, &timers); err != nil {
		errf("timers: %v", err)
		return wakes
	}
	want := map[string]bool{}
	for _, u := range units {
		want[u] = true
	}
	for _, t := range timers {
		if want[t.Unit] && t.Next > 0 {
			wakes = append(wakes, snapshot.Wake{At: time.UnixMicro(t.Next), Unit: t.Unit})
		}
	}
	sort.Slice(wakes, func(i, j int) bool { return wakes[i].At.Before(wakes[j].At) })
	return wakes
}

// ---- /proc and /sys ----

func bootTime() time.Time {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
	}
	return time.Time{}
}

func (c *Collector) disk() (*snapshot.Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(c.DiskMount, &st); err != nil {
		return nil, err
	}
	d := &snapshot.Disk{
		Mount:      c.DiskMount,
		TotalBytes: st.Blocks * uint64(st.Bsize),
		FreeBytes:  st.Bavail * uint64(st.Bsize),
		LinkMbps:   usbSpeed(c.DiskMount),
	}
	return d, nil
}

// usbSpeed finds the block device behind mount and walks up its sysfs path
// to the USB device's "speed" file (Mb/s). 0 when not on USB.
func usbSpeed(mount string) int {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return 0
	}
	var dev string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == mount {
			dev = f[0]
		}
	}
	if !strings.HasPrefix(dev, "/dev/") {
		return 0
	}
	name := filepath.Base(dev)
	if p, err := filepath.EvalSymlinks(dev); err == nil {
		name = filepath.Base(p)
	}
	sys, err := filepath.EvalSymlinks("/sys/class/block/" + name)
	if err != nil {
		return 0
	}
	for dir := sys; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if b, err := os.ReadFile(filepath.Join(dir, "speed")); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				return n
			}
		}
	}
	return 0
}

// cpuTemp prefers the package sensor, else the first thermal zone.
func cpuTemp() (float64, error) {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	var first string
	for _, z := range zones {
		typ, _ := os.ReadFile(z + "/type")
		if first == "" {
			first = z
		}
		if strings.TrimSpace(string(typ)) == "x86_pkg_temp" {
			first = z
			break
		}
	}
	if first == "" {
		return 0, fmt.Errorf("no thermal zones")
	}
	b, err := os.ReadFile(first + "/temp")
	if err != nil {
		return 0, err
	}
	milli, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	return float64(milli) / 1000, nil
}
