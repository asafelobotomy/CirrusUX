// Package appjob installs, updates and removes CirrusStore apps (docs/history/PHASES-4-6
// §5b). It runs as root inside the helper, as a background job. The request
// file comes from the agent, whose run folder (/run/cirrusux-agent) is the
// agent's own, so every request is checked again here, whatever the agent
// sent: against the built-in catalogue, or a signed definition verified
// against the sources root trusts. Each step is written to a status file
// (in a folder only root writes) that the agent reports.
package appjob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/store"
)

// Request is what the agent asks for: one file per job.
type Request struct {
	Job     string            `json:"job"`
	Action  string            `json:"action"` // install, remove or update
	App     string            `json:"app"`
	Options catalogue.Options `json:"options"`
	// Update only: the version to move to, always of the catalogue's own
	// image. Rolling back is an update to the previous version.
	Tag    string `json:"tag,omitempty"`
	Digest string `json:"digest,omitempty"`
	// Source is the CirrusStore source the app comes from ("" or built-in
	// for the built-in catalogue). Definition is its signed envelope, for an
	// install or a definition update; built-in apps have none.
	Source     string        `json:"source,omitempty"`
	Definition *store.Signed `json:"definition,omitempty"`
}

// ParseRequest reads a request strictly: unknown fields and actions refused.
func ParseRequest(b []byte) (Request, error) {
	var r Request
	if len(b) > 16<<10 {
		return r, errors.New("request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, err
	}
	if !helperapi.JobRe.MatchString(r.Job) {
		return r, fmt.Errorf("bad job id %q", r.Job)
	}
	if !store.ValidName(r.App) {
		return r, fmt.Errorf("bad app id %q", r.App)
	}
	if r.Source != "" && r.Source != store.BuiltIn && !store.ValidName(r.Source) {
		return r, fmt.Errorf("bad source %q", r.Source)
	}
	if r.Definition != nil && (r.Source == "" || r.Source == store.BuiltIn || r.Action == "remove") {
		return r, errors.New("a signed definition comes with an install or update from a CirrusStore source")
	}
	switch r.Action {
	case "install", "remove":
		if r.Tag != "" || r.Digest != "" {
			return r, fmt.Errorf("%s takes no version", r.Action)
		}
	case "update":
		if !catalogue.ValidPin(r.Tag, r.Digest) {
			return r, fmt.Errorf("update needs an exact tag (never latest) and a sha256 digest")
		}
	default:
		return r, fmt.Errorf("unknown action %q", r.Action)
	}
	return r, nil
}

// Runtime starts and stops an app on Docker or Podman.
type Runtime interface {
	Name() string
	Pull(ctx context.Context, ref string, progress func(string)) error
	Up(ctx context.Context, p catalogue.Plan, appDir string) error
	Down(ctx context.Context, appID, appDir string) error
	// RemoveImage deletes a downloaded image; it fails if a container
	// still uses it.
	RemoveImage(ctx context.Context, ref string) error
}

// Env is the server: where things go, and how to ask it things.
type Env struct {
	Runtime    Runtime
	Host       catalogue.Host // AppsDir, TZ, apps user, groups, media roots
	ArchiveDir string         // removed apps' folders go here
	RecordsDir string         // installed apps
	StatusDir  string         // job status files (readable by the agent)
	Catalogue  []catalogue.App
	// Sources are the CirrusStore sources root trusts, for signed apps.
	Sources []store.Source
	Now     func() time.Time
	// PortFree reports whether nothing listens on the host port yet.
	PortFree func(port int, proto string) bool
	// Healthy checks the app's health URL once.
	Healthy       func(ctx context.Context, url string) error
	HealthTimeout time.Duration
	// Chown gives the app's folders to the apps user; nil skips (tests).
	Chown func(path string, uid, gid int) error
	// Copy copies a folder with owners and modes; nil uses CopyTree.
	Copy func(src, dst string) error
}

// Check validates a request fully, without changing anything: the helper
// runs it before starting the background job, so a bad request fails at
// once with a clear error.
func (e *Env) Check(r Request) (catalogue.App, error) {
	app, _, err := e.check(r)
	return app, err
}

func (e *Env) check(r Request) (catalogue.App, origin, error) {
	app, o, err := e.resolve(r)
	if err != nil {
		return app, o, err
	}
	installed := e.installed(r.App)
	switch r.Action {
	case "install":
		if installed {
			return app, o, fmt.Errorf("%s is already installed", app.Name)
		}
		p, err := catalogue.Resolve(app, r.Options, e.Host)
		if err != nil {
			return app, o, err
		}
		for _, pm := range p.Ports {
			if !e.PortFree(pm.Host, pm.Protocol) {
				return app, o, fmt.Errorf("port %d/%s is already in use on the server", pm.Host, pm.Protocol)
			}
		}
		for _, m := range p.Mounts {
			if m.ReadOnly || !strings.HasPrefix(m.Host, e.Host.AppsDir+"/") {
				if fi, err := os.Stat(m.Host); err != nil || !fi.IsDir() {
					return app, o, fmt.Errorf("the folder %s doesn't exist on the server", m.Host)
				}
			}
		}
	case "remove", "update":
		if !installed {
			return app, o, fmt.Errorf("%s isn't installed from CirrusStore", app.Name)
		}
		if !emptyOptions(r.Options) {
			return app, o, fmt.Errorf("%s takes no options", r.Action)
		}
		if r.Action == "update" {
			rec, err := e.record(app.ID)
			if err != nil {
				return app, o, err
			}
			if r.Definition != nil {
				if app.Tag != r.Tag || app.Digest != r.Digest {
					return app, o, fmt.Errorf("the definition pins %s, not %s", app.Tag, r.Tag)
				}
				if o.Source == rec.Source && o.Serial == rec.Serial {
					return app, o, fmt.Errorf("%s already has that definition", app.Name)
				}
			} else if rec.Tag == r.Tag && (rec.Digest == "" || rec.Digest == r.Digest) {
				return app, o, fmt.Errorf("%s is already at %s", app.Name, r.Tag)
			}
		}
	}
	return app, o, nil
}

// Run carries out a checked request, writing its status as it goes.
func (e *Env) Run(ctx context.Context, r Request) error {
	// One job per app at a time: a second one waits, then is checked
	// against what the first left (removed, or already on that version).
	if store.ValidName(r.App) { // an app id: safe in a path
		if unlock, err := e.lockApp(r.App); err == nil {
			defer unlock()
		}
	}
	app, o, err := e.check(r)
	st := Status{Job: r.Job, Action: r.Action, App: r.App, Name: app.Name, State: "running", Started: e.Now()}
	if err != nil {
		return e.finish(&st, err)
	}
	switch r.Action {
	case "remove":
		return e.finish(&st, e.remove(ctx, &st, app))
	case "update":
		return e.finish(&st, e.update(ctx, &st, app, r.Tag, r.Digest, o))
	}
	return e.finish(&st, e.install(ctx, &st, app, r.Options, o))
}

// Refuse records that a request failed its check, without waiting for the
// app's lock: the request never runs, and another job of the same app may
// hold the lock for a long time.
func (e *Env) Refuse(r Request, reason error) error {
	app, _ := e.find(r.App)
	st := Status{Job: r.Job, Action: r.Action, App: r.App, Name: app.Name, Started: e.Now()}
	return e.finish(&st, reason)
}

func (e *Env) install(ctx context.Context, st *Status, app catalogue.App, o catalogue.Options, from origin) error {
	p, err := catalogue.Resolve(app, o, e.Host)
	if err != nil {
		return err
	}
	appDir := filepath.Join(e.Host.AppsDir, app.ID)

	e.step(st, "Creating the app's folders", "")
	for _, m := range p.Mounts {
		if !strings.HasPrefix(m.Host, appDir+"/") {
			continue // the owner's media folders: never created or changed
		}
		if err := os.MkdirAll(m.Host, 0o750); err != nil {
			return err
		}
		if e.Chown != nil {
			if err := e.Chown(m.Host, e.Host.UID, e.Host.GID); err != nil {
				return err
			}
		}
	}

	e.step(st, "Downloading "+app.Image+":"+app.Tag, "")
	if err := e.Runtime.Pull(ctx, app.ImageRef(), func(line string) { e.step(st, st.Step, line) }); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	e.step(st, "Starting "+app.Name, "")
	if err := e.Runtime.Up(ctx, p, appDir); err != nil {
		_ = e.Runtime.Down(context.WithoutCancel(ctx), app.ID, appDir)
		return fmt.Errorf("start: %w", err)
	}

	if url := e.healthURL(app, p); url != "" {
		e.step(st, "Waiting for "+app.Name+" to answer", url)
		if err := e.waitHealthy(ctx, url); err != nil {
			_ = e.Runtime.Down(context.WithoutCancel(ctx), app.ID, appDir)
			return fmt.Errorf("%s didn't answer on %s within %s: %w; stopped it again", app.Name, url, e.HealthTimeout, err)
		}
	}

	e.step(st, "Recording the install", "")
	rec := Record{App: app.ID, Name: app.Name, Tag: app.Tag, Digest: app.Digest, Runtime: e.Runtime.Name(), MainPort: hostPortOf(p, app.MainPort()), HTTPS: app.MainPort().HTTPS, Options: o, Installed: e.Now(),
		Source: from.Source, Serial: from.Serial, Definition: from.Signed}
	return fsutil.WriteJSON(filepath.Join(e.RecordsDir, app.ID+".json"), rec, 0o644)
}

func (e *Env) step(st *Status, step, detail string) {
	st.Step, st.Detail = step, detail
	_ = e.writeStatus(st)
}

func (e *Env) finish(st *Status, err error) error {
	t := e.Now()
	st.Finished = &t
	st.State = "done"
	if err != nil {
		st.State, st.Error = "failed", err.Error()
	} else {
		st.Step = st.Name + map[string]string{"install": " is installed", "update": " is updated", "remove": " is removed"}[st.Action]
	}
	if werr := e.writeStatus(st); werr != nil && err == nil {
		err = werr
	}
	return err
}

func (e *Env) find(id string) (catalogue.App, bool) {
	for _, a := range e.Catalogue {
		if a.ID == id {
			return a, true
		}
	}
	return catalogue.App{}, false
}

func (e *Env) installed(id string) bool {
	_, err := os.Stat(filepath.Join(e.RecordsDir, id+".json"))
	return err == nil
}

func emptyOptions(o catalogue.Options) bool {
	return len(o.Ports) == 0 && len(o.Media) == 0 && len(o.Env) == 0 && len(o.Devices) == 0
}
