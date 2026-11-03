package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogs(t *testing.T) {
	for target, want := range map[string]bool{
		"/v1.43/containers/jellyfin/logs?stdout=1&stderr=1&tail=200": true,
		"/containers/jellyfin/logs?follow=0&tail=10":                 true,
		"/containers/jellyfin/logs?follow=1":                         false,
		"/containers/jellyfin/logs?follow=true":                      false,
	} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if allow(r) != want {
			t.Errorf("%s: allowed = %v", target, !want)
		}
	}
}

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
		"GET /containers/abc/logs?follow=1",
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
		"POST /v1.23/containers/abc/start",
		"POST /v1.9/containers/abc/start",
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
	withBody := req("POST /containers/abc/start")
	withBody.ContentLength = 20
	if allow(withBody) {
		t.Error("a POST with a body should be denied")
	}
}

func req(s string) *http.Request {
	method, target, _ := strings.Cut(s, " ")
	return httptest.NewRequest(method, target, nil)
}
