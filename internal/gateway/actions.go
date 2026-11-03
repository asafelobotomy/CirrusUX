package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/storage"
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
	// OS updates (Phase 6b): a check reads; an upgrade changes the system
	// and can't be undone, which its confirmation says.
	"os_check": {
		label:    func(map[string]string) string { return "Checked for OS updates" },
		readOnly: true,
	},
	"os_upgrade": {
		label: func(map[string]string) string { return "Started installing OS package updates" },
	},
	// Storage (Phase 6c): measuring reads; deleting from the archive is the
	// only deletion in CirrusUX. It has no undo, and it takes the entry's
	// name twice: once picked, once typed.
	"storage_scan": {
		label:    func(map[string]string) string { return "Measured the storage" },
		readOnly: true,
	},
	"archive_delete": {
		label: func(a map[string]string) string { return "Deleted " + a["name"] + " from the archive" },
		check: checkArchiveDelete,
	},
	// Logs (Phase 6b): one recent window, never followed.
	"logs": {
		label:    func(a map[string]string) string { return "Read logs of " + a["source"] },
		check:    checkLogSource,
		readOnly: true,
	},
	"egress_report": {
		label:    func(map[string]string) string { return "Egress report" },
		readOnly: true,
	},
}

func checkArchiveDelete(a map[string]string, snap *snapshot.Snapshot) error {
	if snap == nil || snap.Storage == nil || !slices.ContainsFunc(snap.Storage.Archive, func(e storage.Entry) bool { return e.Name == a["name"] }) {
		return fmt.Errorf("%q isn't in the archive the server last reported", a["name"])
	}
	if a["confirm"] != a["name"] {
		return fmt.Errorf("type the name exactly to confirm")
	}
	return nil
}

// checkLogSource accepts a container the agent reported, or one of
// CirrusUX's own units.
func checkLogSource(a map[string]string, snap *snapshot.Snapshot) error {
	if n, err := strconv.Atoi(a["lines"]); a["lines"] != "" && (err != nil || n < 1 || n > 1000) {
		return fmt.Errorf("lines: 1 to 1000")
	}
	kind, name, _ := strings.Cut(a["source"], ":")
	switch kind {
	case "container":
		return checkContainer(map[string]string{"name": name}, snap)
	case "unit":
		if helperapi.UnitRe.MatchString(name) {
			return nil
		}
	}
	return fmt.Errorf("logs come from a container or a CirrusUX unit (cirrusux-….service)")
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
	for _, k := range []string{"minutes", "name", "app", "options", "tag", "digest", "source", "lines", "confirm"} {
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
