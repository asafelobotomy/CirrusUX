// Package version holds the build's version, stamped from the VERSION file
// by the Makefile (-ldflags -X). Plain `go build` and `go run` report "dev".
package version

import (
	"fmt"
	"strings"
)

var (
	Version = "dev"
	Commit  = ""
)

// String is "0.1.0 (a1b2c3d)", or just the version without a commit.
func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}

// Number is the build's plain "X.Y.Z", or "" for a dev build or any version
// that isn't one (so callers can tell "unknown" from a real release).
func Number() string {
	if p, ok := ParseNumber(Version); ok {
		return fmt.Sprintf("%d.%d.%d", p[0], p[1], p[2])
	}
	return ""
}

// ParseNumber reads "X.Y.Z", with an optional leading "v" and an optional
// "-suffix" or "+build" that is ignored. Anything else is not a version.
func ParseNumber(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 6 {
			return out, false
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return out, false
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out, true
}

// AtLeast reports whether have is the same version as need or newer. A have
// that isn't a version (a dev build) accepts everything; a need that isn't
// a version is never met.
func AtLeast(have, need string) bool {
	h, ok := ParseNumber(have)
	if !ok {
		return true
	}
	n, ok := ParseNumber(need)
	if !ok {
		return false
	}
	for i := range h {
		if h[i] != n[i] {
			return h[i] > n[i]
		}
	}
	return true
}
