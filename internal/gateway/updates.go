package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/registry"
	"cirrusux/internal/snapshot"
)

// App updates (PHASES-4-6 §6a). The gateway asks the registries, only when
// the owner checks, so a sleeping server is never woken for it. The result
// is kept, and an update may only move to a version the check found or
// back to the app's previous one.

// Updates is the last check.
type Updates struct {
	Checked time.Time   `json:"checked"`
	Apps    []AppUpdate `json:"apps"`
}

// AppUpdate is one installed app's result.
type AppUpdate struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Current string `json:"current"`
	// Latest and Digest are set when a newer version of the same shape
	// exists; empty means up to date.
	Latest string `json:"latest,omitempty"`
	Digest string `json:"digest,omitempty"`
	Error  string `json:"error,omitempty"`
}

var checking sync.Mutex

var (
	errBadPin     = errors.New("an update needs an exact tag (never latest) and a sha256 digest")
	errNotOffered = errors.New("that version wasn't offered: check for updates first")
)

// checkUpdates asks the registries about every installed app.
func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	if !checking.TryLock() {
		http.Error(w, "a check is already running", http.StatusConflict)
		return
	}
	defer checking.Unlock()
	snap := s.store.Snapshot()
	if snap == nil || len(snap.Apps) == 0 {
		writeJSON(w, Updates{Checked: time.Now(), Apps: []AppUpdate{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	u := Updates{Checked: time.Now(), Apps: []AppUpdate{}}
	for _, a := range snap.Apps {
		u.Apps = append(u.Apps, s.checkApp(ctx, a))
	}
	if err := s.store.SaveUpdates(u); err != nil {
		slog.Error("save updates", "err", err)
	}
	writeJSON(w, u)
}

func (s *Server) checkApp(ctx context.Context, a snapshot.App) AppUpdate {
	res := AppUpdate{App: a.ID, Name: a.Name, Current: a.Tag}
	cat, ok := findApp(a.ID)
	if !ok {
		res.Error = "not in this version's catalogue"
		return res
	}
	tags, err := s.registry.Tags(ctx, cat.Image)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if res.Latest = registry.Newest(a.Tag, tags); res.Latest == "" {
		return res
	}
	if res.Digest, err = s.registry.Digest(ctx, cat.Image, res.Latest); err != nil {
		res.Latest, res.Error = "", err.Error()
	}
	return res
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Updates())
}

// checkUpdate lets an update through only to the version the last check
// found for that app, or back to its previous version.
func (s *Server) checkUpdate(a map[string]string, snap *snapshot.Snapshot) error {
	if err := checkInstalled(a, snap); err != nil {
		return err
	}
	if !catalogue.ValidPin(a["tag"], a["digest"]) {
		return errBadPin
	}
	for _, x := range snap.Apps {
		if x.ID == a["app"] && x.PreviousTag == a["tag"] && x.PreviousDigest == a["digest"] {
			return nil // rolling back
		}
	}
	for _, u := range s.store.Updates().Apps {
		if u.App == a["app"] && u.Latest == a["tag"] && u.Digest == a["digest"] {
			return nil
		}
	}
	return errNotOffered
}

func (s *Store) Updates() Updates {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := Updates{Apps: []AppUpdate{}}
	_ = readFileJSON(filepath.Join(s.dir, "updates.json"), &u)
	return u
}

func (s *Store) SaveUpdates(u Updates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileJSON(filepath.Join(s.dir, "updates.json"), u)
}
