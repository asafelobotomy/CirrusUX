package githost

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// The test servers are on loopback, so here loopback counts as outside and
// 192.0.2.0/24 and 10.0.0.0/8 stand for the private network.
func outsideLoopback(ip netip.Addr) bool {
	return !ip.IsLoopback() && insideNetwork(ip)
}

func TestFollowImportRefusesRedirectInside(t *testing.T) {
	h := newHost(t)
	h.InsecureTLS, h.insideFn = true, outsideLoopback
	for _, target := range []string{"https://10.0.0.1/x.git/info/refs?service=git-upload-pack", "https://192.168.1.1/x.git/info/refs", "http://example.org/x.git/info/refs"} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		}))
		err := h.Import(ctx, "copy", srv.URL+"/x.git")
		srv.Close()
		if !isKind(err, Invalid) {
			t.Errorf("redirect to %s: %v", target, err)
		}
		if strings.HasPrefix(target, "https://") && !strings.Contains(err.Error(), "CIRRUS_GIT_IMPORT_PRIVATE") {
			t.Errorf("redirect to %s: %v", target, err)
		}
	}
}

func TestFollowImportFindsFinalAddress(t *testing.T) {
	h := newHost(t)
	h.InsecureTLS, h.insideFn = true, outsideLoopback
	final := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/new/repo.git/info/refs" || r.URL.Query().Get("service") != "git-upload-pack" {
			t.Errorf("probe: %s", r.URL)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer final.Close()
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/new/repo.git/info/refs?"+r.URL.RawQuery, http.StatusMovedPermanently)
	}))
	defer first.Close()
	u, err := h.followImport(ctx, mustURL(t, first.URL+"/old/repo.git"))
	if err != nil || u.String() != final.URL+"/new/repo.git" {
		t.Errorf("followImport = %v, %v", u, err)
	}
	// Nothing listening: git gets the address as given, and says why.
	dead := mustURL(t, "https://127.0.0.1:1/x.git")
	if u, err := h.followImport(ctx, dead); err != nil || u != dead {
		t.Errorf("unreachable: %v, %v", u, err)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
