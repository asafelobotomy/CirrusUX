package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/snapshot"
)

// Phase 2 actions. The gateway never connects to the server: the agent asks for
// work over its own outbound connection (a long poll on /agent/commands),
// runs it, and reports back on /agent/results. the server keeps no listening
// port, and the idle check doesn't count outbound connections.

// ActionReq names one action and its arguments, from a fixed list.
type ActionReq struct {
	Action string            `json:"action"`
	Args   map[string]string `json:"args,omitempty"`
}

type actionSpec struct {
	label func(args map[string]string) string
	check func(args map[string]string, snap *snapshot.Snapshot) error
	undo  func(args map[string]string) *ActionReq
	// Read-only actions (reports) don't go in the audit log or changelog.
	readOnly bool
}

var stayLabels = map[string]string{"30": "for 30 min", "120": "for 2 h", "forever": "until allowed to sleep"}

var actionSpecs = map[string]actionSpec{
	"stay_awake": {
		label: func(a map[string]string) string { return "Kept {server} awake " + stayLabels[a["minutes"]] },
		check: func(a map[string]string, _ *snapshot.Snapshot) error {
			if _, ok := stayLabels[a["minutes"]]; !ok {
				return fmt.Errorf("minutes must be 30, 120 or forever")
			}
			return nil
		},
		undo: func(map[string]string) *ActionReq { return &ActionReq{Action: "allow_sleep"} },
	},
	"allow_sleep": {
		label: func(map[string]string) string { return "Allowed {server} to sleep" },
	},
	"container_start": {
		label: func(a map[string]string) string { return "Started " + a["name"] },
		check: checkContainer,
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "container_stop", Args: map[string]string{"name": a["name"]}}
		},
	},
	"container_stop": {
		label: func(a map[string]string) string { return "Stopped " + a["name"] },
		check: checkContainer,
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "container_start", Args: map[string]string{"name": a["name"]}}
		},
	},
	"container_restart": {
		label: func(a map[string]string) string { return "Restarted " + a["name"] },
		check: checkContainer,
	},
	"backup_run": {
		label: func(map[string]string) string { return "Started a system backup" },
	},
	// The app store (Phase 5b). The gateway checks the app and the shape of
	// the options; the helper checks everything again against the server.
	"app_install": {
		label: func(a map[string]string) string { return "Started installing " + appName(a["app"]) },
		check: checkInstall,
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "app_remove", Args: map[string]string{"app": a["app"]}}
		},
	},
	"app_update": {
		label: func(a map[string]string) string { return "Started updating " + appName(a["app"]) + " to " + a["tag"] },
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			if err := checkInstalled(a, snap); err != nil {
				return err
			}
			if !catalogue.ValidPin(a["tag"], a["digest"]) {
				return errBadPin
			}
			return nil
		},
	},
	"app_remove": {
		label: func(a map[string]string) string {
			return "Started removing " + appName(a["app"]) + " (its folder is archived)"
		},
		check: checkInstalled,
	},
	"egress_report": {
		label:    func(map[string]string) string { return "Egress report" },
		readOnly: true,
	},
}

// catalogueApps is the built-in catalogue, loaded once.
var catalogueApps = func() []catalogue.App {
	apps, err := catalogue.Load()
	if err != nil {
		panic("built-in catalogue: " + err.Error())
	}
	return apps
}()

func findApp(id string) (catalogue.App, bool) {
	for _, a := range catalogueApps {
		if a.ID == id {
			return a, true
		}
	}
	return catalogue.App{}, false
}

func appName(id string) string {
	if a, ok := findApp(id); ok {
		return a.Name
	}
	return id
}

func checkInstall(a map[string]string, snap *snapshot.Snapshot) error {
	if _, ok := findApp(a["app"]); !ok {
		return fmt.Errorf("no app %q in the catalogue", a["app"])
	}
	if len(a["options"]) > 4096 {
		return fmt.Errorf("options too long")
	}
	if a["options"] != "" {
		if _, err := catalogue.ParseOptions(a["options"]); err != nil {
			return fmt.Errorf("options: %v", err)
		}
	}
	if snap != nil && slices.ContainsFunc(snap.Apps, func(x snapshot.App) bool { return x.ID == a["app"] }) {
		return fmt.Errorf("%s is already installed", appName(a["app"]))
	}
	return nil
}

// previewApp renders the file an install would write, with the facts the
// server reported: the same code and inputs as the job itself. A problem
// with the options comes back as a message to show, not an HTTP error.
func (s *Server) previewApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		App     string            `json:"app"`
		Options catalogue.Options `json:"options"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	app, ok := findApp(body.App)
	if !ok {
		http.Error(w, "no such app", http.StatusNotFound)
		return
	}
	snap := s.store.Snapshot()
	if snap == nil || snap.AppsHost == nil {
		http.Error(w, s.cfg.ServerName+" hasn't reported its app store settings yet (the apps module)", http.StatusConflict)
		return
	}
	hi := snap.AppsHost
	host := catalogue.Host{AppsDir: hi.AppsDir, TZ: hi.TZ, UID: hi.UID, GID: hi.GID, Groups: hi.Groups, MediaRoots: hi.MediaRoots, BindAddress: hi.Bind}
	out := map[string]any{"runtime": hi.Runtime, "mediaRoots": hi.MediaRoots}
	p, err := catalogue.Resolve(app, body.Options, host)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	if hi.Runtime == "podman" {
		out["file"], out["content"] = "/etc/containers/systemd/"+p.QuadletName(), p.Quadlet()
	} else {
		out["file"], out["content"] = hi.AppsDir+"/"+app.ID+"/compose.yml", p.Compose()
	}
	// Ports another container already publishes: the job would refuse them.
	var clash []string
	for _, pm := range p.Ports {
		for _, c := range snap.Containers {
			if slices.Contains(c.Ports, pm.Host) {
				clash = append(clash, fmt.Sprintf("port %d is already used by %s", pm.Host, c.Name))
			}
		}
	}
	out["warnings"] = clash
	writeJSON(w, out)
}

func checkInstalled(a map[string]string, snap *snapshot.Snapshot) error {
	if snap == nil || !slices.ContainsFunc(snap.Apps, func(x snapshot.App) bool { return x.ID == a["app"] }) {
		return fmt.Errorf("%s isn't installed by the app store", appName(a["app"]))
	}
	return nil
}

// checkContainer only accepts containers the agent last reported.
func checkContainer(a map[string]string, snap *snapshot.Snapshot) error {
	if snap == nil || !slices.ContainsFunc(snap.Containers, func(c snapshot.Container) bool { return c.Name == a["name"] }) {
		return fmt.Errorf("unknown container %q", a["name"])
	}
	return nil
}

// Validate checks an action and its arguments, dropping any argument the
// action doesn't use.
func (q ActionReq) Validate(snap *snapshot.Snapshot) (ActionReq, actionSpec, error) {
	spec, ok := actionSpecs[q.Action]
	if !ok {
		return q, spec, fmt.Errorf("unknown action %q", q.Action)
	}
	clean := ActionReq{Action: q.Action}
	for _, k := range []string{"minutes", "name", "app", "options", "tag", "digest"} {
		if v, ok := q.Args[k]; ok {
			if clean.Args == nil {
				clean.Args = map[string]string{}
			}
			clean.Args[k] = v
		}
	}
	if spec.check != nil {
		if err := spec.check(clean.Args, snap); err != nil {
			return clean, spec, err
		}
	}
	return clean, spec, nil
}

// Command is an action on its way to the agent.
type Command struct {
	ID string `json:"id"`
	ActionReq
	Label    string     `json:"label"`
	Who      string     `json:"who"`
	Created  time.Time  `json:"created"`
	State    string     `json:"state"` // pending, sent, done, failed
	Output   []string   `json:"output,omitempty"`
	Error    string     `json:"error,omitempty"`
	Undo     *ActionReq `json:"undo,omitempty"`
	ReadOnly bool       `json:"-"`
	sent     time.Time
}

const (
	pollWait    = 25 * time.Second
	pendingMax  = 2 * time.Minute // agent never asked: server asleep or agent down
	sentMax     = 4 * time.Minute // agent took it but didn't report (egress report: up to 3 min)
	keepHistory = 50
)

// Queue holds commands in memory. A gateway restart drops pending ones,
// which then show as failed in the UI; nothing runs twice.
type Queue struct {
	mu     sync.Mutex
	cmds   []*Command
	notify chan struct{}
}

func NewQueue() *Queue { return &Queue{notify: make(chan struct{}, 1)} }

func (q *Queue) Add(c *Command) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	c.ID = hex.EncodeToString(b)
	c.State, c.Created = "pending", time.Now()
	q.mu.Lock()
	q.cmds = append(q.cmds, c)
	if len(q.cmds) > keepHistory {
		q.cmds = q.cmds[len(q.cmds)-keepHistory:]
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *Queue) Get(id string) (Command, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.expire(time.Now())
	for _, c := range q.cmds {
		if c.ID == id {
			return *c, true
		}
	}
	return Command{}, false
}

// Next waits up to pollWait for a pending command and marks it sent.
func (q *Queue) Next(ctx context.Context) *Command {
	timer := time.NewTimer(pollWait)
	defer timer.Stop()
	for {
		q.mu.Lock()
		q.expire(time.Now())
		for _, c := range q.cmds {
			if c.State == "pending" {
				c.State, c.sent = "sent", time.Now()
				cc := *c
				q.mu.Unlock()
				return &cc
			}
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return nil
		case <-q.notify:
		}
	}
}

// Complete records the agent's result. It returns the finished command, or
// false if it was unknown or had already expired.
func (q *Queue) Complete(id string, ok bool, output []string, errMsg string) (Command, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, c := range q.cmds {
		if c.ID == id && c.State == "sent" {
			c.Output, c.Error = output, errMsg
			c.State = "done"
			if !ok {
				c.State = "failed"
			}
			return *c, true
		}
	}
	return Command{}, false
}

func (q *Queue) expire(now time.Time) {
	for _, c := range q.cmds {
		switch {
		case c.State == "pending" && now.Sub(c.Created) > pendingMax:
			c.State, c.Error = "failed", "the server didn't pick it up (asleep, or the agent isn't running)"
		case c.State == "sent" && now.Sub(c.sent) > sentMax:
			c.State, c.Error = "failed", "the server took it but didn't report back"
		}
	}
}

// label is the action's audit text with the server's name filled in.
func (s *Server) label(spec actionSpec, args map[string]string) string {
	return strings.ReplaceAll(spec.label(args), "{server}", s.cfg.ServerName)
}

// ---- HTTP ----

func (s *Server) postAction(w http.ResponseWriter, r *http.Request) {
	var req ActionReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	snap := s.store.Snapshot()
	clean, spec, err := req.Validate(snap)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := s.power.View(time.Now(), snap); v.State != "awake" {
		http.Error(w, s.cfg.ServerName+" isn't awake. Wake it first.", http.StatusConflict)
		return
	}
	c := &Command{ActionReq: clean, Label: s.label(spec, clean.Args), Who: userOf(r), ReadOnly: spec.readOnly}
	if spec.undo != nil {
		c.Undo = spec.undo(clean.Args)
	}
	if clean.Action == "app_update" {
		// Only versions the last check found, or back to the previous one.
		if err := s.checkUpdate(clean.Args, snap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Its undo goes back to what runs now.
		for _, x := range snap.Apps {
			if x.ID == clean.Args["app"] && catalogue.ValidPin(x.Tag, x.Digest) {
				c.Undo = &ActionReq{Action: "app_update", Args: map[string]string{"app": x.ID, "tag": x.Tag, "digest": x.Digest}}
			}
		}
	}
	s.queue.Add(c)
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"id": c.ID})
}

func (s *Server) getAction(w http.ResponseWriter, r *http.Request) {
	c, ok := s.queue.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such action", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

// agentCommands is the agent's long poll: one command, or 204 after ~25 s.
func (s *Server) agentCommands(w http.ResponseWriter, r *http.Request) {
	c := s.queue.Next(r.Context())
	if c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, map[string]any{"id": c.ID, "action": c.Action, "args": c.Args})
}

func (s *Server) agentResults(w http.ResponseWriter, r *http.Request) {
	var res struct {
		ID     string   `json:"id"`
		OK     bool     `json:"ok"`
		Output []string `json:"output"`
		Error  string   `json:"error"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&res); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	c, ok := s.queue.Complete(res.ID, res.OK, res.Output, res.Error)
	if !ok {
		http.Error(w, "unknown or expired command", http.StatusNotFound)
		return
	}
	if !c.ReadOnly {
		e := AuditEntry{Who: c.Who, What: c.Label, UndoAction: c.Undo}
		if c.Undo != nil {
			e.Undo = s.label(actionSpecs[c.Undo.Action], c.Undo.Args)
		}
		if c.State == "failed" {
			e.What += " (failed)"
			e.Result = c.Error
			e.Undo, e.UndoAction = "", nil
		}
		s.record(e)
	}
	w.WriteHeader(http.StatusNoContent)
}
