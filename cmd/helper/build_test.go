package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/fsutil"
)

const testJob = "0123456789abcdef"

func testRequest(t *testing.T) build.Request {
	t.Helper()
	p, err := build.Parse([]byte("steps:\n  - name: test\n    image: golang:1.25\n    commands:\n      - go test ./...\n  - name: lint\n    image: alpine:3\n    commands:\n      - echo ok\n"))
	if err != nil {
		t.Fatal(err)
	}
	return build.Request{Job: testJob, Repo: "notes", Ref: "main", Commit: strings.Repeat("a", 40), Pipeline: p}
}

func sourceGz(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	tw.WriteHeader(&tar.Header{Name: "main.go", Typeflag: tar.TypeReg, Mode: 0o644, Size: 12})
	tw.Write([]byte("package main"))
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

// fakeBuild sets up a build folder as build-job leaves it, and records the
// commands the build runs. exit says what each command returns.
func fakeBuild(t *testing.T, exit func(args []string) int) (dir string, calls *[]string) {
	t.Helper()
	buildsDir = t.TempDir()
	t.Cleanup(func() { buildsDir = "/var/lib/cirrusux/builds" })
	dir = filepath.Join(buildsDir, testJob)
	os.Mkdir(dir, 0o755)
	if err := fsutil.WriteJSON(filepath.Join(dir, "request.json"), testRequest(t), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "source.tar.gz"), sourceGz(t), 0o600)
	var list []string
	old, oldChown := buildCmd, chownTree
	t.Cleanup(func() { buildCmd, chownTree = old, oldChown })
	chownTree = func(string, int, int) error { return nil }
	buildCmd = func(ctx context.Context, out io.Writer, name string, args ...string) (int, error) {
		line := append([]string{name}, args...)
		list = append(list, strings.Join(line, " "))
		fmt.Fprintf(out, "ran %s\n", name)
		return exit(line), nil
	}
	return dir, &list
}

func readStatus(t *testing.T, dir string) build.Status {
	t.Helper()
	var st build.Status
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &st)
	return st
}

var testConf = conf{ContainerRuntime: "podman", Build: buildConf{On: true}}

func TestBuildRunPasses(t *testing.T) {
	dir, calls := fakeBuild(t, func(a []string) int {
		if a[1] == "network" && a[2] == "inspect" {
			return 1 // not there yet: created
		}
		return 0
	})
	if _, err := buildRun(testConf, testJob); err != nil {
		t.Fatal(err)
	}
	st := readStatus(t, dir)
	if st.State != build.Passed || st.Steps[0].State != build.Passed || st.Steps[1].State != build.Passed || st.Finished == nil {
		t.Fatalf("%+v", st)
	}
	all := strings.Join(*calls, "\n")
	for _, want := range []string{"nft -f " + filepath.Join(dir, "firewall.nft"), "podman network create", "podman pull golang:1.25",
		"podman run --rm --name cirrusux-build-" + testJob + "-1 --network cirrusux-build --cap-drop ALL"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace")); !os.IsNotExist(err) {
		t.Error("workspace left behind")
	}
	if log, _ := os.ReadFile(filepath.Join(dir, "log")); !strings.Contains(string(log), "Step 2 of 2: lint") {
		t.Errorf("log: %s", log)
	}
}

func TestBuildRunStopsAtFailedStep(t *testing.T) {
	dir, calls := fakeBuild(t, func(a []string) int {
		if a[1] == "run" && strings.Contains(strings.Join(a, " "), "-1 ") {
			return 2
		}
		return 0
	})
	buildRun(testConf, testJob)
	st := readStatus(t, dir)
	if st.State != build.Failed || st.Steps[0].State != build.Failed || *st.Steps[0].Exit != 2 || st.Steps[1].State != build.Skipped {
		t.Fatalf("%+v", st)
	}
	if strings.Contains(strings.Join(*calls, "\n"), "alpine") {
		t.Error("ran the step after the one that failed")
	}
}

func TestBuildRunNeedsFirewall(t *testing.T) {
	dir, calls := fakeBuild(t, func(a []string) int {
		if a[0] == "nft" {
			return 1
		}
		return 0
	})
	buildRun(testConf, testJob)
	st := readStatus(t, dir)
	if st.State != build.Failed || !strings.Contains(st.Error, "firewall") {
		t.Fatalf("%+v", st)
	}
	for _, c := range *calls {
		if strings.Contains(c, " run ") || strings.Contains(c, " pull ") {
			t.Fatalf("ran %q without the firewall", c)
		}
	}
}

func TestCappedLog(t *testing.T) {
	var buf bytes.Buffer
	l := &cappedLog{w: &buf, left: 10}
	l.Write([]byte("12345678"))
	l.Write([]byte("abcdef"))
	l.Write([]byte("more"))
	if !strings.HasPrefix(buf.String(), "12345678ab\n[The log is cut") || strings.Contains(buf.String(), "more") {
		t.Fatalf("%q", buf.String())
	}
}

func TestBuildSettings(t *testing.T) {
	var c conf
	for k, v := range map[string]string{"BUILDS": "yes", "BUILD_SUBNET": "10.99.0.0/24", "BUILD_MEMORY_MB": "4096", "BUILD_CPUS": "4", "BUILD_MAX_MINUTES": "90"} {
		if err := keys[k](&c, v); err != nil {
			t.Fatalf("%s=%s: %v", k, v, err)
		}
	}
	if !c.Build.On || c.Build.subnet() != "10.99.0.0/24" || c.Build.limits().MemoryMB != 4096 || c.Build.maxMinutes() != 90 {
		t.Fatalf("%+v", c.Build)
	}
	for k, v := range map[string]string{"BUILDS": "1", "BUILD_SUBNET": "10.99.0.1/24", "BUILD_MEMORY_MB": "12", "BUILD_CPUS": "x"} {
		if keys[k](&c, v) == nil {
			t.Errorf("%s=%s accepted", k, v)
		}
	}
	c = conf{Build: buildConf{On: true}}
	if c.features().Modules != nil && containsStr(c.features().Modules, "builds") {
		t.Error("builds without a container runtime")
	}
	c.ContainerRuntime = "docker"
	if !containsStr(c.features().Modules, "builds") {
		t.Error("builds module missing")
	}
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestPruneBuilds(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "00000000aaaaaaaa")
	fresh := filepath.Join(dir, "00000000bbbbbbbb")
	os.Mkdir(old, 0o755)
	os.Mkdir(fresh, 0o755)
	past := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(old, past, past)
	pruneBuilds(dir, 7*24*time.Hour, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old build kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh build removed")
	}
}
