package gateway

import (
	"fmt"
	"slices"

	"cirrusux/internal/access"
	"cirrusux/internal/snapshot"
)

// Account actions (the Accounts page): SSH keys and locking. The gateway
// checks them against the accounts the agent last reported; the agent and
// the helper check them again.
func init() {
	actionSpecs["ssh_key_add"] = actionSpec{
		label: func(a map[string]string) string { return "Added an SSH key for " + a["user"] + keyNote(a["key"]) },
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			u, err := account(a, snap)
			if err != nil {
				return err
			}
			line, k, err := access.NewKeyLine(a["key"])
			if err != nil {
				return err
			}
			if slices.ContainsFunc(u.Keys, func(x access.Key) bool { return x.Fingerprint == k.Fingerprint }) {
				return fmt.Errorf("%s already has that key", u.Name)
			}
			a["key"], a["fingerprint"] = line, k.Fingerprint
			return nil
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "ssh_key_remove", Args: map[string]string{"user": a["user"], "fingerprint": a["fingerprint"], "key": a["key"]}}
		},
	}
	actionSpecs["ssh_key_remove"] = actionSpec{
		label: func(a map[string]string) string { return "Removed an SSH key from " + a["user"] + keyNote(a["key"]) },
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			u, err := account(a, snap)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(u.Keys, func(x access.Key) bool { return x.Fingerprint == a["fingerprint"] })
			if i < 0 {
				return fmt.Errorf("%s has no key %s", u.Name, a["fingerprint"])
			}
			// Kept for the undo, which puts the same key back.
			k := u.Keys[i]
			a["key"] = k.Public
			if k.Comment != "" {
				a["key"] += " " + k.Comment
			}
			return nil
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "ssh_key_add", Args: map[string]string{"user": a["user"], "key": a["key"]}}
		},
	}
	actionSpecs["account_lock"] = actionSpec{
		label: func(a map[string]string) string { return "Locked the account " + a["user"] },
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			u, err := account(a, snap)
			if err == nil && u.Locked {
				err = fmt.Errorf("%s is already locked", u.Name)
			}
			return err
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "account_unlock", Args: map[string]string{"user": a["user"]}}
		},
	}
	actionSpecs["account_unlock"] = actionSpec{
		label: func(a map[string]string) string { return "Unlocked the account " + a["user"] },
		check: func(a map[string]string, snap *snapshot.Snapshot) error {
			u, err := account(a, snap)
			if err == nil && !u.LockedByPanel {
				err = fmt.Errorf("%s wasn't locked from the panel", u.Name)
			}
			return err
		},
		undo: func(a map[string]string) *ActionReq {
			return &ActionReq{Action: "account_lock", Args: map[string]string{"user": a["user"]}}
		},
	}
}

// account finds a person's login account in the last snapshot.
func account(a map[string]string, snap *snapshot.Snapshot) (access.Account, error) {
	if snap == nil || snap.Access == nil {
		return access.Account{}, fmt.Errorf("the server hasn't reported its accounts (ACCESS=yes in helper.env)")
	}
	for _, u := range snap.Access.Users {
		if u.Name == a["user"] {
			if u.UID < 1000 || !u.CanLogin {
				return u, fmt.Errorf("%s isn't a person's login account", u.Name)
			}
			return u, nil
		}
	}
	return access.Account{}, fmt.Errorf("no account named %q", a["user"])
}

// keyNote names a key in the audit log by its comment, if it has one.
func keyNote(line string) string {
	if k, err := access.ParseKey(line); err == nil && k.Comment != "" {
		return " (" + k.Comment + ")"
	}
	return ""
}
