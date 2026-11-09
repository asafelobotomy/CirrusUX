package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
)

// Docker's container listing and inspect carry far more than the panel
// shows: inspect has every variable the container was given (passwords and
// API keys among them), mounts and settings, and the listing has each
// command line. The filter passes on only the fields below, so the
// cirrusux group can see what runs without reading its secrets.

var (
	listPath    = regexp.MustCompile(`^(/v[0-9.]+)?/containers/json$`)
	inspectPath = regexp.MustCompile(`^(/v[0-9.]+)?/containers/[A-Za-z0-9_.-]+/json$`)
)

// maxBody bounds what the filter reads to trim; Docker's own answers for a
// home server are far smaller.
const maxBody = 16 << 20

type port struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort,omitempty"`
	Type        string `json:"Type"`
}

type listed struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []port            `json:"Ports"`
}

type inspected struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		ExitCode   int    `json:"ExitCode"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
}

// trim rewrites a successful listing or inspect answer down to the fields
// above. Anything it can't parse is refused rather than passed on whole.
func trim(resp *http.Response) error {
	if resp.Request == nil || resp.Request.Method != http.MethodGet || resp.StatusCode != http.StatusOK {
		return nil
	}
	var v any
	switch p := resp.Request.URL.Path; {
	case listPath.MatchString(p):
		v = &[]listed{}
	case inspectPath.MatchString(p):
		v = &inspected{}
	default:
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	resp.Body.Close()
	if err != nil {
		return err
	}
	if len(b) > maxBody {
		return fmt.Errorf("docker answer over %d bytes", maxBody)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("docker answer: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
	resp.Header.Del("Content-Encoding")
	return nil
}
