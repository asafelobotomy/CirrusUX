package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/store"
)

func TestPreviewSource(t *testing.T) {
	root := t.TempDir()
	ts := httptest.NewTLSServer(http.FileServer(http.Dir(root)))
	defer ts.Close()
	key, sec, _ := store.GenerateKey("friends", nil)
	other, _, _ := store.GenerateKey("friends", nil)

	music := builtInDef(t, "navidrome", func(s string) string {
		return strings.Replace(strings.Replace(s, `"id": "navidrome"`, `"id": "music-box"`, 1), `"name": "Navidrome"`, `"name": "Music Box"`, 1)
	})
	jelly := builtInDef(t, "jellyfin", func(s string) string { return s })
	publish(t, filepath.Join(root, "friends"), sec, "friends", 3,
		map[string][]byte{"music-box": music, "jellyfin": jelly, "broken": music},
		map[string]int64{"music-box": 1, "jellyfin": 1, "broken": 1})
	// broken's envelope holds music-box: it's left out, as a check would.

	s, h := newTestServer(t)
	s.storeClient = ts.Client()
	snap := awakeSnap()
	snap.AppsHost = &helperapi.AppsInfo{Runtime: "podman", AppsDir: "/srv/cirrusux/apps", TZ: "UTC",
		Sources: []store.Info{{Name: "taken", URL: "https://example.org/", Keys: []string{key.String()}}}}
	_ = s.store.SaveSnapshot(snap)

	b64 := base64.StdEncoding.EncodeToString(key.Pub)
	preview := func(name, url, k string) (int, SourcePreview, string) {
		body, _ := json.Marshal(SourcePreviewRequest{Name: name, URL: url, Key: k})
		w := do(h, "POST", "/api/store/preview", string(body), actor)
		var p SourcePreview
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		return w.Code, p, w.Body.String()
	}

	code, p, body := preview("friends", ts.URL+"/friends", b64)
	if code != http.StatusOK {
		t.Fatalf("preview: %d %s", code, body)
	}
	if p.Fingerprint != key.Fingerprint() || p.Entry != fmt.Sprintf("friends|%s/friends/|%s", ts.URL, b64) || len(p.Apps) != 3 {
		t.Fatalf("preview: %+v", p)
	}
	apps := map[string]PreviewApp{}
	for _, a := range p.Apps {
		apps[a.ID] = a
	}
	if a := apps["music-box"]; a.Problem != "" || a.Name != "Music Box" || a.Image == "" {
		t.Errorf("music-box: %+v", a)
	}
	if a := apps["jellyfin"]; !strings.Contains(a.Problem, "already has an app") {
		t.Errorf("jellyfin should collide with the built-in one: %+v", a)
	}
	if a := apps["broken"]; !strings.HasPrefix(a.Problem, "left out") {
		t.Errorf("broken should be left out: %+v", a)
	}
	// The full key line works too, and nothing was cached.
	if code, _, body := preview("friends", ts.URL+"/friends/", key.String()); code != http.StatusOK {
		t.Errorf("key line: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(s.storeDir(), "friends")); !os.IsNotExist(err) {
		t.Error("a preview must not cache the catalogue")
	}

	for _, c := range []struct {
		name, url, key string
		code           int
	}{
		{"friends", ts.URL + "/friends/", base64.StdEncoding.EncodeToString(other.Pub), http.StatusUnprocessableEntity}, // wrong key
		{"other", ts.URL + "/friends/", b64, http.StatusUnprocessableEntity},                                            // index names another source
		{"cirrusstore", ts.URL + "/friends/", b64, http.StatusBadRequest},
		{"Friends", ts.URL + "/friends/", b64, http.StatusBadRequest},
		{"friends", "http://example.org/", b64, http.StatusBadRequest},
		{"friends", ts.URL + "/friends/", "short", http.StatusBadRequest},
		{"taken", ts.URL + "/friends/", b64, http.StatusConflict},
	} {
		if code, _, body := preview(c.name, c.url, c.key); code != c.code {
			t.Errorf("%s %s %.10s: %d %s, want %d", c.name, c.url, c.key, code, body, c.code)
		}
	}
}
