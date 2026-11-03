package appjob

import (
	"fmt"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/store"
)

// origin is where a job's definition comes from: the built-in catalogue
// (Source "") or a signed envelope from a CirrusStore source.
type origin struct {
	Source string
	Serial int64
	Signed *store.Signed
}

// resolve finds the definition a request runs with:
//   - a signed definition it carries, verified here against the sources
//     root trusts, and never older than the one installed from that source;
//   - for a rollback, the definition the previous version ran with;
//   - otherwise the definition the app was installed with, or the built-in
//     catalogue's.
func (e *Env) resolve(r Request) (catalogue.App, origin, error) {
	rec, err := e.record(r.App)
	hasRec := err == nil
	if r.Definition != nil {
		src, ok := e.source(r.Source)
		if !ok {
			return catalogue.App{}, origin{}, fmt.Errorf("%s isn't a CirrusStore source this server trusts", r.Source)
		}
		var min int64
		if hasRec && rec.Source == r.Source {
			min = rec.Serial
		}
		a, err := src.OpenApp(r.App, *r.Definition, e.Now(), min)
		if err != nil {
			return catalogue.App{}, origin{}, err
		}
		return a.Def, origin{Source: r.Source, Serial: a.Serial, Signed: r.Definition}, nil
	}
	if r.Action == "install" && r.Source != "" && r.Source != store.BuiltIn {
		return catalogue.App{}, origin{}, fmt.Errorf("an app from %s needs its signed definition", r.Source)
	}
	if !hasRec {
		return e.builtIn(r.App)
	}
	if r.Action == "update" && rec.PreviousTag != "" && r.Tag == rec.PreviousTag && r.Digest == rec.PreviousDigest {
		return e.recorded(r.App, rec.PreviousSource, rec.PreviousSerial, rec.PreviousDefinition)
	}
	return e.recorded(r.App, rec.Source, rec.Serial, rec.Definition)
}

// recorded is a definition a record kept, or the built-in one if it kept
// none (the app came from the built-in catalogue).
func (e *Env) recorded(id, source string, serial int64, f *store.Signed) (catalogue.App, origin, error) {
	if f == nil {
		return e.builtIn(id)
	}
	a, err := store.Recorded(id, *f)
	return a.Def, origin{Source: source, Serial: serial, Signed: f}, err
}

func (e *Env) builtIn(id string) (catalogue.App, origin, error) {
	app, ok := e.find(id)
	if !ok {
		return app, origin{}, fmt.Errorf("no app %q in the catalogue", id)
	}
	return app, origin{}, nil
}

func (e *Env) source(name string) (store.Source, bool) {
	for _, s := range e.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return store.Source{}, false
}

// installedApp is the definition an installed app runs with now.
func (e *Env) installedApp(rec Record) (catalogue.App, error) {
	app, _, err := e.recorded(rec.App, rec.Source, rec.Serial, rec.Definition)
	return app, err
}
