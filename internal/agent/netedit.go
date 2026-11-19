package agent

import (
	"context"
	"fmt"

	"cirrusux/internal/helperapi"
	"cirrusux/internal/netinfo"
)

// netAction checks a network change again and passes it to the helper,
// which checks it a third time and arranges the automatic undo.
func (a *Agent) netAction(ctx context.Context, c command, helper func(string, ...string) ([]string, error)) ([]string, error) {
	switch c.Action {
	case "net_dns_set":
		if _, _, err := netinfo.ParseDNS(c.Args["servers"], c.Args["search"]); err != nil {
			return nil, err
		}
		return helper(helperapi.CmdNetDNS, c.Args["servers"], c.Args["search"])
	case "net_tailscale_set":
		for _, k := range []string{"dns", "routes"} {
			if v := c.Args[k]; v != "yes" && v != "no" {
				return nil, fmt.Errorf("%s must be yes or no", k)
			}
		}
		return helper(helperapi.CmdNetTailscale, c.Args["dns"], c.Args["routes"])
	case "net_confirm":
		return helper(helperapi.CmdNetConfirm)
	case "net_revert":
		return helper(helperapi.CmdNetRevert)
	}
	return nil, fmt.Errorf("unknown action %q", c.Action)
}
