package osupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const listing = `Listing...
bash/stable-security 5.2.15-2+b7 amd64 [upgradable from: 5.2.15-2+b2]
tzdata/stable-updates 2026b-0+deb13u1 all [upgradable from: 2026a-0+deb13u1]
WARNING: apt does not have a stable CLI interface. Use with caution in scripts.
`

func TestParseUpgradable(t *testing.T) {
	p := ParseUpgradable(listing)
	if len(p) != 2 || p[0] != (Package{"bash", "5.2.15-2+b2", "5.2.15-2+b7", true}) || p[1].Security || p[1].To != "2026b-0+deb13u1" {
		t.Errorf("packages = %+v", p)
	}
	if len(ParseUpgradable("Listing...\n")) != 0 {
		t.Error("nothing upgradable")
	}
}

func TestCheckAndUpgrade(t *testing.T) {
	dir := t.TempDir()
	var ran []string
	upgraded := false
	exec := func(_ context.Context, env []string, name string, args ...string) (string, error) {
		if strings.Join(env, " ") != "DEBIAN_FRONTEND=noninteractive LC_ALL=C" {
			t.Errorf("env = %v", env)
		}
		ran = append(ran, name+" "+strings.Join(args, " "))
		if name == "apt-get" && args[0] == "upgrade" {
			upgraded = true
		}
		if name == "apt" {
			if upgraded {
				return "Listing...\n", nil
			}
			return listing, nil
		}
		return "", nil
	}
	var steps []string
	r := &Runner{Exec: exec, Progress: func(s string) { steps = append(steps, s) }, Now: func() time.Time { return time.Unix(1793600000, 0) },
		ResultFile: filepath.Join(dir, "os-updates.json"), RebootFile: filepath.Join(dir, "reboot-required")}

	res, err := r.Check(context.Background())
	if err != nil || len(res.Packages) != 2 || res.Upgraded != nil {
		t.Fatalf("check = %+v, %v", res, err)
	}
	// After a restart the file is gone, and the saved result stops asking for one.
	os.WriteFile(r.RebootFile, nil, 0o644)
	if got := Read(r.ResultFile, r.RebootFile); got == nil || !got.RebootRequired {
		t.Error("restart needed, but Read says not")
	}
	os.Remove(r.RebootFile)
	if got := Read(r.ResultFile, r.RebootFile); got == nil || got.RebootRequired {
		t.Error("after a restart, Read still asks for one")
	}
	if got := Read(r.ResultFile, r.RebootFile); got == nil || len(got.Packages) != 2 {
		t.Error("result not written")
	}

	os.WriteFile(r.RebootFile, nil, 0o644)
	ran = nil
	res, err = r.Upgrade(context.Background())
	if err != nil || len(res.Packages) != 0 || res.UpgradedCount != 2 || !res.RebootRequired || res.Upgraded == nil {
		t.Fatalf("upgrade = %+v, %v", res, err)
	}
	if !strings.Contains(strings.Join(ran, "|"), "apt-get upgrade --with-new-pkgs -y -q -o Dpkg::Options::=--force-confold") || strings.Contains(strings.Join(ran, "|"), "dist-upgrade") {
		t.Errorf("ran %v", ran)
	}
	if steps[len(steps)-2] != "Installing 2 packages" {
		t.Errorf("steps = %v", steps)
	}
}
