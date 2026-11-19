package agent

import (
	"context"
	"encoding/base64"
	"fmt"

	"cirrusux/internal/access"
	"cirrusux/internal/helperapi"
)

// accountAction checks an account change again and passes it to the
// helper, which checks it a third time against the server's own files.
// A key travels as base64url, so the helper gets one plain word.
func (a *Agent) accountAction(ctx context.Context, c command, helper func(string, ...string) ([]string, error)) ([]string, error) {
	user := c.Args["user"]
	if !access.UserRe.MatchString(user) {
		return nil, fmt.Errorf("bad account name %q", user)
	}
	switch c.Action {
	case "ssh_key_add":
		line, _, err := access.NewKeyLine(c.Args["key"])
		if err != nil {
			return nil, err
		}
		return helper(helperapi.CmdSSHKeyAdd, user, base64.RawURLEncoding.EncodeToString([]byte(line)))
	case "ssh_key_remove":
		if !access.FingerprintRe.MatchString(c.Args["fingerprint"]) {
			return nil, fmt.Errorf("bad fingerprint")
		}
		return helper(helperapi.CmdSSHKeyRemove, user, c.Args["fingerprint"])
	case "account_lock":
		return helper(helperapi.CmdAccountLock, user)
	case "account_unlock":
		return helper(helperapi.CmdAccountUnlock, user)
	}
	return nil, fmt.Errorf("unknown action %q", c.Action)
}
