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
)

// publish writes a signed catalogue for source into dir: each definition
// at the given serial, and an index.
func publish(t *testing.T, dir string, sec store.Secret, source string, ixSerial int64, defs map[string][]byte, serials map[string]int64) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "apps"), 0o755)
	exp := time.Now().Add(24 * time.Hour)
	ix := store.Index{Source: source, Serial: ixSerial, Expires: exp}
	for id, def := range defs {
		b, app, err := store.SealApp(source, serials[id], exp, def)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "apps", id+".json"), b, 0o644)
		os.WriteFile(filepath.Join(dir, "apps", id+".json.sig"), sec.Sign(b), 0o644)
		ix.Apps = append(ix.Apps, store.Entry{ID: id, Serial: serials[id], Name: app.Name, Group: app.Group})
	}
	b, _ := json.Marshal(ix)
	os.WriteFile(filepath.Join(dir, "index.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "index.json.sig"), sec.Sign(b), 0o644)
}

func builtInDef(t *testing.T, id string, edit func(string) string) []byte {
	t.Helper()
	b, err := os.ReadFile("../catalogue/apps/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return []byte(edit(string(b)))
}

func TestCirrusStore(t *testing.T) {
	root := t.TempDir()
	ts := httptest.NewTLSServer(http.FileServer(http.Dir(root)))
	defer ts.Close()
	offKey, offSec, _ := store.GenerateKey("cirrusstore", nil)
	extKey, extSec, _ := store.GenerateKey("extra", nil)

	// The official source has a newer Jellyfin; "extra" a new app, and an
	// app whose id Jellyfin already has.
	newJelly := builtInDef(t, "jellyfin", func(s string) string {
		return strings.Replace(s, `"description": "`, `"description": "Updated. `, 1)
	})
	publish(t, filepath.Join(root, "official"), offSec, "cirrusstore", 1, map[string][]byte{"jellyfin": newJelly}, map[string]int64{"jellyfin": 2})
	music := builtInDef(t, "navidrome", func(s string) string {
		return strings.Replace(strings.Replace(s, `"id": "navidrome"`, `"id": "music-box"`, 1), `"name": "Navidrome"`, `"name": "Music Box"`, 1)
	})
	publish(t, filepath.Join(root, "extra"), extSec, "extra", 1, map[string][]byte{"music-box": music, "jellyfin": newJelly}, map[string]int64{"music-box": 1, "jellyfin": 1})

	s, h := newTestServer(t)
	s.storeClient = ts.Client()
	snap := awakeSnap()
	snap.AppsHost = &helperapi.AppsInfo{Runtime: "podman", AppsDir: "/srv/cirrusux/apps", MediaRoots: []string{"/srv/media"}, TZ: "UTC", UID: 990, GID: 990,
		Sources: []store.Info{
			store.Source{Name: "cirrusstore", URL: ts.URL + "/official/", Keys: []store.Key{offKey}, Official: true}.Info(),
			store.Source{Name: "extra", URL: ts.URL + "/extra/", Keys: []store.Key{extKey}}.Info(),
			{Name: "broken", Error: "refused"},
		}}
	snap.Apps = []snapshot.App{{ID: "jellyfin", Name: "Jellyfin", Tag: "10.11.2", Digest: oldDigest}}
	_ = s.store.SaveSnapshot(snap)

	catalogue := func() map[string]StoreApp {
		w := do(h, "GET", "/api/catalogue", "", owner)
		var apps []StoreApp
		_ = json.Unmarshal(w.Body.Bytes(), &apps)
		m := map[string]StoreApp{}
		for _, a := range apps {
			m[a.Source+"/"+a.ID] = a
		}
		return m
	}
	if c := catalogue(); c["built-in/jellyfin"].ID == "" || c["extra/music-box"].ID != "" {
		t.Fatalf("before a check, only the built-in apps: %v", c)
	}

	w := do(h, "POST", "/api/store/refresh", "", actor)
	var v StoreView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != http.StatusOK || v.Fetched == nil || len(v.Sources) != 3 || v.Sources[0].Status != "ok" || v.Sources[1].Status != "ok" || v.Sources[2].Status != "unusable" {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	c := catalogue()
	if c["built-in/jellyfin"].ID != "" || !strings.HasPrefix(c["cirrusstore/jellyfin"].Description, "Updated.") {
		t.Error("the official Jellyfin should replace the built-in one")
	}
	if c["extra/music-box"].Name != "Music Box" || c["extra/music-box"].Unavailable != "" {
		t.Errorf("third-party app: %+v", c["extra/music-box"])
	}
	if !strings.Contains(c["extra/jellyfin"].Unavailable, "CirrusStore already has") {
		t.Errorf("id collision: %+v", c["extra/jellyfin"])
	}

	// Installing a signed app hands its envelope on; the colliding one can't.
	w = do(h, "POST", "/api/actions", `{"action":"app_install","args":{"app":"music-box","store":"extra","signed":"forged"}}`, actor)
	var id struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &id)
	cmd, ok := s.queue.Get(id.ID)
	if w.Code != http.StatusAccepted || !ok || cmd.Args["store"] != "extra" {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	var f store.Signed
	if json.Unmarshal([]byte(cmd.Args["signed"]), &f) != nil || !strings.Contains(string(f.Data), `"music-box"`) {
		t.Fatalf("signed definition not attached (or the panel's was kept): %q", cmd.Args["signed"])
	}
	if w := do(h, "POST", "/api/actions", `{"action":"app_install","args":{"app":"jellyfin","store":"extra"}}`, actor); w.Code != http.StatusBadRequest {
		t.Errorf("an unavailable app installed: %d", w.Code)
	}

	// The installed built-in Jellyfin is offered the official definition.
	w = do(h, "GET", "/api/updates", "", owner)
	var u Updates
	_ = json.Unmarshal(w.Body.Bytes(), &u)
	if len(u.Apps) != 1 || !u.Apps[0].Definition || u.Apps[0].Source != "cirrusstore" || u.Apps[0].Serial != 2 {
		t.Fatalf("definition update: %s", w.Body)
	}
	body := fmt.Sprintf(`{"action":"app_update","args":{"app":"jellyfin","store":"cirrusstore","tag":%q,"digest":%q}}`, u.Apps[0].Latest, u.Apps[0].Digest)
	w = do(h, "POST", "/api/actions", body, actor)
	_ = json.Unmarshal(w.Body.Bytes(), &id)
	if cmd, _ := s.queue.Get(id.ID); w.Code != http.StatusAccepted || cmd.Args["signed"] == "" {
		t.Fatalf("definition update: %d %s", w.Code, w.Body)
	}

	// A tampered app is left out; an older index is refused as a replay.
	os.WriteFile(filepath.Join(root, "extra", "apps", "music-box.json.sig"), offSec.Sign([]byte("x")), 0o644)
	publish(t, filepath.Join(root, "extra2"), extSec, "extra", 2, map[string][]byte{"music-box": music}, map[string]int64{"music-box": 2})
	os.WriteFile(filepath.Join(root, "extra", "index.json"), mustRead(t, filepath.Join(root, "extra2", "index.json")), 0o644)
	os.WriteFile(filepath.Join(root, "extra", "index.json.sig"), mustRead(t, filepath.Join(root, "extra2", "index.json.sig")), 0o644)
	_ = json.Unmarshal(do(h, "POST", "/api/store/refresh", "", actor).Body.Bytes(), &v)
	if v.Sources[1].Status != "ok" || !strings.Contains(v.Sources[1].Detail, "1 app(s) left out") || catalogue()["extra/music-box"].ID != "" {
		t.Errorf("tampered app: %+v", v.Sources[1])
	}
	publish(t, filepath.Join(root, "extra"), extSec, "extra", 1, map[string][]byte{"music-box": music}, map[string]int64{"music-box": 1})
	_ = json.Unmarshal(do(h, "POST", "/api/store/refresh", "", actor).Body.Bytes(), &v)
	if v.Sources[1].Status != "refused" || !strings.Contains(v.Sources[1].Detail, "older than") {
		t.Errorf("replayed index: %+v", v.Sources[1])
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
