package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllow(t *testing.T) {
	ok := []string{
		"POST /containers/kavita/restart",
		"POST /v1.47/containers/abc/stop?t=10",
		"POST /containers/abc/start",
		"GET /containers/json?all=1",
		"GET /v1.47/containers/json",
		"GET /containers/abc123def456/json",
		"GET /_ping",
	}
	denied := []string{
		"POST /containers/create",
		"GET /containers/abc/logs",
		"GET /containers/abc/archive",
		"GET /images/json",
		"DELETE /containers/abc",
		"GET /containers/../images/json",
		"POST /v1.47/containers/abc/exec",
		"POST /containers/abc/kill",
		"POST /containers/abc/pause",
		"POST /containers/abc/update",
		"POST /containers/abc/restart?signal=KILL",
		"POST /containers/../images/create/start",
		"PUT /containers/abc/archive",
	}
	for _, s := range ok {
		if !allow(req(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
	for _, s := range denied {
		if allow(req(s)) {
			t.Errorf("%s should be denied", s)
		}
	}
}

func req(s string) *http.Request {
	method, target, _ := strings.Cut(s, " ")
	return httptest.NewRequest(method, target, nil)
}
