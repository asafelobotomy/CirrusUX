// Package appjob installs and removes app-store apps (PHASES-4-6 §5b). It
// runs as root inside the helper, as a background job: every request is
// checked again here against the built-in catalogue, whatever the agent
// sent, and each step is written to a status file the agent reports.
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
	"cirrusux/internal/helperapi"
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

// Status is a job's progress, rewritten at every step.
type Status struct {
	Job      string     `json:"job"`
	Action   string     `json:"action"`
	App      string     `json:"app"`
	Name     string     `json:"name"`
	State    string     `json:"state"` // running, done, failed
	Step     string     `json:"step"`
	Detail   string     `json:"detail,omitempty"` // e.g. pull progress
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
}

// Record is an installed app, kept in RecordsDir/<id>.json.
type Record struct {
	App    string `json:"app"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Digest string `json:"digest,omitempty"`
	// The version before the last update, for rolling back.
	PreviousTag    string            `json:"previousTag,omitempty"`
	PreviousDigest string            `json:"previousDigest,omitempty"`
	Updated        time.Time         `json:"updated,omitempty"`
	Runtime        string            `json:"runtime"`
	MainPort       int               `json:"mainPort"`
	HTTPS          bool              `json:"https,omitempty"`
	Options        catalogue.Options `json:"options"`
	Installed      time.Time         `json:"installed"`
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
	Now        func() time.Time
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
	app, ok := e.find(r.App)
	if !ok {
		return app, fmt.Errorf("no app %q in the catalogue", r.App)
	}
	installed := e.installed(r.App)
	switch r.Action {
	case "install":
		if installed {
			return app, fmt.Errorf("%s is already installed", app.Name)
		}
		p, err := catalogue.Resolve(app, r.Options, e.Host)
		if err != nil {
			return app, err
		}
		for _, pm := range p.Ports {
			if !e.PortFree(pm.Host, pm.Protocol) {
				return app, fmt.Errorf("port %d/%s is already in use on the server", pm.Host, pm.Protocol)
			}
		}
		for _, m := range p.Mounts {
			if m.ReadOnly || !strings.HasPrefix(m.Host, e.Host.AppsDir+"/") {
				if fi, err := os.Stat(m.Host); err != nil || !fi.IsDir() {
					return app, fmt.Errorf("the folder %s doesn't exist on the server", m.Host)
				}
			}
		}
	case "remove", "update":
		if !installed {
			return app, fmt.Errorf("%s isn't installed by the app store", app.Name)
		}
		if !emptyOptions(r.Options) {
			return app, fmt.Errorf("%s takes no options", r.Action)
		}
		if r.Action == "update" {
			rec, err := e.record(app.ID)
			if err != nil {
				return app, err
			}
			if rec.Tag == r.Tag && (rec.Digest == "" || rec.Digest == r.Digest) {
				return app, fmt.Errorf("%s is already at %s", app.Name, r.Tag)
			}
		}
	}
	return app, nil
}

// Run carries out a checked request, writing its status as it goes.
func (e *Env) Run(ctx context.Context, r Request) error {
	// One job per app at a time: a second one waits, then is checked
	// against what the first left (removed, or already on that version).
	if _, known := e.find(r.App); known { // a catalogue id: safe in a path
		if unlock, err := e.lockApp(r.App); err == nil {
			defer unlock()
		}
	}
	app, err := e.Check(r)
	st := Status{Job: r.Job, Action: r.Action, App: r.App, Name: app.Name, State: "running", Started: e.Now()}
	if err != nil {
		return e.finish(&st, err)
	}
	switch r.Action {
	case "remove":
		return e.finish(&st, e.remove(ctx, &st, app))
	case "update":
		return e.finish(&st, e.update(ctx, &st, app, r.Tag, r.Digest))
	}
	return e.finish(&st, e.install(ctx, &st, app, r.Options))
}

func (e *Env) install(ctx context.Context, st *Status, app catalogue.App, o catalogue.Options) error {
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

	main := app.MainPort()
	if app.Check != nil {
		for _, port := range app.Ports {
			if port.Name == app.Check.Port {
				main = port
			}
		}
	}
	hostPort := hostPortOf(p, main)
	if app.Check != nil {
		scheme := "http"
		if main.HTTPS {
			scheme = "https"
		}
		url := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, hostPort, app.Check.Path)
		e.step(st, "Waiting for "+app.Name+" to answer", url)
		if err := e.waitHealthy(ctx, url); err != nil {
			_ = e.Runtime.Down(context.WithoutCancel(ctx), app.ID, appDir)
			return fmt.Errorf("%s didn't answer on %s within %s: %w; stopped it again", app.Name, url, e.HealthTimeout, err)
		}
	}

	e.step(st, "Recording the install", "")
	rec := Record{App: app.ID, Name: app.Name, Tag: app.Tag, Digest: app.Digest, Runtime: e.Runtime.Name(), MainPort: hostPortOf(p, app.MainPort()), HTTPS: app.MainPort().HTTPS, Options: o, Installed: e.Now()}
	return writeJSON(filepath.Join(e.RecordsDir, app.ID+".json"), rec, 0o644)
}

func (e *Env) healthURL(app catalogue.App, p catalogue.Plan) string {
	if app.Check == nil {
		return ""
	}
	for _, port := range app.Ports {
		if port.Name == app.Check.Port {
			scheme := "http"
			if port.HTTPS {
				scheme = "https"
			}
			return fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, hostPortOf(p, port), app.Check.Path)
		}
	}
	return ""
}

func (e *Env) waitHealthy(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, e.HealthTimeout)
	defer cancel()
	var last error
	for {
		if last = e.Healthy(ctx, url); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(2 * time.Second):
		}
	}
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

func hostPortOf(p catalogue.Plan, port catalogue.Port) int {
	for _, pm := range p.Ports {
		if pm.Container == port.Container && pm.Protocol == protoOr(port.Protocol) {
			return pm.Host
		}
	}
	return port.Host
}

func protoOr(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}

func emptyOptions(o catalogue.Options) bool {
	return len(o.Ports) == 0 && len(o.Media) == 0 && len(o.Env) == 0 && len(o.Devices) == 0
}
