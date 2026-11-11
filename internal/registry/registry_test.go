package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewest(t *testing.T) {
	cases := []struct {
		pinned string
		tags   []string
		want   string
	}{
		{"0.64.2", []string{"0.64.1", "0.64.2", "0.65.0", "0.65.1", "latest", "0.66.0-rc1", "develop"}, "0.65.1"},
		{"35.0.2-apache", []string{"35.0.3-apache", "36.0.0-fpm-alpine", "36.0.1-apache", "36.0.1", "36-apache"}, "36.0.1-apache"},
		{"12.4.20261026-211555", []string{"12.4.20261026-211555", "12.4.20261102-080000", "12.5.20261110-090000", "12.5", "2026110213", "unstable"}, "12.5.20261110-090000"},
		{"2026.10.4", []string{"2026.10.3", "2026.11.0b2", "2026.11.0", "2026.11", "dev"}, "2026.11.0"},
		{"v0.9.1", []string{"v0.9.2", "0.9.3", "v0.10.0", "v0.10.1-beta"}, "v0.10.0"},
		{"1.37.5", []string{"1.37.5-alpine", "1.37.6-alpine", "1.37.4"}, ""},
		{"2.1.7", []string{"2.1.10", "2.1.9"}, "2.1.10"},
		{"latest", []string{"1.0"}, ""},
		// LinuxServer restarts its build counter: ls329 came before ls4.
		// Only a higher version is offered, never another build of this one.
		{"4.0.20.3014-ls4", []string{"4.0.20.3014-ls329", "4.0.20.3014-ls3", "4.0.19.2979-ls324"}, ""},
		{"4.0.19.2979-ls324", []string{"4.0.19.2979-ls325", "4.0.20.3014-ls4", "4.0.20.3014-ls3"}, "4.0.20.3014-ls4"},
		{"5.3.0_v2.1.2-ls487", []string{"5.3.0_v2.1.3-ls488", "5.3.1_v2.1.2-ls490"}, "5.3.1_v2.1.2-ls490"},
	}
	for _, c := range cases {
		if got := Newest(c.pinned, c.tags); got != c.want {
			t.Errorf("Newest(%q) = %q, want %q", c.pinned, got, c.want)
		}
	}
}

// A fake registry that wants an anonymous token and pages its tag list,
// the way Docker Hub does.
func fakeRegistry(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.URL.Query().Get("scope") != "repository:acme/app:pull" || r.URL.Query().Get("service") != "fake" {
				t.Errorf("token request %s", r.URL)
			}
			fmt.Fprint(w, `{"token":"t0k"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer t0k" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fake",scope="repository:acme/app:pull"`, srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v2/acme/app/tags/list" && r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/acme/app/tags/list?last=1.1&n=1000>; rel="next"`)
			fmt.Fprint(w, `{"name":"acme/app","tags":["1.0","1.1"]}`)
		case r.URL.Path == "/v2/acme/app/tags/list":
			fmt.Fprint(w, `{"name":"acme/app","tags":["1.2","latest"]}`)
		case r.URL.Path == "/v2/acme/app/manifests/1.2" && r.Method == http.MethodHead:
			if !strings.Contains(r.Header.Get("Accept"), "image.index") {
				t.Error("no index Accept header")
			}
			w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("ab", 32))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient(t *testing.T) {
	srv := fakeRegistry(t)
	c := &Client{Base: func(host string) string {
		if host != "registry.example" {
			t.Errorf("host %q", host)
		}
		return srv.URL
	}}
	tags, err := c.Tags(context.Background(), "registry.example/acme/app")
	if err != nil || strings.Join(tags, ",") != "1.0,1.1,1.2,latest" {
		t.Fatalf("tags = %v, %v", tags, err)
	}
	d, err := c.Digest(context.Background(), "registry.example/acme/app", "1.2")
	if err != nil || d != "sha256:"+strings.Repeat("ab", 32) {
		t.Errorf("digest = %q, %v", d, err)
	}
	if _, err := c.Digest(context.Background(), "registry.example/acme/app", "9.9"); err == nil {
		t.Error("missing tag gave a digest")
	}
	if h, r := split("docker.io/library/nextcloud"); h != "registry-1.docker.io" || r != "library/nextcloud" {
		t.Errorf("split = %s %s", h, r)
	}
}

// The next page stays on the registry: the token goes with that request.
func TestNextLink(t *testing.T) {
	base := "https://registry-1.docker.io"
	for link, want := range map[string]string{
		`</v2/a/tags/list?last=x>; rel="next"`:                          base + "/v2/a/tags/list?last=x",
		`<https://registry-1.docker.io/v2/a/tags/list?n=2>; rel="next"`: base + "/v2/a/tags/list?n=2",
		`<https://evil.example/v2/a/tags/list>; rel="next"`:             "",
		`<https://registry-1.docker.io.evil.example/v2/x>; rel="next"`:  "",
		`<//evil.example/v2/a/tags/list>; rel="next"`:                   "",
		`</v2/a/tags/list?last=x>; rel="prev"`:                          "",
	} {
		if got := nextLink(base, link); got != want {
			t.Errorf("nextLink(%s) = %q, want %q", link, got, want)
		}
	}
}
