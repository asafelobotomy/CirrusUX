package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/snapshot"
	"cirrusux/internal/store"
	"cirrusux/internal/version"
)

// publishFuture signs a catalogue holding one app written for CirrusUX
// 0.8.0 (a field this version has never heard of) and one ordinary app.
// withEntry says whether the index entry carries minVersion too.
func publishFuture(t *testing.T, dir string, sec store.Secret, withEntry bool) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "apps"), 0o755)
	exp := time.Now().Add(24 * time.Hour)
	ix := store.Index{Source: "extra", Serial: 1, Expires: exp}
	var def map[string]any
	_ = json.Unmarshal(builtInDef(t, "navidrome", func(s string) string { return s }), &def)
	def["id"], def["name"], def["minVersion"], def["fromTheFuture"] = "future-app", "Future App", "0.8.0", map[string]any{"x": 1}
	raw, _ := json.Marshal(def)
	env, _ := json.Marshal(map[string]any{"source": "extra", "serial": 3, "expires": exp, "app": json.RawMessage(raw)})
	os.WriteFile(filepath.Join(dir, "apps", "future-app.json"), env, 0o644)
	os.WriteFile(filepath.Join(dir, "apps", "future-app.json.sig"), sec.Sign(env), 0o644)
	e := store.Entry{ID: "future-app", Serial: 3, Name: "Future App", Group: "Media"}
	if withEntry {
		e.MinVersion = "0.8.0"
	}
	ix.Apps = append(ix.Apps, e)
	b, _ := json.Marshal(ix)
	os.WriteFile(filepath.Join(dir, "index.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "index.json.sig"), sec.Sign(b), 0o644)
}

func TestAppsNeedingNewerCirrusUX(t *testing.T) {
	prev := version.Version
	version.Version = "0.7.0"
	defer func() { version.Version = prev }()

	for _, withEntry := range []bool{true, false} {
		root := t.TempDir()
		ts := httptest.NewTLSServer(http.FileServer(http.Dir(root)))
		defer ts.Close()
		extKey, extSec, _ := store.GenerateKey("extra", nil)
		publishFuture(t, filepath.Join(root, "extra"), extSec, withEntry)

		s, h := newTestServer(t)
		s.storeClient = ts.Client()
		snap := awakeSnap()
		snap.AppsHost = &helperapi.AppsInfo{Runtime: "podman", AppsDir: "/srv/cirrusux/apps", MediaRoots: []string{"/srv/media"}, TZ: "UTC", UID: 990, GID: 990,
			Sources: []store.Info{store.Source{Name: "extra", URL: ts.URL + "/extra/", Keys: []store.Key{extKey}}.Info()}}
		_ = s.store.SaveSnapshot(snap)

		w := do(h, "POST", "/api/store/refresh", "", actor)
		var v StoreView
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if w.Code != http.StatusOK || v.Sources[0].Status != "ok" || strings.Contains(v.Sources[0].Detail, "left out") {
			t.Fatalf("withEntry=%v refresh: %d %s", withEntry, w.Code, w.Body)
		}
		var apps []StoreApp
		_ = json.Unmarshal(do(h, "GET", "/api/catalogue", "", owner).Body.Bytes(), &apps)
		var got *StoreApp
		for i := range apps {
			if apps[i].ID == "future-app" {
				got = &apps[i]
			}
		}
		if got == nil || got.NeedsVersion != "0.8.0" || got.Name != "Future App" || got.Unavailable != "" {
			t.Fatalf("withEntry=%v listed as %+v", withEntry, got)
		}

		w = do(h, "POST", "/api/actions", `{"action":"app_install","args":{"app":"future-app","store":"extra"}}`, actor)
		want := "This app needs CirrusUX 0.8.0 or newer: run sudo cirrusux-setup upgrade on the server."
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) {
			t.Errorf("withEntry=%v install: %d %s", withEntry, w.Code, w.Body)
		}
		w = do(h, "POST", "/api/apps/preview", `{"app":"future-app","store":"extra","options":{}}`, owner)
		if w.Code == http.StatusOK {
			t.Errorf("withEntry=%v preview of an app that can't be installed: %s", withEntry, w.Body)
		}

		// Installed from this source at serial 1: the update is held back, and
		// naming it in an update is refused with the same reason.
		snap.Apps = []snapshot.App{{ID: "future-app", Name: "Future App", Tag: "1.0.0", Digest: oldDigest, Source: "extra", Serial: 1}}
		_ = s.store.SaveSnapshot(snap)
		var u Updates
		_ = json.Unmarshal(do(h, "GET", "/api/updates", "", owner).Body.Bytes(), &u)
		if len(u.Apps) != 1 || u.Apps[0].NeedsVersion != "0.8.0" || u.Apps[0].Latest != "" || u.Apps[0].Definition {
			t.Fatalf("withEntry=%v updates: %+v", withEntry, u)
		}
		body := fmt.Sprintf(`{"action":"app_update","args":{"app":"future-app","store":"extra","tag":"2.0.0","digest":%q}}`, newDigest)
		if w := do(h, "POST", "/api/actions", body, actor); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) {
			t.Errorf("withEntry=%v update: %d %s", withEntry, w.Code, w.Body)
		}
		// An update the installed serial already has isn't held back.
		snap.Apps[0].Serial = 3
		_ = s.store.SaveSnapshot(snap)
		_ = json.Unmarshal(do(h, "GET", "/api/updates", "", owner).Body.Bytes(), &u)
		if len(u.Apps) != 0 {
			t.Errorf("withEntry=%v nothing newer, but: %+v", withEntry, u)
		}
	}
}
