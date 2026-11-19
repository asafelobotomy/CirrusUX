package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckGatewayURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"http://127.0.0.1:8064/agent/snapshot":         true,
		"http://localhost:8064/agent/snapshot":         true,
		"http://[::1]:8064/agent/snapshot":             true,
		"https://gw.tail1234.ts.net/agent/snapshot":    true,
		"http://gw.tail1234.ts.net/agent/snapshot":     true,
		"http://100.101.102.103/agent/snapshot":        true,
		"http://[fd7a:115c:a1e0::1]/agent/snapshot":    true,
		"https://192.0.2.10/agent/snapshot":            true,
		"http://192.0.2.10:8064/agent/snapshot":        false,
		"http://gateway.example.org/agent/snapshot":    false,
		"http://user:pw@127.0.0.1:8064/agent/snapshot": false,
		"ftp://127.0.0.1/agent/snapshot":               false,
		"127.0.0.1:8064/agent/snapshot":                false,
		"":                                             false,
	} {
		if err := CheckGatewayURL(raw); (err == nil) != ok {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

// A redirect would carry the token along to wherever it points.
func TestAgentFollowsNoRedirect(t *testing.T) {
	var leaked bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer target.Close()
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/agent/snapshot", http.StatusTemporaryRedirect)
	}))
	defer gw.Close()
	a := New(Config{GatewayURL: gw.URL + "/agent/snapshot", Token: "secret"}, nil)
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/agent/snapshot", nil)
	req.Header.Set("Authorization", "Bearer secret")
	if _, err := a.client.Do(req); err == nil || leaked {
		t.Errorf("redirect followed: err %v, token sent on %v", err, leaked)
	}
}
