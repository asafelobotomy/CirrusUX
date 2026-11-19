package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"cirrusux/internal/build"
	"cirrusux/internal/githost"
	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
)

// Builds' HTTP side: the panel starts, follows and cancels builds under
// /api/git/; the agent fetches what to build and reports on it under
// /agent/builds/. Starting and cancelling go to the server as commands,
// like any action, and are written to the Changelog.

func (s *Server) buildRoutes(mux, api *http.ServeMux) {
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.git == nil || s.builds == nil {
				http.Error(w, "Git hosting is off", http.StatusNotFound)
				return
			}
			h(w, r)
		}
	}
	api.HandleFunc("GET /api/git/repos/{name}/builds", on(s.buildList))
	api.HandleFunc("POST /api/git/repos/{name}/builds", on(s.buildStart))
	api.HandleFunc("GET /api/git/repos/{name}/build-settings", on(s.buildSettingsGet))
	api.HandleFunc("GET /api/git/repos/{name}/secrets", on(s.secretList))
	api.HandleFunc("PUT /api/git/repos/{name}/secrets/{key}", on(s.secretPut))
	api.HandleFunc("DELETE /api/git/repos/{name}/secrets/{key}", on(s.secretDelete))
	api.HandleFunc("GET /api/git/builds/{id}", on(s.buildGet))
	api.HandleFunc("GET /api/git/builds/{id}/log", on(s.buildLog))
	api.HandleFunc("POST /api/git/builds/{id}/cancel", on(s.buildCancel))
	mux.HandleFunc("GET /agent/builds/{id}", s.agentOnly(on(s.agentBuild)))
	mux.HandleFunc("GET /agent/builds/{id}/source", s.agentOnly(on(s.agentBuildSource)))
	mux.HandleFunc("POST /agent/builds/{id}/progress", s.agentOnly(on(s.agentBuildProgress)))
}

func (s *Server) buildList(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := githost.ValidName(name); err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, s.builds.list(name))
}

func (s *Server) buildGet(w http.ResponseWriter, r *http.Request) {
	b, ok := s.builds.get(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such build", http.StatusNotFound)
		return
	}
	writeJSON(w, b.view())
}

func (s *Server) buildLog(w http.ResponseWriter, r *http.Request) {
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	text, next, err := s.builds.readLog(r.PathValue("id"), from, 1<<20)
	if errors.Is(err, errNoBuild) {
		http.Error(w, "no such build", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "reading the log failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"text": text, "next": next})
}

// canBuild says why the server can't take a build now, or "".
func (s *Server) canBuild(action string, snap *snapshot.Snapshot) (string, int) {
	if snap == nil || !slices.Contains(snap.Features, helperapi.ModBuilds) {
		return "Builds aren't set up on " + s.cfg.ServerName + " (BUILDS=yes in its helper.env; see docs/BUILDS.md)", http.StatusConflict
	}
	if msg := s.agentRefuses(action, snap); msg != "" {
		return msg, http.StatusConflict
	}
	if v := s.power.View(time.Now(), snap); v.State != "awake" {
		return s.cfg.ServerName + " isn't awake. Wake it first.", http.StatusConflict
	}
	return "", 0
}

// buildProblem is why a build can't be made, with the HTTP status to show.
type buildProblem struct {
	code int
	msg  string
	// noPipeline: the commit has no pipeline file, which a push to a
	// repository that builds on push doesn't count as a problem.
	noPipeline bool
}

func (e *buildProblem) Error() string { return e.msg }

// prepareBuild finds the commit ref points at and reads its pipeline.
func (s *Server) prepareBuild(ctx context.Context, name, ref string) (githost.Commit, build.Pipeline, string, error) {
	commits, err := s.git.Commits(ctx, name, ref, "", 1)
	if err != nil {
		code, msg := gitStatus(err)
		return githost.Commit{}, build.Pipeline{}, ref, &buildProblem{code: code, msg: msg}
	}
	if len(commits) == 0 {
		return githost.Commit{}, build.Pipeline{}, ref, &buildProblem{code: http.StatusBadRequest, msg: "There's nothing to build yet: push some commits first"}
	}
	c := commits[0]
	if ref == "" {
		ref = c.SHA[:12]
	}
	blob, err := s.git.Blob(ctx, name, c.SHA, build.File)
	var gerr *githost.Error
	switch {
	case errors.As(err, &gerr) && gerr.Kind == githost.NotFound:
		return c, build.Pipeline{}, ref, &buildProblem{code: http.StatusBadRequest, noPipeline: true,
			msg: "There's no " + build.File + " in " + name + " at " + ref + ". Add one to say what to build (docs/BUILDS.md)."}
	case err != nil:
		code, msg := gitStatus(err)
		return c, build.Pipeline{}, ref, &buildProblem{code: code, msg: msg}
	case blob.TooLarge || blob.Binary:
		return c, build.Pipeline{}, ref, &buildProblem{code: http.StatusBadRequest, msg: build.File + " isn't a small text file"}
	}
	p, err := build.Parse([]byte(blob.Text))
	if err != nil {
		return c, build.Pipeline{}, ref, &buildProblem{code: http.StatusBadRequest, msg: build.File + ": " + err.Error()}
	}
	return c, p, ref, nil
}

// newRun is a queued build of commit c under pipeline p.
func newRun(name, ref string, c githost.Commit, p build.Pipeline, who string) *BuildRun {
	now := time.Now()
	run := &BuildRun{ID: newBuildID(), Repo: name, Ref: ref, Commit: c.SHA, Subject: c.Subject, Who: who,
		Created: now, Updated: now, State: build.Queued, Pipeline: &p}
	for _, st := range p.Steps {
		run.Steps = append(run.Steps, build.StepStatus{Name: st.Name, Image: st.Image, State: build.Waiting})
	}
	return run
}

func (s *Server) buildStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if msg, code := s.canBuild("build_run", s.store.Snapshot()); msg != "" {
		http.Error(w, msg, code)
		return
	}
	c, p, ref, err := s.prepareBuild(r.Context(), name, body.Ref)
	if err != nil {
		var bp *buildProblem
		errors.As(err, &bp)
		http.Error(w, bp.msg, bp.code)
		return
	}
	if _, err := s.secretsFor(name, p, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	run := newRun(name, ref, c, p, userOf(r))
	if err := s.builds.add(run); err != nil {
		http.Error(w, "saving the build failed", http.StatusInternalServerError)
		return
	}
	s.queueBuild(run)
	writeJSONStatus(w, http.StatusAccepted, run.view())
}

// queueBuild tells the server to start a build.
func (s *Server) queueBuild(run *BuildRun) {
	s.queue.Add(&Command{
		ActionReq: ActionReq{Action: "build_run", Args: map[string]string{"run": run.ID}},
		Label:     buildLabel(run),
		Who:       run.Who,
	})
}

func buildLabel(run *BuildRun) string {
	after := ""
	if run.Auto {
		after = " after a push"
	}
	return fmt.Sprintf("Started a build of %s at %s (%s)%s", run.Repo, run.Commit[:8], run.Ref, after)
}

func newBuildID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) buildCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := userOf(r)
	var queueCancel bool
	b, err := s.builds.update(id, func(b *BuildRun) error {
		switch {
		case build.Finished(b.State):
			return fmt.Errorf("the build has already ended")
		case b.State == build.Queued:
			// The server hasn't started it: it never will.
			now := time.Now()
			b.State, b.Error, b.Finished = build.Cancelled, "Cancelled before it started", &now
			for i := range b.Steps {
				b.Steps[i].State = build.Skipped
			}
		default:
			if msg, code := s.canBuild("build_cancel", s.store.Snapshot()); msg != "" && code != 0 {
				return errors.New(msg)
			}
			b.CancelSent, queueCancel = true, true
		}
		return nil
	})
	if errors.Is(err, errNoBuild) {
		http.Error(w, "no such build", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	label := fmt.Sprintf("Cancelled the build of %s at %s", b.Repo, b.Commit[:8])
	if queueCancel {
		s.queue.Add(&Command{ActionReq: ActionReq{Action: "build_cancel", Args: map[string]string{"run": id}}, Label: label, Who: who})
	} else {
		s.record(AuditEntry{Who: who, What: label})
	}
	writeJSON(w, b.view())
}

// buildResult marks a build failed when the server couldn't start it.
func (s *Server) buildResult(c Command) {
	if c.Action != "build_run" || c.State != "failed" || s.builds == nil {
		return
	}
	_, _ = s.builds.update(c.Args["run"], func(b *BuildRun) error {
		if b.State == build.Queued {
			now := time.Now()
			b.State, b.Error, b.Finished = build.Failed, c.Error, &now
			for i := range b.Steps {
				b.Steps[i].State = build.Skipped
			}
		}
		return nil
	})
}

// ---- agent ----

func (s *Server) agentBuild(w http.ResponseWriter, r *http.Request) {
	b, ok := s.builds.get(r.PathValue("id"))
	if !ok || b.State != build.Queued || b.Pipeline == nil {
		http.Error(w, "that build isn't waiting to start", http.StatusGone)
		return
	}
	req := build.Request{Job: b.ID, Repo: b.Repo, Ref: b.Ref, Commit: b.Commit, Pipeline: *b.Pipeline}
	if c := b.Pipeline.Cache; c != "" {
		// Builds from pushes and builds started by hand keep separate caches.
		req.CacheKey = "manual-" + c
		if b.Auto {
			req.CacheKey = "push-" + c
		}
	}
	// The secrets are read now, so a build gets the values as they are when
	// the server asks.
	var err error
	if req.Secrets, err = s.secretsFor(b.Repo, *b.Pipeline, b.Auto); err != nil {
		s.failWaiting(b.ID, err.Error())
		http.Error(w, err.Error(), http.StatusGone)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, req)
}

func (s *Server) agentBuildSource(w http.ResponseWriter, r *http.Request) {
	b, ok := s.builds.get(r.PathValue("id"))
	if !ok || b.State != build.Queued {
		http.Error(w, "that build isn't waiting to start", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	if err := s.git.Archive(r.Context(), b.Repo, b.Commit, "tar.gz", w); err != nil {
		gitError(w, err)
	}
}

func (s *Server) agentBuildProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p struct {
		Status build.Status `json:"status"`
		From   int64        `json:"from"`
		Log    []byte       `json:"log"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if err := p.Status.Check(); err != nil || p.Status.Job != id {
		http.Error(w, "bad status", http.StatusBadRequest)
		return
	}
	var cancel, stop bool
	b, err := s.builds.update(id, func(b *BuildRun) error {
		if b.State == build.Cancelled && !build.Finished(p.Status.State) {
			// Cancelled here while the server was starting it.
			cancel, stop = !b.CancelSent, true
			b.CancelSent = true
			return nil
		}
		applyStatus(b, p.Status, time.Now())
		return nil
	})
	if errors.Is(err, errNoBuild) {
		http.Error(w, "no such build", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "saving the build failed", http.StatusInternalServerError)
		return
	}
	if cancel {
		s.queue.Add(&Command{ActionReq: ActionReq{Action: "build_cancel", Args: map[string]string{"run": id}},
			Label: fmt.Sprintf("Cancelled the build of %s at %s", b.Repo, b.Commit[:8]), Who: b.Who})
	}
	size := b.LogSize
	if p.From >= 0 {
		if size, err = s.builds.appendLog(id, p.From, p.Log); err != nil {
			http.Error(w, "saving the log failed", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"size": size, "stop": stop})
}
