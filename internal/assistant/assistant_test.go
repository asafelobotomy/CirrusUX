package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake chat API: first asks for a tool, then answers. It records
// every request body so the test can check nothing sensitive was sent.
func TestAskToolLoopAndRedaction(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[
				{"id":"1","type":"function","function":{"name":"backups","arguments":"{}"}},
				{"id":"2","type":"function","function":{"name":"propose_wake","arguments":"{\"label\":\"Wake homeserver\"}"}}]}}],
				"usage":{"prompt_tokens":100,"completion_tokens":10}}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Backup OK."}}],"usage":{"prompt_tokens":50,"completion_tokens":5}}`)
	}))
	defer srv.Close()

	c := &Client{Base: srv.URL, Model: "test", Key: "k"}
	tools := []Tool{
		{Name: "backups", Description: "backups", Run: func(json.RawMessage) (any, error) {
			return map[string]string{"file": "/mnt/media/BackUp/x.age", "client": "192.0.2.23", "env": "TMM_PASSWORD=hunter2"}, nil
		}},
		{Name: "propose_wake", Description: "Wake homeserver", Proposal: true, Action: "wake"},
	}
	reply, err := c.Ask(context.Background(), "system", []Turn{{Role: "user", Text: "my mac is 02:00:5e:00:00:01"}}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Backup OK." || reply.Proposal == nil || reply.Proposal.Action != "wake" || reply.Proposal.Label != "Wake homeserver" {
		t.Errorf("reply = %+v", reply)
	}
	if reply.TokensIn != 150 || reply.TokensOut != 15 {
		t.Errorf("tokens = %d/%d", reply.TokensIn, reply.TokensOut)
	}
	all := strings.Join(bodies, "\n")
	for _, leak := range []string{"hunter2", "192.0.2.23", "/mnt/media", "02:00:5e"} {
		if strings.Contains(all, leak) {
			t.Errorf("request to the API contained %q", leak)
		}
	}
}
