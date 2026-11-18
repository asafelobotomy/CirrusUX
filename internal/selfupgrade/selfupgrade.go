// Package selfupgrade is what CirrusUX knows about its own install on a
// machine: which release runs, which one came before, and whether the
// panel can move between them. Anyone may read it: the release links are
// world-readable, so neither the agent nor the gateway needs root to look.
package selfupgrade

import (
	"os"
	"path/filepath"
	"regexp"
)

// SetupLink is where setup links the release in use
// (deploy/host/modules/cirrusux.sh). Root runs it for upgrade and rollback.
const SetupLink = "/usr/local/sbin/cirrusux-setup"

// VersionRe matches a release's tag, vX.Y.Z, as install.sh accepts it.
var VersionRe = regexp.MustCompile(`^v(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})$`)

// Info is this machine's install.
type Info struct {
	// Managed: installed from a release bundle (install.sh), with the
	// `current` and `previous` links `cirrusux-setup upgrade` moves. A
	// copy built from source is not, and is upgraded by hand.
	Managed bool `json:"managed"`
	// Current and Previous are release tags (v0.8.1); Previous is empty
	// when there is nothing to go back to.
	Current  string `json:"current,omitempty"`
	Previous string `json:"previous,omitempty"`
}

// Read looks at the install setupLink points into: the link names
// <prefix>/releases/vX.Y.Z/deploy/host/cirrusux-setup, and <prefix>/current
// is a link to the release in use. Anything else is not managed.
func Read(setupLink string) Info {
	target, err := os.Readlink(setupLink)
	if err != nil {
		return Info{}
	}
	rel := filepath.Dir(filepath.Dir(filepath.Dir(target)))
	if !VersionRe.MatchString(filepath.Base(rel)) || filepath.Base(filepath.Dir(rel)) != "releases" {
		return Info{}
	}
	prefix := filepath.Dir(filepath.Dir(rel))
	cur := release(prefix, "current")
	if cur == "" {
		return Info{}
	}
	return Info{Managed: true, Current: cur, Previous: release(prefix, "previous")}
}

// release is the tag the link prefix/name points at, if that release is
// still unpacked there.
func release(prefix, name string) string {
	t, err := os.Readlink(filepath.Join(prefix, name))
	if err != nil || filepath.Dir(t) != "releases" || !VersionRe.MatchString(filepath.Base(t)) {
		return ""
	}
	if st, err := os.Stat(filepath.Join(prefix, t)); err != nil || !st.IsDir() {
		return ""
	}
	return filepath.Base(t)
}
