package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func trimmed(t *testing.T, method, path, body string) (string, error) {
	t.Helper()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": {"999"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: httptest.NewRequest(method, path, nil)}
	if err := trim(resp); err != nil {
		return "", err
	}
	b, _ := io.ReadAll(resp.Body)
	if strings.Contains(path, "/json") && (resp.ContentLength != int64(len(b)) || resp.Header.Get("Content-Length") == "999") {
		t.Errorf("content length %d for %d bytes", resp.ContentLength, len(b))
	}
	return string(b), nil
}

// Inspect keeps the container's state and drops its variables, mounts and
// settings.
func TestTrimInspect(t *testing.T) {
	body := `{"Id":"abc","Name":"/nextcloud","State":{"Status":"running","Running":true,"StartedAt":"2026-11-07T10:00:00Z","Health":{"Log":[{"Output":"x"}]}},
		"Config":{"Env":["POSTGRES_PASSWORD=hunter2","PATH=/bin"],"Cmd":["--token","s3cret"]},"Mounts":[{"Source":"/srv"}],"HostConfig":{"Privileged":false}}`
	out, err := trimmed(t, http.MethodGet, "/v1.47/containers/abc/json", body)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "s3cret", "Env", "Mounts", "HostConfig", "Health"} {
		if strings.Contains(out, leak) {
			t.Errorf("inspect still carries %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, `"StartedAt":"2026-11-07T10:00:00Z"`) || !strings.Contains(out, `"Name":"/nextcloud"`) {
		t.Errorf("inspect lost what the agent reads: %s", out)
	}
}

// The listing keeps names, image, state, labels and ports, and drops the
// command line and mounts.
func TestTrimList(t *testing.T) {
	body := `[{"Id":"abc","Names":["/kavita"],"Image":"kavita:0.8","Command":"run --password s3cret","State":"running","Status":"Up 2 hours",
		"Labels":{"io.cirrusux.version":"0.8"},"Ports":[{"IP":"0.0.0.0","PrivatePort":5000,"PublicPort":5000,"Type":"tcp"}],"Mounts":[{"Source":"/srv"}]}]`
	out, err := trimmed(t, http.MethodGet, "/containers/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "s3cret") || strings.Contains(out, "Mounts") {
		t.Errorf("listing still carries the command or mounts: %s", out)
	}
	for _, keep := range []string{`"Names":["/kavita"]`, `"io.cirrusux.version":"0.8"`, `"PublicPort":5000`, `"Status":"Up 2 hours"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("listing lost %s: %s", keep, out)
		}
	}
}

// An answer the filter can't read is refused, not passed on whole; other
// paths pass through untouched.
func TestTrimRefusesWhatItCantRead(t *testing.T) {
	if _, err := trimmed(t, http.MethodGet, "/containers/abc/json", `not json {"Env":["A=1"]}`); err == nil {
		t.Error("unparsable inspect passed on")
	}
	if out, err := trimmed(t, http.MethodGet, "/_ping", "OK"); err != nil || out != "OK" {
		t.Errorf("ping: %q, %v", out, err)
	}
}

func TestLogsNeedABoundedTail(t *testing.T) {
	for target, want := range map[string]bool{
		"/containers/jellyfin/logs?stdout=1":           false,
		"/containers/jellyfin/logs?tail=all":           false,
		"/containers/jellyfin/logs?tail=6000":          false,
		"/containers/jellyfin/logs?tail=-1":            false,
		"/containers/jellyfin/logs?tail=1000&stdout=1": true,
	} {
		if allow(httptest.NewRequest(http.MethodGet, target, nil)) != want {
			t.Errorf("%s: allowed = %v", target, !want)
		}
	}
}
