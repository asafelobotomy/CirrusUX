package store

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"cirrusux/internal/catalogue"
)

// Why a file was refused; wrapped with the details.
var (
	ErrSignature = errors.New("not signed by a key trusted for this source")
	ErrSource    = errors.New("signed for another source")
	ErrExpired   = errors.New("expired")
	ErrReplay    = errors.New("older than a version already seen")
)

// Size limits. An app's envelope travels inside the helper's 16 KB request,
// base64-encoded, so it is kept well under that.
const (
	MaxApp   = 8 << 10
	MaxIndex = 1 << 20
	MaxSig   = 256
)

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

// Envelope is apps/<id>.json: one app definition, signed with the source's
// name, a serial that only goes up, and an expiry date.
type Envelope struct {
	Source  string          `json:"source"`
	Serial  int64           `json:"serial"`
	Expires time.Time       `json:"expires"`
	App     json.RawMessage `json:"app"`
}

// Index is index.json: what a source offers, signed the same way.
type Index struct {
	Source  string    `json:"source"`
	Serial  int64     `json:"serial"`
	Expires time.Time `json:"expires"`
	Apps    []Entry   `json:"apps"`
}

// Entry is one app in an index; Serial is its envelope's.
type Entry struct {
	ID     string `json:"id"`
	Serial int64  `json:"serial"`
	Name   string `json:"name"`
	Group  string `json:"group"`
	Icon   string `json:"icon,omitempty"`
}

// Signed is a file with its .sig, as it travels from the gateway to the
// helper and as an install's record keeps it.
type Signed struct {
	Data []byte `json:"data"`
	Sig  string `json:"sig"`
}

// App is a verified envelope and its checked definition.
type App struct {
	Source  string
	Serial  int64
	Expires time.Time
	Def     catalogue.App
}

// verify checks the signature against every key the source trusts.
func (s Source) verify(data []byte, sigFile string) error {
	if len(sigFile) > MaxSig {
		return fmt.Errorf("%w: signature too long", ErrSignature)
	}
	sig, err := ParseSig([]byte(sigFile))
	if err != nil {
		return err
	}
	for _, k := range s.Keys {
		if ed25519.Verify(k.Pub, data, sig) {
			return nil
		}
	}
	if len(s.Keys) == 0 {
		return fmt.Errorf("%w: %s has no key", ErrSignature, s.Name)
	}
	return fmt.Errorf("%w (%s)", ErrSignature, s.Name)
}

// checkHeader is what an index and an envelope share.
func (s Source) checkHeader(source string, serial int64, expires, now time.Time, minSerial int64) error {
	switch {
	case source != s.Name:
		return fmt.Errorf("%w: says %q, came from %q", ErrSource, source, s.Name)
	case serial < 1:
		return errors.New("serial must be 1 or more")
	case expires.IsZero() || !now.Before(expires):
		return fmt.Errorf("%w on %s", ErrExpired, expires.UTC().Format(time.DateOnly))
	case serial < minSerial:
		return fmt.Errorf("%w: serial %d, have %d", ErrReplay, serial, minSerial)
	}
	return nil
}

// OpenApp verifies an app's envelope from source s: signature first, then
// the source, the expiry and the serial (at least minSerial; 0 for a first
// install), then the definition itself, which must be the app id.
func (s Source) OpenApp(id string, f Signed, now time.Time, minSerial int64) (App, error) {
	if len(f.Data) > MaxApp {
		return App{}, fmt.Errorf("%s: envelope larger than %d bytes", id, MaxApp)
	}
	if err := s.verify(f.Data, f.Sig); err != nil {
		return App{}, fmt.Errorf("%s: %w", id, err)
	}
	var env Envelope
	if err := strict(f.Data, &env); err != nil {
		return App{}, fmt.Errorf("%s: envelope: %v", id, err)
	}
	if err := s.checkHeader(env.Source, env.Serial, env.Expires, now, minSerial); err != nil {
		return App{}, fmt.Errorf("%s: %w", id, err)
	}
	def, err := catalogue.Parse(env.App)
	if err != nil {
		return App{}, fmt.Errorf("%s: %v", id, err)
	}
	if def.ID != id {
		return App{}, fmt.Errorf("%s: the envelope holds %q", id, def.ID)
	}
	return App{Source: env.Source, Serial: env.Serial, Expires: env.Expires, Def: def}, nil
}

// OpenIndex verifies a source's index the same way.
func (s Source) OpenIndex(f Signed, now time.Time, minSerial int64) (Index, error) {
	if len(f.Data) > MaxIndex {
		return Index{}, fmt.Errorf("index larger than %d bytes", MaxIndex)
	}
	if err := s.verify(f.Data, f.Sig); err != nil {
		return Index{}, fmt.Errorf("index: %w", err)
	}
	var ix Index
	if err := strict(f.Data, &ix); err != nil {
		return Index{}, fmt.Errorf("index: %v", err)
	}
	if err := s.checkHeader(ix.Source, ix.Serial, ix.Expires, now, minSerial); err != nil {
		return Index{}, fmt.Errorf("index: %w", err)
	}
	seen := map[string]bool{}
	for _, e := range ix.Apps {
		if !idRe.MatchString(e.ID) || seen[e.ID] || e.Serial < 1 {
			return Index{}, fmt.Errorf("index: app %q: a unique id and a serial", e.ID)
		}
		seen[e.ID] = true
	}
	return ix, nil
}

// SealApp wraps a definition in an envelope for source, ready to sign. The
// definition is checked first, so a publisher can't sign a bad one.
func SealApp(source string, serial int64, expires time.Time, def []byte) ([]byte, catalogue.App, error) {
	app, err := catalogue.Parse(def)
	if err != nil {
		return nil, app, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, def); err != nil {
		return nil, app, err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Envelope{Source: source, Serial: serial, Expires: expires.UTC(), App: compact.Bytes()}); err != nil {
		return nil, app, err
	}
	if out.Len() > MaxApp {
		return nil, app, fmt.Errorf("%s: envelope larger than %d bytes", app.ID, MaxApp)
	}
	return out.Bytes(), app, nil
}

// strict decodes one JSON value, refusing unknown fields and trailing data.
func strict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}
