package store

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	// Official is the name of CirrusUX's own source.
	Official = "cirrusstore"
	// OfficialURL is where the official catalogue is published.
	OfficialURL = "https://asafelobotomy.github.io/cirrusstore/"
	// BuiltIn names the apps compiled into the binaries: always there, and
	// trusted without a signature.
	BuiltIn = "built-in"
	// SourcesDir holds the third-party sources the server trusts, one
	// <name>.source file each, written by cirrusux-setup's store module.
	SourcesDir = "/etc/cirrusux/store.d"
)

// officialKeys holds the public half of the official signing key, one key
// line per line (more than one while a key is being replaced). The signing
// key itself stays offline with the publisher.
//
//go:embed official.pub
var officialKeys []byte

// Source is one catalogue the server trusts.
type Source struct {
	Name     string
	URL      string // https, ending in /
	Keys     []Key
	Official bool
}

var officialOverride *Source

// OverrideOfficialForTest swaps the official source for a test one (a test
// server's URL and key) and returns the undo. Tests only.
func OverrideOfficialForTest(s Source) (restore func()) {
	prev := officialOverride
	s.Name, s.Official = Official, true
	officialOverride = &s
	return func() { officialOverride = prev }
}

// OfficialSource is the built-in official source, with whatever keys
// official.pub holds; none means it can't be used yet.
func OfficialSource() Source {
	if officialOverride != nil {
		return *officialOverride
	}
	s := Source{Name: Official, URL: OfficialURL, Official: true}
	sc := bufio.NewScanner(bytes.NewReader(officialKeys))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, err := ParseKey(line); err == nil {
			s.Keys = append(s.Keys, k)
		}
	}
	return s
}

// ParseSource reads a store.d/<name>.source file:
//
//	url=https://example.org/catalogue/
//	key=cirrusstore-key v1 example AAAA…
//
// key may be given more than once, for a key being replaced.
func ParseSource(name string, b []byte) (Source, error) {
	if !ValidName(name) || name == Official || name == BuiltIn {
		return Source{}, fmt.Errorf("source name %q: lowercase letters, digits and -, and not %s", name, Official)
	}
	s := Source{Name: name}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		switch strings.TrimSpace(k) {
		case "url":
			u, err := CheckURL(strings.TrimSpace(v))
			if err != nil {
				return s, fmt.Errorf("line %d: %v", n, err)
			}
			s.URL = u
		case "key":
			key, err := ParseKey(v)
			if err != nil {
				return s, fmt.Errorf("line %d: %v", n, err)
			}
			s.Keys = append(s.Keys, key)
		default:
			return s, fmt.Errorf("line %d: unknown setting %q", n, k)
		}
	}
	if err := sc.Err(); err != nil {
		return s, err
	}
	if s.URL == "" || len(s.Keys) == 0 {
		return s, errors.New("a source needs a url and at least one key")
	}
	return s, nil
}

// CheckURL accepts an https URL with a host and no query, credentials or
// fragment, and returns it ending in /.
func CheckURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("url %q: https://host/path, without a query or credentials", raw)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

// Info is a source as the panel sees it.
type Info struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Keys         []string `json:"keys"`         // key lines, for the gateway to check with
	Fingerprints []string `json:"fingerprints"` // for the owner to compare
	Official     bool     `json:"official,omitempty"`
	// Error says why the server can't use this source (a file it refused,
	// an official source without a key yet).
	Error string `json:"error,omitempty"`
}

// Info describes the source for the snapshot.
func (s Source) Info() Info {
	in := Info{Name: s.Name, URL: s.URL, Official: s.Official, Keys: []string{}, Fingerprints: []string{}}
	if len(s.Keys) == 0 {
		in.Error = "no signing key yet"
	}
	for _, k := range s.Keys {
		in.Keys = append(in.Keys, k.String())
		in.Fingerprints = append(in.Fingerprints, k.Fingerprint())
	}
	return in
}

// FromInfo turns a snapshot's source back into one to verify with. The URL
// is checked again; keys that don't parse are left out. The official source
// ignores the snapshot's URL and keys and uses the embedded ones; with none
// embedded yet it has no keys and so can't be fetched.
func FromInfo(in Info) (Source, error) {
	if !ValidName(in.Name) {
		return Source{}, fmt.Errorf("source name %q", in.Name)
	}
	// The official source is the one built into this binary: its URL and
	// keys come from here, whatever the snapshot says.
	if in.Name == Official {
		return OfficialSource(), nil
	}
	u, err := CheckURL(in.URL)
	if err != nil {
		return Source{}, err
	}
	s := Source{Name: in.Name, URL: u}
	for _, line := range in.Keys {
		if k, err := ParseKey(line); err == nil {
			s.Keys = append(s.Keys, k)
		}
	}
	return s, nil
}
