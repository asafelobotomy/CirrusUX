package catalogue

import (
	"encoding/json"
	"errors"
	"fmt"

	"cirrusux/internal/version"
)

// Definitions are read strictly, so a field added in a newer CirrusUX would
// make every older one refuse the app with a parse error. A publisher who
// uses such a field sets minVersion instead: older CirrusUX versions read
// that one field first, and say the app needs a newer version.

// ErrNeedsNewer: the definition (or index entry) is meant for a newer
// CirrusUX than this one.
type ErrNeedsNewer struct{ Min string }

func (e ErrNeedsNewer) Error() string { return NeedsMessage(e.Min) }

// NeedsMessage is what the person sees when an app needs CirrusUX min.
func NeedsMessage(min string) string {
	return "This app needs CirrusUX " + min + " or newer: run sudo cirrusux-setup upgrade on the server."
}

// AsNeedsNewer reports whether err is, or wraps, an ErrNeedsNewer.
func AsNeedsNewer(err error) (ErrNeedsNewer, bool) {
	var e ErrNeedsNewer
	ok := errors.As(err, &e)
	return e, ok
}

// ValidVersion reports whether s is a plain "X.Y.Z" (digits only).
func ValidVersion(s string) bool {
	if s == "" || s[0] == 'v' {
		return false
	}
	for _, c := range s {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	_, ok := version.ParseNumber(s)
	return ok
}

// NeedsNewer reports whether a minVersion asks for more than this CirrusUX
// (a dev build accepts everything).
func NeedsNewer(min string) bool {
	return min != "" && !version.AtLeast(version.Version, min)
}

// preRead looks at minVersion alone, ignoring every other field, so it
// works on definitions written for versions that know fields this one
// doesn't. The shape is checked here too, because Parse must not trust it.
func preRead(b []byte) error {
	var pre struct {
		MinVersion string `json:"minVersion"`
	}
	if json.Unmarshal(b, &pre) != nil || pre.MinVersion == "" {
		return nil // not readable this way: the strict decode reports it
	}
	if !ValidVersion(pre.MinVersion) {
		return fmt.Errorf("minVersion %q: use X.Y.Z, like 0.8.0", pre.MinVersion)
	}
	if NeedsNewer(pre.MinVersion) {
		return ErrNeedsNewer{Min: pre.MinVersion}
	}
	return nil
}
