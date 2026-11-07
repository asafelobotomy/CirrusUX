package githost

import (
	"errors"
	"strings"
	"testing"
)

func kindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return 0, false
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"notes", "my-app", "a.b_c", "x", strings.Repeat("a", 64)} {
		if err := ValidName(ok); err != nil {
			t.Errorf("ValidName(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", ".x", "X", "a/b", "../x", "x.git", ".trash", strings.Repeat("a", 65), "a b"} {
		err := ValidName(bad)
		if k, ok := kindOf(err); !ok || k != Invalid {
			t.Errorf("ValidName(%q) = %v, want an Invalid error", bad, err)
		}
	}
}

func TestCleanPath(t *testing.T) {
	good := map[string]string{"": "", "src/main.go": "src/main.go", "src//a/./b": "src/a/b"}
	for in, want := range good {
		got, err := cleanPath(in)
		if err != nil || got != want {
			t.Errorf("cleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"/", "/etc/passwd", "../x", "a/../../x", "-x", "a/-b"} {
		if _, err := cleanPath(bad); err == nil {
			t.Errorf("cleanPath(%q) accepted", bad)
		}
	}
}

func TestValidBranch(t *testing.T) {
	for _, ok := range []string{"main", "feature/x", "v1.0"} {
		if err := validBranch(ok); err != nil {
			t.Errorf("validBranch(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "a..b", "x.lock", "a b", "x/", "a~b", "a:b", "a\x01b"} {
		if err := validBranch(bad); err == nil {
			t.Errorf("validBranch(%q) accepted", bad)
		}
	}
}

func TestValidCommit(t *testing.T) {
	for _, ok := range []string{"abcd", strings.Repeat("a", 40)} {
		if err := validCommit(ok); err != nil {
			t.Errorf("validCommit(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"abc", "xyz1", strings.Repeat("a", 65), "-p", "ABCD"} {
		if err := validCommit(bad); err == nil {
			t.Errorf("validCommit(%q) accepted", bad)
		}
	}
}
