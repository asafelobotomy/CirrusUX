package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"cirrusux/internal/snapshot"
)

// Phase 2 actions. The gateway never connects to cloud64: the agent asks for
// work over its own outbound connection (a long poll on /agent/commands),
// runs it, and reports back on /agent/results. cloud64 keeps no listening
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
		label: func(a map[string]string) string { return "Kept cloud64 awake " + stayLabels[a["minutes"]] },
		check: func(a map[string]string, _ *snapshot.Snapshot) error {
			if _, ok := stayLabels[a["minutes"]]; !ok {
				return fmt.Errorf("minutes must be 30, 120 or forever")
			}
			return nil
		},
		undo: func(map[string]string) *ActionReq { return &ActionReq{Action: "allow_sleep"} },
	},
	"allow_sleep": {
		label: func(map[string]string) string { return "Allowed cloud64 to sleep" },
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
	"egress_report": {
		label:    func(map[string]string) string { return "Egress report" },
		readOnly: true,
	},
}

// checkContainer only accepts containers cloud64 last reported.
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
	for _, k := range []string{"minutes", "name"} {
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

// Command is an action on its way to cloud64.
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
	pendingMax  = 2 * time.Minute // agent never asked: cloud64 asleep or agent down
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
			c.State, c.Error = "failed", "cloud64 didn't pick it up (asleep, or the agent isn't running)"
		case c.State == "sent" && now.Sub(c.sent) > sentMax:
			c.State, c.Error = "failed", "cloud64 took it but didn't report back"
		}
	}
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
		http.Error(w, "cloud64 isn't awake. Wake it first.", http.StatusConflict)
		return
	}
	c := &Command{ActionReq: clean, Label: spec.label(clean.Args), Who: userOf(r), ReadOnly: spec.readOnly}
	if spec.undo != nil {
		c.Undo = spec.undo(clean.Args)
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
			e.Undo = actionSpecs[c.Undo.Action].label(c.Undo.Args)
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
