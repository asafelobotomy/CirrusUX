package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cirrusux/internal/snapshot"
	"cirrusux/internal/sysinfo"
)

func nextWakes(ctx context.Context, units []string, errf func(string, ...any)) []snapshot.Wake {
	wakes := []snapshot.Wake{}
	if len(units) == 0 {
		return wakes
	}
	out, err := exec.CommandContext(ctx, "systemctl", sysinfo.ListTimersArgs...).Output()
	if err != nil {
		errf("timers: %v", err)
		return wakes
	}
	timers, err := sysinfo.ParseTimers(string(out))
	if err != nil {
		errf("timers: %v", err)
		return wakes
	}
	want := map[string]bool{}
	for _, u := range units {
		want[u] = true
	}
	for _, t := range timers {
		if want[t.Unit] {
			wakes = append(wakes, snapshot.Wake{At: t.Next, Unit: t.Unit})
		}
	}
	return wakes
}

func bootTime() time.Time { return sysinfo.BootTime() }

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

var errNoSensor = errors.New("no thermal zones")

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
		return 0, errNoSensor
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
