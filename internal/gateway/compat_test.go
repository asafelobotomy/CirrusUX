package gateway

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"cirrusux/internal/agent"
	"cirrusux/internal/snapshot"
)

func TestActionRefusedByOlderAgent(t *testing.T) {
	s, h := newTestServer(t)
	snap := awakeSnap()
	snap.Protocol, snap.Actions = snapshot.Protocol, []string{"stay_awake", "allow_sleep"}
	_ = s.store.SaveSnapshot(snap)
	w := do(h, "POST", "/api/actions", `{"action":"backup_run"}`, actor)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "server's CirrusUX is too old") {
		t.Errorf("unlisted action: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/actions", `{"action":"allow_sleep"}`, actor); w.Code != http.StatusAccepted {
		t.Errorf("listed action: %d %s", w.Code, w.Body)
	}
}

func TestAgentWithoutActionsListAllowsAll(t *testing.T) {
	s, h := newTestServer(t)
	_ = s.store.SaveSnapshot(awakeSnap()) // no protocol, no actions: an older agent
	if w := do(h, "POST", "/api/actions", `{"action":"backup_run"}`, actor); w.Code != http.StatusAccepted {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestProtocolMismatchRefusesActionsNotReads(t *testing.T) {
	for _, tc := range []struct {
		protocol int
		want     string
	}{
		{snapshot.Protocol + 1, "gateway's CirrusUX is too old"},
		{snapshot.Protocol - 1, "server's CirrusUX is too old"},
	} {
		if tc.protocol < 1 {
			continue // protocol 0 means "not reported"; an older agent needs protocol 2 or more here
		}
		s, h := newTestServer(t)
		snap := awakeSnap()
		snap.Protocol, snap.Actions = tc.protocol, agent.Actions()
		_ = s.store.SaveSnapshot(snap)
		w := do(h, "POST", "/api/actions", `{"action":"allow_sleep"}`, actor)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("protocol %d: %d %s", tc.protocol, w.Code, w.Body)
		}
		w = do(h, "GET", "/api/state", "", owner)
		var st struct{ Compat Compat }
		_ = json.Unmarshal(w.Body.Bytes(), &st)
		if w.Code != http.StatusOK || st.Compat.AgentTooOld == st.Compat.GatewayTooOld {
			t.Errorf("state: %d %+v", w.Code, st.Compat)
		}
	}
}

// Every action the gateway can send to the agent is one the agent lists, so a
// current agent never trips the check above.
func TestQueuedActionsAreAgentActions(t *testing.T) {
	listed := agent.Actions()
	for name, spec := range actionSpecs {
		if spec.local == nil && !slices.Contains(listed, name) {
			t.Errorf("the gateway can queue %q but the agent doesn't list it", name)
		}
	}
}

// With the gateway claiming a newer protocol than the agent's, the agent is
// the one that is too old.
func TestAgentTooOldWhenGatewayIsAhead(t *testing.T) {
	s, h := newTestServer(t)
	s.protocol = 3
	snap := awakeSnap()
	snap.Protocol, snap.Actions = 2, agent.Actions()
	_ = s.store.SaveSnapshot(snap)
	w := do(h, "POST", "/api/actions", `{"action":"allow_sleep"}`, actor)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "The server's CirrusUX is too old for this") {
		t.Errorf("action: %d %s", w.Code, w.Body)
	}
	w = do(h, "GET", "/api/state", "", owner)
	var st struct{ Compat Compat }
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != http.StatusOK || !st.Compat.AgentTooOld || st.Compat.GatewayTooOld || st.Compat.Protocol != 3 {
		t.Errorf("state: %d %+v", w.Code, st.Compat)
	}
}

// An agent that reports no protocol (protocol 0) is older than the field
// itself and is not flagged, however far ahead the gateway is.
func TestAgentWithoutProtocolNotFlagged(t *testing.T) {
	s, h := newTestServer(t)
	s.protocol = 3
	_ = s.store.SaveSnapshot(awakeSnap())
	if w := do(h, "POST", "/api/actions", `{"action":"allow_sleep"}`, actor); w.Code != http.StatusAccepted {
		t.Errorf("action: %d %s", w.Code, w.Body)
	}
	w := do(h, "GET", "/api/state", "", owner)
	var st struct{ Compat Compat }
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st.Compat.AgentTooOld || st.Compat.GatewayTooOld {
		t.Errorf("flagged: %+v", st.Compat)
	}
}
