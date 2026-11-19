package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cirrusux/internal/store"
)

// Previewing a source before trusting it: the owner gives a name, a URL and
// the publisher's public key, and the gateway fetches that catalogue once
// and checks it exactly as it would a trusted one. Nothing is cached and
// nothing changes on the server: trusting the source is still done by root
// (host.env's STORE_SOURCES and the store module), and the preview gives
// the line to add there.

// SourcePreviewRequest is what the owner typed.
type SourcePreviewRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Key is the publisher's NAME.pub line, or just its last field.
	Key string `json:"key"`
}

// PreviewApp is one app the source offers, and whether it would be usable.
type PreviewApp struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Group   string `json:"group"`
	Serial  int64  `json:"serial"`
	Image   string `json:"image,omitempty"`
	Problem string `json:"problem,omitempty"` // why it would be left out or unavailable
}

// SourcePreview is a source that verified, ready to be trusted on the server.
type SourcePreview struct {
	Name        string       `json:"name"`
	URL         string       `json:"url"`
	Fingerprint string       `json:"fingerprint"`
	Key         string       `json:"key"`     // the full key line
	Expires     time.Time    `json:"expires"` // the index's
	Apps        []PreviewApp `json:"apps"`
	// Entry is this source's STORE_SOURCES item: name|url|KEY.
	Entry string `json:"entry"`
}

// previewSourceInput checks what the owner typed and turns it into a source.
func previewSourceInput(req SourcePreviewRequest) (store.Source, error) {
	name := strings.TrimSpace(req.Name)
	if !store.ValidName(name) {
		return store.Source{}, errors.New("the name: 2 to 31 lowercase letters, digits and -, starting with a letter")
	}
	if name == store.Official || name == store.BuiltIn {
		return store.Source{}, fmt.Errorf("%q is CirrusUX's own; pick another name", name)
	}
	u, err := store.CheckURL(strings.TrimSpace(req.URL))
	if err != nil {
		return store.Source{}, err
	}
	line := strings.TrimSpace(req.Key)
	if !strings.HasPrefix(line, "cirrusstore-key ") {
		line = "cirrusstore-key v1 " + name + " " + line
	}
	key, err := store.ParseKey(line)
	if err != nil {
		return store.Source{}, err
	}
	return store.Source{Name: name, URL: u, Keys: []store.Key{key}}, nil
}

// previewSource is POST /api/store/preview.
func (s *Server) previewSource(w http.ResponseWriter, r *http.Request) {
	var req SourcePreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	src, err := previewSourceInput(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if snap := s.store.Snapshot(); snap != nil && snap.AppsHost != nil {
		for _, in := range snap.AppsHost.Sources {
			if in.Name == src.Name {
				http.Error(w, fmt.Sprintf("the server already has a source named %s", src.Name), http.StatusConflict)
				return
			}
		}
	}
	if !s.refreshingStore.TryLock() {
		http.Error(w, "a check is already running", http.StatusConflict)
		return
	}
	defer s.refreshingStore.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	p, err := s.fetchPreview(ctx, src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, p)
}

// fetchPreview fetches and verifies a source's index and every app in it,
// keeping nothing. An index that fails is an error; an app that fails is
// listed with its problem, as Check CirrusStore would leave it out.
func (s *Server) fetchPreview(ctx context.Context, src store.Source) (SourcePreview, error) {
	now := time.Now()
	f, err := s.fetchSigned(ctx, src.URL+"index.json", store.MaxIndex)
	if err != nil {
		return SourcePreview{}, err
	}
	ix, err := src.OpenIndex(f, now, 0)
	if err != nil {
		return SourcePreview{}, fmt.Errorf("the catalogue didn't verify with that key: %v", err)
	}
	key := src.Keys[0]
	p := SourcePreview{
		Name: src.Name, URL: src.URL, Fingerprint: key.Fingerprint(), Key: key.String(), Expires: ix.Expires,
		Apps:  []PreviewApp{},
		Entry: src.Name + "|" + src.URL + "|" + base64.StdEncoding.EncodeToString(key.Pub),
	}
	taken := map[string]string{}
	for _, a := range s.storeCatalogue() {
		if a.Unavailable == "" {
			taken[a.ID] = a.Source
		}
	}
	for _, e := range ix.Apps {
		pa := PreviewApp{ID: e.ID, Name: e.Name, Group: e.Group, Serial: e.Serial}
		if min := e.NeedsNewer(); min != "" {
			pa.Problem = "needs CirrusUX " + min + " or newer"
			p.Apps = append(p.Apps, pa)
			continue
		}
		af, err := s.fetchSigned(ctx, src.URL+"apps/"+e.ID+".json", store.MaxApp)
		if err == nil {
			var a store.App
			a, err = src.OpenApp(e.ID, af, now, e.Serial)
			switch {
			case isNeedsNewer(err):
				pa.Problem, err = "needs a newer CirrusUX", nil
			case err == nil && a.Serial != e.Serial:
				err = fmt.Errorf("serial %d, the index says %d", a.Serial, e.Serial)
			case err == nil:
				pa.Name, pa.Group, pa.Image = a.Def.Name, a.Def.Group, a.Def.Image
			}
		}
		if err != nil {
			pa.Problem = "left out: " + err.Error()
		} else if by, ok := taken[e.ID]; ok && pa.Problem == "" {
			pa.Problem = fmt.Sprintf("unavailable: %s already has an app with this id", sourceLabel(by))
		}
		p.Apps = append(p.Apps, pa)
	}
	return p, nil
}
