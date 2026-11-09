package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/store"
	"cirrusux/internal/version"
)

// A catalogue folder, as it is served:
//
//	index.json, index.json.sig
//	apps/<id>.json, apps/<id>.json.sig

// signApps wraps each definition in an envelope in dir/apps and signs it.
// An app keeps its serial while its definition is unchanged (re-signing
// only moves the expiry); a changed one gets the next serial.
func signApps(dir string, sec store.Secret, source string, expires time.Time, defs []string) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(dir, "apps"), 0o755); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range defs {
		raw, err := os.ReadFile(p)
		if err != nil {
			return out, err
		}
		_, app, err := store.SealApp(source, 1, expires, raw)
		if err != nil {
			return out, fmt.Errorf("%s: %v", p, err)
		}
		target := filepath.Join(dir, "apps", app.ID+".json")
		serial := int64(1)
		if prev, err := readEnvelope(target); err == nil {
			serial = prev.Serial
			if !sameJSON(prev.App, raw) {
				serial++
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return out, fmt.Errorf("%s: %v", target, err)
		}
		b, _, err := store.SealApp(source, serial, expires, raw)
		if err != nil {
			return out, err
		}
		if err := writeSigned(target, b, sec); err != nil {
			return out, err
		}
		out = append(out, fmt.Sprintf("%s serial %d", app.ID, serial))
	}
	return out, nil
}

// buildIndex lists every signed app in dir/apps (each checked with sec's
// own public key) in a new index, one serial up from the last.
func buildIndex(dir string, sec store.Secret, source string, expires, now time.Time) (store.Index, error) {
	src := store.Source{Name: source, Keys: []store.Key{sec.Public()}}
	ix := store.Index{Source: source, Serial: 1, Expires: expires.UTC(), Apps: []store.Entry{}}
	if prev, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil {
		var old store.Index
		if json.Unmarshal(prev, &old) == nil {
			ix.Serial = old.Serial + 1
		}
	}
	apps, err := openApps(dir, src, now)
	if err != nil {
		return ix, err
	}
	for _, a := range apps {
		ix.Apps = append(ix.Apps, store.Entry{ID: a.Def.ID, Serial: a.Serial, Name: a.Def.Name, Group: a.Def.Group, Icon: a.Def.Icon, MinVersion: a.Def.MinVersion})
	}
	b, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return ix, err
	}
	return ix, writeSigned(filepath.Join(dir, "index.json"), append(b, '\n'), sec)
}

// verifyDir checks a catalogue folder the way a server will: the index and
// every app against src's keys, and that the index lists each app at the
// serial its envelope has.
func verifyDir(dir string, src store.Source, now time.Time) (store.Index, error) {
	f, err := readSigned(filepath.Join(dir, "index.json"))
	if err != nil {
		return store.Index{}, err
	}
	ix, err := src.OpenIndex(f, now, 0)
	if err != nil {
		return ix, err
	}
	apps, err := openApps(dir, src, now)
	if err != nil {
		return ix, err
	}
	have := map[string]int64{}
	for _, a := range apps {
		have[a.Def.ID] = a.Serial
	}
	for _, e := range ix.Apps {
		s, ok := have[e.ID]
		switch {
		case !ok:
			return ix, fmt.Errorf("index lists %s, but apps/%s.json is missing", e.ID, e.ID)
		case s != e.Serial:
			return ix, fmt.Errorf("index lists %s at serial %d, its envelope has %d: run index again", e.ID, e.Serial, s)
		}
		delete(have, e.ID)
	}
	for id := range have {
		return ix, fmt.Errorf("apps/%s.json isn't in the index: run index again", id)
	}
	return ix, nil
}

func openApps(dir string, src store.Source, now time.Time) ([]store.App, error) {
	names, err := filepath.Glob(filepath.Join(dir, "apps", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var apps []store.App
	for _, n := range names {
		f, err := readSigned(n)
		if err != nil {
			return nil, err
		}
		id := filepath.Base(n)
		a, err := src.OpenApp(id[:len(id)-len(".json")], f, now, 0)
		if need, ok := catalogue.AsNeedsNewer(err); ok {
			return nil, fmt.Errorf("%s: needs CirrusUX %s, but this cirrusstore is %s: use a newer cirrusstore", id, need.Min, version.String())
		}
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}

func readSigned(p string) (store.Signed, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return store.Signed{}, err
	}
	sig, err := os.ReadFile(p + ".sig")
	if err != nil {
		return store.Signed{}, err
	}
	return store.Signed{Data: data, Sig: string(sig)}, nil
}

func readEnvelope(p string) (store.Envelope, error) {
	var env store.Envelope
	b, err := os.ReadFile(p)
	if err != nil {
		return env, err
	}
	return env, json.Unmarshal(b, &env)
}

func writeSigned(p string, data []byte, sec store.Secret) error {
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(p+".sig", sec.Sign(data), 0o644)
}

// sameJSON compares two JSON documents ignoring layout and escaping.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
