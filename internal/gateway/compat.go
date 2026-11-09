package gateway

import (
	"slices"

	"cirrusux/internal/snapshot"
)

// In a split setup the gateway and the server's agent are upgraded
// separately. The agent says in each snapshot which protocol it speaks and
// which actions it runs; the gateway uses that to refuse an action the agent
// would answer with "unknown action", and to tell the panel what to upgrade.

// Compat is what /api/state says about how the two sides fit. The agent's own
// protocol and actions are in the snapshot.
type Compat struct {
	// Protocol is the gateway's own (snapshot.Protocol).
	Protocol int `json:"protocol"`
	// AgentTooOld: the agent speaks an older protocol. GatewayTooOld: a newer one.
	AgentTooOld   bool `json:"agentTooOld,omitempty"`
	GatewayTooOld bool `json:"gatewayTooOld,omitempty"`
}

// compatOf compares the latest snapshot with this gateway. An agent from
// before the protocol field existed reports nothing, so it is not flagged.
// protocol is the gateway's own (snapshot.Protocol outside tests).
func compatOf(protocol int, snap *snapshot.Snapshot) Compat {
	c := Compat{Protocol: protocol}
	if snap == nil || snap.Protocol == 0 {
		return c
	}
	c.AgentTooOld = snap.Protocol < protocol
	c.GatewayTooOld = snap.Protocol > protocol
	return c
}

const upgradeCmd = "sudo cirrusux-setup upgrade"

// agentRefuses says why an action can't go to the agent, or "" if it can.
func (s *Server) agentRefuses(action string, snap *snapshot.Snapshot) string {
	c := compatOf(s.protocol, snap)
	switch {
	case c.GatewayTooOld:
		return "The gateway's CirrusUX is too old for " + s.cfg.ServerName + "'s: update it on " + s.cfg.GatewayName + " (" + upgradeCmd + "), then try again."
	case c.AgentTooOld:
		return "The server's CirrusUX is too old for this: update it there (" + upgradeCmd + "), then try again."
	case snap != nil && len(snap.Actions) > 0 && !slices.Contains(snap.Actions, action):
		return "The server's CirrusUX is too old for this: update it there (" + upgradeCmd + "), then try again."
	}
	return ""
}
