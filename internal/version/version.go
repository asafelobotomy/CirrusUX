// Package version holds the build's version, stamped from the VERSION file
// by the Makefile (-ldflags -X). Plain `go build` and `go run` report "dev".
package version

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
