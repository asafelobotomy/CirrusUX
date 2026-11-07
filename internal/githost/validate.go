package githost

import (
	"path"
	"regexp"
	"strings"
)

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	commitRe = regexp.MustCompile(`^[0-9a-f]{4,64}$`)
)

// ValidName checks a repository name: lower case letters, digits, dot,
// dash and underscore, starting with a letter or digit, up to 64
// characters, not ending in .git.
func ValidName(name string) error {
	if !nameRe.MatchString(name) || strings.HasSuffix(name, ".git") {
		return errf(Invalid, "A repository name uses lower-case letters, digits, dots, dashes and underscores, starts with a letter or digit, is up to 64 characters and doesn't end in .git")
	}
	return nil
}

// validBranch checks the shape of a branch or tag name, as git's
// check-ref-format would, and refuses anything that could pass for an
// option.
func validBranch(name string) error {
	bad := name == "" || len(name) > 255 || strings.HasPrefix(name, "-") ||
		strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") ||
		strings.HasPrefix(name, "/") || strings.ContainsAny(name, " ~^:?*[\\")
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			bad = true
		}
	}
	if bad {
		return errf(Invalid, "%q isn't a usable branch or tag name", name)
	}
	return nil
}

// validCommit checks a commit id: 4 to 64 lower-case hex digits.
func validCommit(sha string) error {
	if !commitRe.MatchString(sha) {
		return errf(Invalid, "%q isn't a commit id", sha)
	}
	return nil
}

// cleanPath tidies a path inside a repository. "" is the top folder.
func cleanPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") {
		return "", errf(Invalid, "A path inside a repository can't start with /")
	}
	c := path.Clean(p)
	for _, part := range strings.Split(c, "/") {
		if part == ".." || strings.HasPrefix(part, "-") {
			return "", errf(Invalid, "%q isn't a usable path", p)
		}
	}
	if c == "." {
		return "", nil
	}
	return c, nil
}
