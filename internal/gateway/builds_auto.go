package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/fsutil"
	"cirrusux/internal/githost"
	"cirrusux/internal/snapshot"
)

// Builds on push (docs/BUILDS.md). Off for every repository until its
// owner turns it on, and then only for branches the pipeline names (all
// of them if it names none). A push makes a build per branch it moved;
// the build waits on the gateway until the server is awake, and the
// repository can also ask for the server to be woken.

// BuildSettings are one repository's choices.
type BuildSettings struct {
	OnPush bool `json:"onPush"`
	// Wake: a push wakes a sleeping server to run its build, rather than
	// waiting for it to wake by itself.
	Wake bool `json:"wake"`
}

type buildSettingsStore struct {
	path string
	mu   sync.Mutex
	m    map[string]BuildSettings
}

func openBuildSettings(path string) *buildSettingsStore {
	st := &buildSettingsStore{path: path, m: map[string]BuildSettings{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &st.m); err != nil {
			slog.Warn("build settings: starting empty", "err", err)
			st.m = map[string]BuildSettings{}
		}
	}
	return st
}

func (st *buildSettingsStore) get(repo string) BuildSettings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.m[repo]
}

// set stores a repository's settings and returns the ones it had.
func (st *buildSettingsStore) set(repo string, v BuildSettings) (BuildSettings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	old := st.m[repo]
	if v == (BuildSettings{}) {
		delete(st.m, repo)
	} else {
		st.m[repo] = v
	}
	return old, st.save()
}

// rename moves a repository's settings with it.
func (st *buildSettingsStore) rename(from, to string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if v, ok := st.m[from]; ok {
		delete(st.m, from)
		st.m[to] = v
		_ = st.save()
	}
}

func (st *buildSettingsStore) save() error {
	return fsutil.WriteJSON(st.path, st.m, 0o600)
}

func (s *Server) buildSettingsGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := githost.ValidName(name); err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, s.buildSettings.get(name))
}

// buildSettingsAction is the git_build_settings action: confirmed and
// logged like any change, with the old settings as its undo.
func buildSettingsAction(s *Server, _ context.Context, a gitArgs) (*ActionReq, []string, error) {
	if !s.git.Exists(a["name"]) {
		return nil, nil, localErr("There's no repository called " + a["name"])
	}
	old, err := s.buildSettings.set(a["name"], BuildSettings{OnPush: a["on_push"] == "yes", Wake: a["wake"] == "yes"})
	return gitUndo("git_build_settings", gitArgs{"name": a["name"], "on_push": yesNo(old.OnPush), "wake": yesNo(old.Wake)}), nil, err
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func checkBuildSettings(a gitArgs, snap *snapshot.Snapshot) error {
	for _, k := range []string{"on_push", "wake"} {
		if a[k] != "yes" && a[k] != "no" {
			return fmt.Errorf("%s: yes or no", k)
		}
	}
	if a["wake"] == "yes" && a["on_push"] != "yes" {
		return errors.New("waking the server only means something when builds run on push")
	}
	return checkGitName(a, snap)
}

func buildSettingsLabel(a gitArgs) string {
	switch {
	case a["on_push"] != "yes":
		return "Stopped running builds of " + a["name"] + " on push"
	case a["wake"] == "yes":
		return "Run builds of " + a["name"] + " on push, waking the server if it sleeps"
	}
	return "Run builds of " + a["name"] + " on push, once the server is awake"
}

func init() {
	actionSpecs["git_build_settings"] = actionSpec{label: buildSettingsLabel, check: checkBuildSettings, local: buildSettingsAction}
}

// maxPushBuilds caps the builds one push can start, so pushing every
// branch of a big repository at once doesn't queue them all.
const maxPushBuilds = 5

// pushHeads is a repository's branches before a push, if its builds run on
// push; nil otherwise.
func (s *Server) pushHeads(ctx context.Context, repo string) map[string]string {
	if s.builds == nil || !s.buildSettings.get(repo).OnPush {
		return nil
	}
	heads, err := s.git.Heads(ctx, repo)
	if err != nil {
		slog.Warn("builds on push: reading branches", "repo", repo, "err", err)
		return nil
	}
	return heads
}

// afterPush starts builds for the branches a push moved.
func (s *Server) afterPush(repo, who string, before map[string]string) {
	if before == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.baseContext(), 2*time.Minute)
	defer cancel()
	after, err := s.git.Heads(ctx, repo)
	if err != nil {
		slog.Warn("builds on push: reading branches", "repo", repo, "err", err)
		return
	}
	var moved []string
	for b, sha := range after {
		if before[b] != sha {
			moved = append(moved, b)
		}
	}
	slices.Sort(moved)
	started := 0
	for _, b := range moved {
		if started == maxPushBuilds {
			s.record(AuditEntry{Who: who, What: fmt.Sprintf("Didn't build %d more branches of %s after a push (up to %d at once)", len(moved)-started, repo, maxPushBuilds)})
			break
		}
		if s.pushBuild(ctx, repo, b, after[b], who) {
			started++
		}
	}
	if started > 0 {
		s.wakeForBuilds(repo, who)
		s.dispatchBuilds()
	}
}

// pushBuild makes a waiting build of branch b at sha, unless the pipeline
// there doesn't ask for one. It reports whether a build was made.
func (s *Server) pushBuild(ctx context.Context, repo, b, sha, who string) bool {
	c, p, ref, err := s.prepareBuild(ctx, repo, b)
	var bp *buildProblem
	switch {
	case errors.As(err, &bp) && bp.noPipeline:
		return false
	case err == nil && (c.SHA != sha || !p.BuildsOnPush(b)):
		return false // moved again since, or not a branch this pipeline builds
	case err != nil && (!errors.As(err, &bp) || bp.code != http.StatusBadRequest):
		slog.Warn("builds on push", "repo", repo, "branch", b, "err", err)
		return false
	}
	run := newRun(repo, ref, c, p, who)
	run.Auto = true
	if err != nil {
		// A pipeline file that doesn't parse shows as a failed build, so
		// the push that broke it is noticed.
		now := time.Now()
		run.State, run.Error, run.Finished, run.Pipeline = build.Failed, bp.msg, &now, nil
		run.Steps = nil
		run.Commit = sha
	}
	// A newer push to the branch replaces builds still waiting for the server.
	s.builds.supersede(repo, b)
	if err := s.builds.add(run); err != nil {
		slog.Error("builds on push", "err", err)
		return false
	}
	return true
}

// supersede cancels the waiting, not yet started builds a push made for
// branch b of repo.
func (b *buildStore) supersede(repo, branch string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for _, r := range b.runs {
		if r.Repo == repo && r.Ref == branch && r.Auto && r.State == build.Queued && r.DispatchedAt == nil {
			r.State, r.Error, r.Finished = build.Cancelled, "Replaced by a newer push", &now
			for i := range r.Steps {
				r.Steps[i].State = build.Skipped
			}
			_ = b.save(r)
		}
	}
}

// waiting lists the builds a push made that the server hasn't been told
// about, oldest first.
func (b *buildStore) waiting() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ids []*BuildRun
	for _, r := range b.runs {
		if r.State == build.Queued && r.Auto && r.DispatchedAt == nil {
			ids = append(ids, r)
		}
	}
	slices.SortFunc(ids, func(x, y *BuildRun) int { return x.Created.Compare(y.Created) })
	out := make([]string, len(ids))
	for i, r := range ids {
		out[i] = r.ID
	}
	return out
}

// dispatchBuilds hands waiting builds to the server once it is awake and
// can build. The agent's reports call it, so a server that wakes by
// itself, or is woken for a build, gets its work as soon as it reports.
func (s *Server) dispatchBuilds() {
	if s.builds == nil {
		return
	}
	for _, id := range s.builds.waiting() {
		snap := s.store.Snapshot()
		if msg, _ := s.canBuild("build_run", snap); msg != "" {
			if s.serverAwake(snap) {
				// Awake, and it still can't: that won't change by waiting.
				s.failWaiting(id, msg)
				continue
			}
			return
		}
		var run *BuildRun
		_, _ = s.builds.update(id, func(r *BuildRun) error {
			if r.State != build.Queued || r.DispatchedAt != nil {
				return errors.New("not waiting")
			}
			now := time.Now()
			r.DispatchedAt, r.Updated = &now, now
			cp := *r
			run = &cp
			return nil
		})
		if run != nil {
			s.queueBuild(run)
		}
	}
}

func (s *Server) failWaiting(id, msg string) {
	_, _ = s.builds.update(id, func(r *BuildRun) error {
		if r.State == build.Queued && r.DispatchedAt == nil {
			now := time.Now()
			r.State, r.Error, r.Finished = build.Failed, msg, &now
			for i := range r.Steps {
				r.Steps[i].State = build.Skipped
			}
		}
		return nil
	})
}

func (s *Server) serverAwake(snap *snapshot.Snapshot) bool {
	return s.power.View(time.Now(), snap).State == "awake"
}

// wakeForBuilds wakes a sleeping server if the repository asks for that.
func (s *Server) wakeForBuilds(repo, who string) {
	if !s.buildSettings.get(repo).Wake || !s.power.CanWake() || s.serverAwake(s.store.Snapshot()) {
		return
	}
	failed := func(err error) {
		s.record(AuditEntry{Who: who, What: "Wake of " + s.cfg.ServerName + " for a build of " + repo + " (failed)", Result: err.Error()})
	}
	if s.power.Wake(failed) {
		s.record(AuditEntry{Who: who, What: "Sent a wake to " + s.cfg.ServerName + " for a build of " + repo})
	}
}
