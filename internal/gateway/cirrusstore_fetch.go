package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/store"
)

// The cache, one folder per source, laid out like the source itself:
//
//	DataDir/store/<source>/index.json, index.json.sig
//	DataDir/store/<source>/apps/<id>.json, apps/<id>.json.sig
//	DataDir/store/state.json   when each source was checked, and how it went

type storeState struct {
	Fetched time.Time              `json:"fetched"`
	Sources map[string]sourceState `json:"sources"`
}

type sourceState struct {
	Checked time.Time `json:"checked"`
	Status  string    `json:"status"`
	Detail  string    `json:"detail,omitempty"`
	// IndexSerial is the newest index seen: an older one is a replay.
	IndexSerial int64 `json:"indexSerial"`
}

func (s *Server) loadStoreState() storeState {
	st := storeState{Sources: map[string]sourceState{}}
	_ = readFileJSON(filepath.Join(s.storeDir(), "state.json"), &st)
	if st.Sources == nil {
		st.Sources = map[string]sourceState{}
	}
	return st
}

// readSigned reads a cached file and its signature.
func readSigned(p string) (store.Signed, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return store.Signed{}, err
	}
	sig, err := os.ReadFile(p + ".sig")
	return store.Signed{Data: data, Sig: string(sig)}, err
}

// cachedApps are the source's cached apps that still verify now: the
// index, then each app at the serial the index lists.
func (s *Server) cachedApps(src store.Source, now time.Time) []StoreApp {
	dir := filepath.Join(s.storeDir(), src.Name)
	f, err := readSigned(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil
	}
	ix, err := src.OpenIndex(f, now, 0)
	if err != nil {
		return nil
	}
	var out []StoreApp
	for _, e := range ix.Apps {
		// Listed from its index entry alone when it asks for a newer
		// CirrusUX than this: its definition may hold fields we can't read.
		if min := e.NeedsNewer(); min != "" {
			out = append(out, needsNewerApp(src.Name, e, min))
			continue
		}
		af, err := readSigned(filepath.Join(dir, "apps", e.ID+".json"))
		if err != nil {
			continue
		}
		// Not older than the index asks for, unless the fetch failed and the
		// previous (still valid) copy was kept; fetchSource never goes
		// backwards from what it had.
		a, err := src.OpenApp(e.ID, af, now, 0)
		if need, ok := catalogue.AsNeedsNewer(err); ok {
			out = append(out, needsNewerApp(src.Name, e, need.Min))
			continue
		}
		if err != nil {
			continue
		}
		out = append(out, StoreApp{App: a.Def, Source: src.Name, Serial: a.Serial, signed: &af})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) cachedIndexExpired(src store.Source, now time.Time) bool {
	f, err := readSigned(filepath.Join(s.storeDir(), src.Name, "index.json"))
	if err != nil {
		return false
	}
	_, err = src.OpenIndex(f, now, 0)
	return errors.Is(err, store.ErrExpired)
}

// refreshStore is Check CirrusStore: fetch each trusted source's index and
// the apps that changed, verify them, and swap them into the cache.
func (s *Server) refreshStore(w http.ResponseWriter, r *http.Request) {
	if !s.refreshingStore.TryLock() {
		http.Error(w, "a check is already running", http.StatusConflict)
		return
	}
	defer s.refreshingStore.Unlock()
	srcs, _ := trustedSources(s.store.Snapshot())
	st := s.loadStoreState()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	for _, src := range srcs {
		prev := st.Sources[src.Name]
		next := s.fetchSource(ctx, src, prev)
		if next.Status != "ok" {
			slog.Warn("cirrusstore", "source", src.Name, "status", next.Status, "detail", next.Detail)
		}
		st.Sources[src.Name] = next
	}
	st.Fetched = time.Now()
	if err := os.MkdirAll(s.storeDir(), 0o700); err == nil {
		if err := writeFileJSON(filepath.Join(s.storeDir(), "state.json"), st); err != nil {
			slog.Error("cirrusstore state", "err", err)
		}
	}
	writeJSON(w, s.storeView())
}

func (s *Server) fetchSource(ctx context.Context, src store.Source, prev sourceState) sourceState {
	now := time.Now()
	res := sourceState{Checked: now, IndexSerial: prev.IndexSerial}
	fail := func(status string, err error) sourceState {
		res.Status, res.Detail = status, err.Error()
		return res
	}
	ixFile, err := s.fetchSigned(ctx, src.URL+"index.json", store.MaxIndex)
	if err != nil {
		return fail("unreachable", err)
	}
	ix, err := src.OpenIndex(ixFile, now, prev.IndexSerial)
	if errors.Is(err, store.ErrExpired) {
		return fail("expired", err)
	}
	if err != nil {
		return fail("refused", err)
	}

	cache := filepath.Join(s.storeDir(), src.Name)
	tmp := cache + ".new"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "apps"), 0o700); err != nil {
		return fail("refused", err)
	}
	var refused, kept []string
	for _, e := range ix.Apps {
		if e.NeedsNewer() != "" {
			continue // listed from the index; nothing to fetch or run
		}
		f, err := s.appFile(ctx, src, cache, e, now)
		if err != nil {
			old, ok := s.cachedApp(src, cache, e.ID, now)
			if !ok {
				refused = append(refused, err.Error())
				continue
			}
			kept = append(kept, e.ID)
			f = old
		}
		if err := writeSignedFile(filepath.Join(tmp, "apps", e.ID+".json"), f); err != nil {
			return fail("refused", err)
		}
	}
	if err := writeSignedFile(filepath.Join(tmp, "index.json"), ixFile); err != nil {
		return fail("refused", err)
	}
	if err := swapDir(tmp, cache); err != nil {
		return fail("refused", err)
	}
	res.Status, res.IndexSerial = "ok", ix.Serial
	if len(refused) > 0 {
		res.Detail = fmt.Sprintf("%d app(s) left out: %s", len(refused), strings.Join(refused, "; "))
	}
	if len(kept) > 0 {
		if res.Detail != "" {
			res.Detail += "; "
		}
		res.Detail += fmt.Sprintf("%d app(s) kept at the previous copy: %s", len(kept), strings.Join(kept, ", "))
	}
	return res
}

// cachedApp is the app's previously cached envelope, if it's still valid.
func (s *Server) cachedApp(src store.Source, cache, id string, now time.Time) (store.Signed, bool) {
	f, err := readSigned(filepath.Join(cache, "apps", id+".json"))
	if err != nil {
		return f, false
	}
	if _, err := src.OpenApp(id, f, now, 0); err != nil && !isNeedsNewer(err) {
		return f, false
	}
	return f, true
}

// swapDir puts the new directory in place of cache: the old one is renamed
// aside first and put back if the second rename fails, so a failure never
// leaves the catalogue empty.
func swapDir(newDir, cache string) error {
	old := cache + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(cache, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(newDir, cache); err != nil {
		_ = os.Rename(old, cache)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

// appFile is the app's envelope at the index's serial: the cached one if
// it's that serial already, otherwise fetched. Never older than the cache.
func (s *Server) appFile(ctx context.Context, src store.Source, cache string, e store.Entry, now time.Time) (store.Signed, error) {
	var have int64
	if f, err := readSigned(filepath.Join(cache, "apps", e.ID+".json")); err == nil {
		if a, err := src.OpenApp(e.ID, f, now, 0); err == nil || isNeedsNewer(err) {
			if a.Serial == e.Serial {
				return f, nil
			}
			have = a.Serial
		}
	}
	f, err := s.fetchSigned(ctx, src.URL+"apps/"+e.ID+".json", store.MaxApp)
	if err != nil {
		return f, fmt.Errorf("%s: %v", e.ID, err)
	}
	a, err := src.OpenApp(e.ID, f, now, max(have, e.Serial))
	if err != nil && !isNeedsNewer(err) {
		return f, err
	}
	if a.Serial != e.Serial {
		return f, fmt.Errorf("%s: serial %d, the index says %d", e.ID, a.Serial, e.Serial)
	}
	return f, nil
}

func (s *Server) fetchSigned(ctx context.Context, url string, limit int64) (store.Signed, error) {
	data, err := s.fetch(ctx, url, limit)
	if err != nil {
		return store.Signed{}, err
	}
	sig, err := s.fetch(ctx, url+".sig", store.MaxSig)
	return store.Signed{Data: data, Sig: string(sig)}, err
}

// fetch GETs one file, refusing redirects off https and anything larger
// than limit. The client honours HTTPS_PROXY.
func (s *Server) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.storeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, limit)
	}
	return b, nil
}

func writeSignedFile(p string, f store.Signed) error {
	if err := writeFileAtomic(p, f.Data); err != nil {
		return err
	}
	return writeFileAtomic(p+".sig", []byte(f.Sig))
}

// newStoreClient fetches catalogues: through HTTPS_PROXY if set, https
// only, redirects included.
func newStoreClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 5 {
				return errors.New("redirected off https, or too often")
			}
			return nil
		},
	}
}
