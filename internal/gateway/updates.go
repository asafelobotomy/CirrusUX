package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
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
	// Definition: the update is a newer signed definition from Source
	// (new ports, settings or version), at Serial.
	Definition bool   `json:"definition,omitempty"`
	Source     string `json:"source,omitempty"`
	Serial     int64  `json:"serial,omitempty"`
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
	writeJSON(w, s.withDefinitionUpdates(u, snap))
}

func (s *Server) checkApp(ctx context.Context, a snapshot.App) AppUpdate {
	res := AppUpdate{App: a.ID, Name: a.Name, Current: a.Tag}
	cat, ok := s.findStoreApp("", a.ID)
	if !ok {
		res.Error = "not in CirrusStore's catalogue"
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
	writeJSON(w, s.withDefinitionUpdates(s.store.Updates(), s.store.Snapshot()))
}

// withDefinitionUpdates adds the newer signed definitions CirrusStore's
// last check fetched, which need no registry check: one replaces a
// version-only update for the same app.
func (s *Server) withDefinitionUpdates(u Updates, snap *snapshot.Snapshot) Updates {
	if snap == nil {
		return u
	}
	for _, a := range snap.Apps {
		c, ok := s.definitionUpdate(a)
		if !ok {
			continue
		}
		up := AppUpdate{App: a.ID, Name: a.Name, Current: a.Tag, Latest: c.Tag, Digest: c.Digest, Definition: true, Source: c.Source, Serial: c.Serial}
		i := slices.IndexFunc(u.Apps, func(x AppUpdate) bool { return x.App == a.ID })
		if i < 0 {
			u.Apps = append(u.Apps, up)
		} else {
			u.Apps[i] = up
		}
	}
	return u
}

// checkUpdate lets an update through only to the version the last check
// found for that app, back to its previous version, or (naming its store)
// to the newer signed definition CirrusStore offers, which it attaches.
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
	if a["store"] != "" {
		for _, x := range snap.Apps {
			if c, ok := s.definitionUpdate(x); ok && x.ID == a["app"] && c.Source == a["store"] && c.Tag == a["tag"] && c.Digest == a["digest"] {
				a["signed"] = encodeSigned(c.signed)
				return nil
			}
		}
		return errNotOffered
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
