package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cirrusux/internal/registry"
	"cirrusux/internal/snapshot"
)

const (
	oldDigest = "sha256:38dc2727bfcfd5ede290f8ada114fc90368146f265ae4701ddddbcbe2a44ee52"
	newDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
)

func TestUpdates(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deluan/navidrome/tags/list"):
			fmt.Fprint(w, `{"tags":["0.64.2","0.65.0","0.65.1","0.66.0-rc1","latest"]}`)
		case strings.HasSuffix(r.URL.Path, "/deluan/navidrome/manifests/0.65.1"):
			w.Header().Set("Docker-Content-Digest", newDigest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer reg.Close()

	s, h := newTestServer(t)
	s.registry = &registry.Client{Base: func(string) string { return reg.URL }}
	snap := awakeSnap()
	snap.Apps = []snapshot.App{{ID: "navidrome", Name: "Navidrome", Tag: "0.64.2", Digest: oldDigest}}
	_ = s.store.SaveSnapshot(snap)

	w := do(h, "POST", "/api/updates/check", "", actor)
	var u Updates
	_ = json.Unmarshal(w.Body.Bytes(), &u)
	if w.Code != http.StatusOK || len(u.Apps) != 1 || u.Apps[0].Latest != "0.65.1" || u.Apps[0].Digest != newDigest {
		t.Fatalf("check: %d %s", w.Code, w.Body)
	}
	if got := s.store.Updates(); got.Apps[0].Latest != "0.65.1" {
		t.Error("result not kept")
	}

	post := func(tag, digest string) *httptest.ResponseRecorder {
		return do(h, "POST", "/api/actions", fmt.Sprintf(`{"action":"app_update","args":{"app":"navidrome","tag":%q,"digest":%q}}`, tag, digest), actor)
	}
	if w := post("0.66.0", newDigest); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "wasn't offered") {
		t.Errorf("unoffered version: %d %s", w.Code, w.Body)
	}
	if w := post("0.65.1", oldDigest); w.Code != http.StatusBadRequest {
		t.Errorf("offered tag with another digest: %d", w.Code)
	}
	if w := post("latest", newDigest); w.Code != http.StatusBadRequest {
		t.Errorf("latest: %d", w.Code)
	}
	w = post("0.65.1", newDigest)
	if w.Code != http.StatusAccepted {
		t.Fatalf("offered update: %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	c, _ := s.queue.Get(created.ID)
	if c.Label != "Started updating Navidrome to 0.65.1" || c.Undo == nil || c.Undo.Args["tag"] != "0.64.2" || c.Undo.Args["digest"] != oldDigest {
		t.Errorf("command = %+v undo %+v", c, c.Undo)
	}

	// After the update, going back to the previous version is allowed.
	snap.Apps[0] = snapshot.App{ID: "navidrome", Name: "Navidrome", Tag: "0.65.1", Digest: newDigest, PreviousTag: "0.64.2", PreviousDigest: oldDigest}
	_ = s.store.SaveSnapshot(snap)
	if w := post("0.64.2", oldDigest); w.Code != http.StatusAccepted {
		t.Errorf("rollback: %d %s", w.Code, w.Body)
	}
}
