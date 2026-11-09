package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/store"
)

// CirrusStore: the built-in apps, always there (and the offline fallback),
// plus the apps of every source the server trusts, fetched only when the
// owner presses Check and kept in DataDir/store. Each file is verified
// against the keys the server's helper reported, every time it's read, and
// the helper verifies an install's definition again before running it.

// builtInApps is the catalogue compiled into this binary, loaded once.
var builtInApps = func() []catalogue.App {
	apps, err := catalogue.Load()
	if err != nil {
		panic("built-in catalogue: " + err.Error())
	}
	return apps
}()

// StoreApp is one app as the panel lists it.
type StoreApp struct {
	catalogue.App
	Source string `json:"source"` // store.BuiltIn, store.Official or a third-party source
	Serial int64  `json:"serial,omitempty"`
	// Unavailable says why it can't be installed (its id is taken).
	Unavailable string `json:"unavailable,omitempty"`
	// NeedsVersion: it needs this CirrusUX version or newer, so it is listed
	// but can't be installed or updated to yet. Only its name, group and icon
	// are known; its definition may use fields this version can't read.
	NeedsVersion string `json:"needsVersion,omitempty"`
	signed       *store.Signed
}

// needsNewerApp lists an app from its index entry when its definition is
// meant for a newer CirrusUX than this one.
func needsNewerApp(source string, e store.Entry, min string) StoreApp {
	return StoreApp{
		App: catalogue.App{
			ID: e.ID, Name: e.Name, Group: e.Group, Icon: e.Icon, MinVersion: min,
			Description: "Needs CirrusUX " + min + " or newer.",
			Ports:       []catalogue.Port{}, Volumes: []catalogue.Volume{},
		},
		Source: source, Serial: e.Serial, NeedsVersion: min,
	}
}

func isNeedsNewer(err error) bool {
	_, ok := catalogue.AsNeedsNewer(err)
	return ok
}

// knownNames maps app ids to names for audit labels, which only see ids.
var knownNames sync.Map

func appName(id string) string {
	if n, ok := knownNames.Load(id); ok {
		return n.(string)
	}
	for _, a := range builtInApps {
		if a.ID == id {
			return a.Name
		}
	}
	return id
}

// trustedSources are the sources in the server's last report that it can
// use (a file the helper refused, or a source without a key, is left out).
func trustedSources(snap *snapshot.Snapshot) ([]store.Source, []store.Info) {
	if snap == nil || snap.AppsHost == nil {
		return nil, nil
	}
	var srcs []store.Source
	for _, in := range snap.AppsHost.Sources {
		if in.Error != "" {
			continue
		}
		if s, err := store.FromInfo(in); err == nil && len(s.Keys) > 0 {
			srcs = append(srcs, s)
		}
	}
	// The official source first: its apps win an id another source uses.
	sort.SliceStable(srcs, func(i, j int) bool { return srcs[i].Official && !srcs[j].Official })
	return srcs, snap.AppsHost.Sources
}

// storeCatalogue merges the built-in apps with every trusted source's.
// Ids are unique: the official source's version of a built-in app replaces
// it; any other app whose id is taken is listed as unavailable.
func (s *Server) storeCatalogue() []StoreApp {
	out := make([]StoreApp, 0, len(builtInApps))
	at := map[string]int{}
	for _, a := range builtInApps {
		at[a.ID] = len(out)
		out = append(out, StoreApp{App: a, Source: store.BuiltIn})
	}
	srcs, _ := trustedSources(s.store.Snapshot())
	now := time.Now()
	for _, src := range srcs {
		for _, a := range s.cachedApps(src, now) {
			i, taken := at[a.ID]
			switch {
			case !taken:
				at[a.ID] = len(out)
				out = append(out, a)
			case a.NeedsVersion != "" && out[i].Source == store.BuiltIn:
				// The built-in app stays installable; heldBackUpdate tells an
				// installed one about the newer definition.
			case src.Official && out[i].Source == store.BuiltIn:
				out[i] = a
			default:
				a.Unavailable = fmt.Sprintf("%s already has an app with the id %q", sourceLabel(out[i].Source), a.ID)
				out = append(out, a)
			}
		}
	}
	for _, a := range out {
		if a.Unavailable == "" {
			knownNames.Store(a.ID, a.Name)
		}
	}
	return out
}

func sourceLabel(name string) string {
	switch name {
	case store.BuiltIn:
		return "CirrusUX's built-in catalogue"
	case store.Official:
		return "CirrusStore"
	}
	return "the source " + name
}

// findStoreApp is the installable app id from source; source "" takes
// whichever source the merged catalogue offers it from.
func (s *Server) findStoreApp(source, id string) (StoreApp, bool) {
	for _, a := range s.storeCatalogue() {
		if a.ID == id && a.Unavailable == "" && (source == "" || a.Source == source) {
			return a, true
		}
	}
	return StoreApp{}, false
}

// installedSource is the source an installed app's record names.
func installedSource(a snapshot.App) string {
	if a.Source == "" {
		return store.BuiltIn
	}
	return a.Source
}

// updateSource is the source an installed app's updates come from: its own,
// or the official one for a built-in app.
func updateSource(a snapshot.App) string {
	if src := installedSource(a); src != store.BuiltIn {
		return src
	}
	return store.Official
}

// heldBackUpdate is a newer definition for an installed app that needs a
// newer CirrusUX than this one: not offered, but its reason is shown.
func (s *Server) heldBackUpdate(a snapshot.App) (StoreApp, bool) {
	srcs, _ := trustedSources(s.store.Snapshot())
	for _, src := range srcs {
		if src.Name != updateSource(a) {
			continue
		}
		for _, c := range s.cachedApps(src, time.Now()) {
			if c.ID == a.ID && c.NeedsVersion != "" && (installedSource(a) == store.BuiltIn || c.Serial > a.Serial) {
				return c, true
			}
		}
	}
	return StoreApp{}, false
}

// definitionUpdate is a newer signed definition for an installed app: from
// its own source, or from the official source for a built-in app.
func (s *Server) definitionUpdate(a snapshot.App) (StoreApp, bool) {
	src := updateSource(a)
	c, ok := s.findStoreApp(src, a.ID)
	if !ok || c.signed == nil || c.NeedsVersion != "" {
		return StoreApp{}, false
	}
	if installedSource(a) == store.BuiltIn {
		// Only when the official definition differs from the built-in one.
		for _, b := range builtInApps {
			if b.ID == a.ID && reflect.DeepEqual(b, c.App) {
				return StoreApp{}, false
			}
		}
		return c, true
	}
	return c, c.Serial > a.Serial
}

func encodeSigned(f *store.Signed) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// ---- HTTP ----

// getCatalogue lists every app CirrusStore offers, tagged with its source.
func (s *Server) getCatalogue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.storeCatalogue())
}

// SourceStatus is a source as the panel's Sources card shows it.
type SourceStatus struct {
	store.Info
	// Status: ok, not-checked, expired, unreachable, refused or unusable.
	Status  string     `json:"status"`
	Detail  string     `json:"detail,omitempty"`
	Checked *time.Time `json:"checked,omitempty"`
	Apps    int        `json:"apps"`
}

// StoreView is the state of CirrusStore's sources.
type StoreView struct {
	Fetched *time.Time     `json:"fetched,omitempty"`
	Sources []SourceStatus `json:"sources"`
}

func (s *Server) storeView() StoreView {
	st := s.loadStoreState()
	v := StoreView{Sources: []SourceStatus{}}
	if !st.Fetched.IsZero() {
		v.Fetched = &st.Fetched
	}
	_, infos := trustedSources(s.store.Snapshot())
	now := time.Now()
	for _, in := range infos {
		ss := SourceStatus{Info: in, Status: "not-checked"}
		if in.Error != "" {
			ss.Status, ss.Detail = "unusable", in.Error
			v.Sources = append(v.Sources, ss)
			continue
		}
		if rec, ok := st.Sources[in.Name]; ok {
			ss.Status, ss.Detail = rec.Status, rec.Detail
			t := rec.Checked
			ss.Checked = &t
		}
		if src, err := store.FromInfo(in); err == nil {
			ss.Apps = len(s.cachedApps(src, now))
			if ss.Status == "ok" && ss.Apps == 0 && s.cachedIndexExpired(src, now) {
				ss.Status, ss.Detail = "expired", "its catalogue has expired: check again"
			}
		}
		v.Sources = append(v.Sources, ss)
	}
	return v
}

func (s *Server) getStore(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.storeView())
}

func (s *Server) storeDir() string { return filepath.Join(s.store.dir, "store") }
