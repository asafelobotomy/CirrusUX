package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/helperapi"
)

// The gateway keeps every build: what was asked for, its status as the
// agent last sent it, and its log (docs/BUILDS.md). A file per build in
// the data folder's builds/, so a restart keeps them.

// BuildRun is one build of a repository at one commit.
type BuildRun struct {
	ID       string             `json:"id"`
	Repo     string             `json:"repo"`
	Ref      string             `json:"ref"`
	Commit   string             `json:"commit"`
	Subject  string             `json:"subject"`
	Who      string             `json:"who"`
	Created  time.Time          `json:"created"`
	State    string             `json:"state"`
	Steps    []build.StepStatus `json:"steps"`
	Error    string             `json:"error,omitempty"`
	Started  *time.Time         `json:"started,omitempty"`
	Finished *time.Time         `json:"finished,omitempty"`
	LogSize  int64              `json:"logSize"`
	// Updated is when the agent last reported on it.
	Updated time.Time `json:"updated"`
	// Pipeline is what runs; the panel gets the steps instead.
	Pipeline *build.Pipeline `json:"pipeline,omitempty"`
	// CancelSent: a cancel has gone to the server.
	CancelSent bool `json:"cancelSent,omitempty"`
	// Auto: a push started it (builds_auto.go). It waits, undispatched,
	// until the server is awake; DispatchedAt is when the server was told.
	Auto         bool       `json:"auto,omitempty"`
	DispatchedAt *time.Time `json:"dispatchedAt,omitempty"`
}

// view is a build as the panel sees it.
// view is a copy that is safe to hand out: it owns its steps, which the
// store goes on changing, and leaves the pipeline out.
func (r BuildRun) view() BuildRun {
	r.Pipeline = nil
	r.Steps = slices.Clone(r.Steps)
	return r
}

const (
	maxGatewayLog = 8 << 20
	keepBuilds    = 30 // per repository; older finished ones go
	// A queued build the server never started, or a running one it has
	// stopped reporting on, is shown as failed after this.
	buildSilence = 5 * time.Minute
	// A build a push started waits this long for the server to wake.
	autoWait = 24 * time.Hour
)

type buildStore struct {
	dir  string
	mu   sync.Mutex
	runs map[string]*BuildRun
}

var buildIDRe = helperapi.JobRe

func openBuilds(dir string) *buildStore {
	b := &buildStore{dir: dir, runs: map[string]*BuildRun{}}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Error("builds", "err", err)
		return b
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		var r BuildRun
		if err != nil || json.Unmarshal(raw, &r) != nil || !buildIDRe.MatchString(r.ID) {
			slog.Warn("builds: skipping", "file", f)
			continue
		}
		b.runs[r.ID] = &r
	}
	return b
}

func (b *buildStore) save(r *BuildRun) error {
	return fsutil.WriteJSON(filepath.Join(b.dir, r.ID+".json"), r, 0o600)
}

func (b *buildStore) logPath(id string) string { return filepath.Join(b.dir, id+".log") }

// expire shows a build the server has gone quiet on as failed.
func expire(r *BuildRun, now time.Time) bool {
	switch {
	case r.State == build.Queued && r.Auto && r.DispatchedAt == nil:
		if now.Sub(r.Created) <= autoWait {
			return false
		}
		r.Error = "The server stayed asleep for a day, so it never started"
	case r.State == build.Queued && now.Sub(r.Created) > buildSilence && (r.DispatchedAt == nil || now.Sub(*r.DispatchedAt) > buildSilence):
		r.Error = "The server didn't start it (asleep, or the agent isn't running)"
	case r.State == build.Running && now.Sub(r.Updated) > buildSilence:
		r.Error = "The server stopped reporting on it"
	default:
		return false
	}
	r.State, r.Finished = build.Failed, &now
	for i := range r.Steps {
		if !build.Finished(r.Steps[i].State) {
			r.Steps[i].State = build.Skipped
		}
	}
	return true
}

func (b *buildStore) add(r *BuildRun) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.save(r); err != nil {
		return err
	}
	b.runs[r.ID] = r
	b.prune(r.Repo)
	return nil
}

// prune drops a repository's oldest finished builds beyond keepBuilds.
func (b *buildStore) prune(repo string) {
	var mine []*BuildRun
	for _, r := range b.runs {
		if r.Repo == repo {
			mine = append(mine, r)
		}
	}
	slices.SortFunc(mine, func(x, y *BuildRun) int { return y.Created.Compare(x.Created) })
	for _, r := range mine[min(len(mine), keepBuilds):] {
		if build.Finished(r.State) {
			delete(b.runs, r.ID)
			os.Remove(filepath.Join(b.dir, r.ID+".json"))
			os.Remove(b.logPath(r.ID))
		}
	}
}

func (b *buildStore) list(repo string) []BuildRun {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	out := []BuildRun{}
	for _, r := range b.runs {
		if r.Repo != repo {
			continue
		}
		if expire(r, now) {
			_ = b.save(r)
		}
		out = append(out, r.view())
	}
	slices.SortFunc(out, func(x, y BuildRun) int { return y.Created.Compare(x.Created) })
	return out
}

func (b *buildStore) get(id string) (BuildRun, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[id]
	if !ok {
		return BuildRun{}, false
	}
	if expire(r, time.Now()) {
		_ = b.save(r)
	}
	c := *r
	c.Steps = slices.Clone(r.Steps)
	return c, true
}

var errNoBuild = errors.New("no such build")

// update changes a build under the lock and saves it.
func (b *buildStore) update(id string, change func(r *BuildRun) error) (BuildRun, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[id]
	if !ok {
		return BuildRun{}, errNoBuild
	}
	err := change(r)
	if err == nil {
		err = b.save(r)
	}
	c := *r
	c.Steps = slices.Clone(r.Steps)
	return c, err
}

// applyStatus takes the status the agent sent.
func applyStatus(r *BuildRun, st build.Status, now time.Time) {
	r.State, r.Steps, r.Error, r.Updated = st.State, st.Steps, st.Error, now
	started := st.Started
	r.Started, r.Finished = &started, st.Finished
}

// appendLog adds data to a build's log if it starts where the log ends
// now, and returns the log's size.
func (b *buildStore) appendLog(id string, from int64, data []byte) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[id]
	if !ok {
		return 0, errNoBuild
	}
	if from != r.LogSize || len(data) == 0 || r.LogSize+int64(len(data)) > maxGatewayLog {
		return r.LogSize, nil
	}
	f, err := os.OpenFile(b.logPath(id), os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return r.LogSize, err
	}
	n, err := f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	r.LogSize += int64(n)
	if serr := b.save(r); err == nil {
		err = serr
	}
	return r.LogSize, err
}

// readLog returns up to n bytes of a build's log from offset from, as
// text the panel can show.
func (b *buildStore) readLog(id string, from int64, n int) (string, int64, error) {
	r, ok := b.get(id)
	if !ok {
		return "", 0, errNoBuild
	}
	if from < 0 || from >= r.LogSize {
		return "", r.LogSize, nil
	}
	f, err := os.Open(b.logPath(id))
	if err != nil {
		return "", r.LogSize, err
	}
	defer f.Close()
	buf := make([]byte, min(int64(n), r.LogSize-from))
	k, err := f.ReadAt(buf, from)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", r.LogSize, err
	}
	return strings.ToValidUTF8(string(buf[:k]), "�"), from + int64(k), nil
}
