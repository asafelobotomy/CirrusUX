package selfupgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// install lays out a prefix as install.sh does and returns the setup link.
func install(t *testing.T, current, previous string) string {
	t.Helper()
	dir := t.TempDir()
	prefix := filepath.Join(dir, "opt")
	for _, v := range []string{current, previous} {
		if v == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(prefix, "releases", v, "deploy", "host"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("releases/"+current, filepath.Join(prefix, "current")); err != nil {
		t.Fatal(err)
	}
	if previous != "" {
		if err := os.Symlink("releases/"+previous, filepath.Join(prefix, "previous")); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "cirrusux-setup")
	if err := os.Symlink(filepath.Join(prefix, "releases", current, "deploy", "host", "cirrusux-setup"), link); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestReadManagedInstall(t *testing.T) {
	got := Read(install(t, "v0.9.0", "v0.8.1"))
	if want := (Info{Managed: true, Current: "v0.9.0", Previous: "v0.8.1"}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := Read(install(t, "v0.9.0", "")); !got.Managed || got.Previous != "" {
		t.Errorf("no previous release: got %+v", got)
	}
}

// A checkout built from source, or no install at all, is not managed.
func TestReadUnmanaged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "CirrusUX", "deploy", "host")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cirrusux-setup")
	if err := os.Symlink(filepath.Join(src, "cirrusux-setup"), link); err != nil {
		t.Fatal(err)
	}
	if got := Read(link); got.Managed {
		t.Errorf("a source checkout counted as managed: %+v", got)
	}
	if got := Read(filepath.Join(dir, "missing")); got.Managed {
		t.Errorf("a missing link counted as managed: %+v", got)
	}
}

// A previous link to a release that was removed offers nothing to go back to.
func TestReadPreviousGone(t *testing.T) {
	link := install(t, "v0.9.0", "v0.8.1")
	prefix := filepath.Join(filepath.Dir(link), "opt")
	if err := os.RemoveAll(filepath.Join(prefix, "releases", "v0.8.1")); err != nil {
		t.Fatal(err)
	}
	if got := Read(link); !got.Managed || got.Previous != "" {
		t.Errorf("got %+v", got)
	}
}

func TestVersionRe(t *testing.T) {
	for _, s := range []string{"v0.8.1", "v1.0.0", "v12.345.6"} {
		if !VersionRe.MatchString(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "0.8.1", "v0.8", "v0.8.1-rc1", "v01.2.3", "v1.2.3 -y", "v1.2.3\n", "latest", "-y", "v1.2.99999"} {
		if VersionRe.MatchString(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
