package main

import (
	"strings"
	"testing"
)

// A request's id names the job's status, queue file and unit, so it must
// be the name the agent passed: otherwise one request could overwrite
// another job's files.
func TestParseJobChecksTheID(t *testing.T) {
	req := `{"job":"aaaa1111","action":"remove","app":"navidrome"}`
	if r, err := parseJob([]byte(req), "aaaa1111"); err != nil || r.Job != "aaaa1111" {
		t.Fatalf("matching id: %+v, %v", r, err)
	}
	if _, err := parseJob([]byte(req), "bbbb2222"); err == nil || !strings.Contains(err.Error(), "carries job id") {
		t.Errorf("other job's id accepted: %v", err)
	}
	if _, err := parseJob([]byte(`{"job":"../x","action":"remove","app":"navidrome"}`), "../x"); err == nil {
		t.Error("bad id accepted")
	}
}
