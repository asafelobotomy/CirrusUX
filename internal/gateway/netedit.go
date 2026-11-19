package gateway

import (
	"fmt"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/netinfo"
	"cirrusux/internal/snapshot"
)

// Network settings (the Network page): DNS and Tailscale's DNS and routes.
// A change reverts itself in two minutes unless it's kept, so a bad one
// can't lock anyone out.
func init() {
	actionSpecs["net_dns_set"] = actionSpec{
		label: func(a map[string]string) string {
			if a["servers"] == "-" {
				return "Went back to the system's own DNS settings"
			}
			return "Set DNS servers to " + a["servers"]
		},
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			st, err := netEdit(snap)
			if err != nil {
				return err
			}
			if !st.Resolved {
				return fmt.Errorf("DNS can only be set where systemd-resolved runs")
			}
			if _, _, err := netinfo.ParseDNS(a["servers"], a["search"]); err != nil {
				return err
			}
			a["prevServers"] = netinfo.JoinList(st.DNS)
			a["prevSearch"] = netinfo.JoinList(st.Search)
			return nil
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "net_dns_set", Args: map[string]string{"servers": a["prevServers"], "search": a["prevSearch"]}}
		},
	}
	actionSpecs["net_tailscale_set"] = actionSpec{
		label: func(a map[string]string) string {
			return "Tailscale: DNS from the tailnet " + onOff(a["dns"]) + ", subnet routes " + onOff(a["routes"])
		},
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			st, err := netEdit(snap)
			if err != nil {
				return err
			}
			if st.Tailscale == nil {
				return fmt.Errorf("Tailscale isn't running on this server")
			}
			for _, k := range []string{"dns", "routes"} {
				if a[k] != "yes" && a[k] != "no" {
					return fmt.Errorf("%s must be yes or no", k)
				}
			}
			a["prevDns"], a["prevRoutes"] = yesNo(st.Tailscale.AcceptDNS), yesNo(st.Tailscale.AcceptRoutes)
			return nil
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "net_tailscale_set", Args: map[string]string{"dns": a["prevDns"], "routes": a["prevRoutes"]}}
		},
	}
	actionSpecs["net_confirm"] = actionSpec{
		label: func(map[string]string) string { return "Kept the network change" },
		check: func(_ map[string]string, snap *snapshot.Snapshot) error { return pendingNet(snap, true) },
	}
	actionSpecs["net_revert"] = actionSpec{
		label: func(map[string]string) string { return "Undid the network change" },
		check: func(_ map[string]string, snap *snapshot.Snapshot) error { return pendingNet(snap, true) },
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func onOff(v string) string {
	if v == "yes" {
		return "on"
	}
	return "off"
}

func netEdit(snap *snapshot.Snapshot) (*helperapi.NetStatus, error) {
	if snap == nil || snap.NetEdit == nil {
		return nil, fmt.Errorf("the server hasn't reported its network settings (NETWORK_EDIT=yes in helper.env)")
	}
	return snap.NetEdit, nil
}

// pendingNet checks there is (want) or isn't an unconfirmed change.
func pendingNet(snap *snapshot.Snapshot, want bool) error {
	st, err := netEdit(snap)
	if err != nil {
		return err
	}
	if want && st.Pending == nil {
		return fmt.Errorf("there's no network change waiting to be kept")
	}
	return nil
}
