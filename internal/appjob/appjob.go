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
	"regexp"
	"strings"
	"time"

	"cirrusux/internal/catalogue"
)

// Request is what the agent asks for: one file per job.
type Request struct {
	Job     string            `json:"job"`
	Action  string            `json:"action"` // install or remove
	App     string            `json:"app"`
	Options catalogue.Options `json:"options"`
}

var jobRe = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

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
	if !jobRe.MatchString(r.Job) {
		return r, fmt.Errorf("bad job id %q", r.Job)
	}
	if r.Action != "install" && r.Action != "remove" {
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
	App       string            `json:"app"`
	Name      string            `json:"name"`
	Tag       string            `json:"tag"`
	Runtime   string            `json:"runtime"`
	MainPort  int               `json:"mainPort"`
	HTTPS     bool              `json:"https,omitempty"`
	Options   catalogue.Options `json:"options"`
	Installed time.Time         `json:"installed"`
}

// Runtime starts and stops an app on Docker or Podman.
type Runtime interface {
	Name() string
	Pull(ctx context.Context, ref string, progress func(string)) error
	Up(ctx context.Context, p catalogue.Plan, appDir string) error
	Down(ctx context.Context, appID, appDir string) error
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
	case "remove":
		if !installed {
			return app, fmt.Errorf("%s isn't installed by the app store", app.Name)
		}
		if !emptyOptions(r.Options) {
			return app, errors.New("remove takes no options")
		}
	}
	return app, nil
}

// Run carries out a checked request, writing its status as it goes.
func (e *Env) Run(ctx context.Context, r Request) error {
	app, err := e.Check(r)
	st := Status{Job: r.Job, Action: r.Action, App: r.App, Name: app.Name, State: "running", Started: e.Now()}
	if err != nil {
		return e.finish(&st, err)
	}
	if r.Action == "remove" {
		return e.finish(&st, e.remove(ctx, &st, app))
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
	rec := Record{App: app.ID, Name: app.Name, Tag: app.Tag, Runtime: e.Runtime.Name(), MainPort: hostPortOf(p, app.MainPort()), HTTPS: app.MainPort().HTTPS, Options: o, Installed: e.Now()}
	return writeJSON(filepath.Join(e.RecordsDir, app.ID+".json"), rec, 0o644)
}

func (e *Env) remove(ctx context.Context, st *Status, app catalogue.App) error {
	appDir := filepath.Join(e.Host.AppsDir, app.ID)
	e.step(st, "Stopping "+app.Name, "")
	if err := e.Runtime.Down(ctx, app.ID, appDir); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	e.step(st, "Archiving its folder", "")
	dest := filepath.Join(e.ArchiveDir, app.ID+"-"+e.Now().Format("20060102-150405"))
	if err := os.MkdirAll(e.ArchiveDir, 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(appDir); err == nil {
		if err := os.Rename(appDir, dest); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
	} else if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	rec := filepath.Join(e.RecordsDir, app.ID+".json")
	if b, err := os.ReadFile(rec); err == nil {
		_ = os.WriteFile(filepath.Join(dest, "cirrusux-record.json"), b, 0o644)
	}
	st.Detail = "archived to " + dest
	return os.Remove(rec)
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
	} else if st.Action == "install" {
		st.Step = st.Name + " is installed"
	} else {
		st.Step = st.Name + " is removed"
	}
	if werr := e.writeStatus(st); werr != nil && err == nil {
		err = werr
	}
	return err
}

func (e *Env) writeStatus(st *Status) error {
	return writeJSON(filepath.Join(e.StatusDir, st.Job+".json"), st, 0o644)
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

// Records lists the installed apps.
func Records(dir string) []Record {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []Record
	for _, f := range files {
		var r Record
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Statuses lists job statuses, newest first.
func Statuses(dir string) []Status {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []Status
	for _, f := range files {
		var s Status
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &s) == nil {
			out = append(out, s)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Started.After(out[j-1].Started); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
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

func writeJSON(path string, v any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
