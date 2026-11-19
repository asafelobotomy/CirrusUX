package gateway

import (
	"testing"
	"time"

	"cirrusux/internal/helperapi"
)

func TestAccountCreateRemove(t *testing.T) {
	snap := accessSnap()
	snap.Access.Users[3].CreatedByPanel = true // carol
	ok := ActionReq{Action: "account_create", Args: map[string]string{"user": "dave", "role": "user"}}
	c, spec, err := ok.Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if u := spec.undo(c.Args); u.Action != "account_remove" || u.Args["user"] != "dave" {
		t.Errorf("undo %+v", u)
	}
	for _, a := range []ActionReq{
		{Action: "account_create", Args: map[string]string{"user": "alice", "role": "user"}}, // exists
		{Action: "account_create", Args: map[string]string{"user": "Dave", "role": "user"}},  // bad name
		{Action: "account_create", Args: map[string]string{"user": "dave", "role": "root"}},  // bad role
		{Action: "account_remove", Args: map[string]string{"user": "alice"}},                 // not made here
		{Action: "account_remove", Args: map[string]string{"user": "root"}},                  // not a person
	} {
		if _, _, err := a.Validate(&snap); err == nil {
			t.Errorf("%+v should be refused", a)
		}
	}
	c, spec, err = (ActionReq{Action: "account_remove", Args: map[string]string{"user": "carol", "role": "admin"}}).Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if u := spec.undo(c.Args); u.Action != "account_create" || u.Args["role"] != "user" {
		t.Errorf("undo should recreate with the real role: %+v", u)
	}
}

func TestNetActions(t *testing.T) {
	snap := awakeSnap()
	if _, _, err := (ActionReq{Action: "net_dns_set", Args: map[string]string{"servers": "192.0.2.53", "search": "-"}}).Validate(&snap); err == nil {
		t.Error("accepted without the network-edit module")
	}
	snap.NetEdit = &helperapi.NetStatus{Resolved: true, DNS: []string{"192.0.2.1"}, Tailscale: &helperapi.TSPrefs{AcceptDNS: true}}
	c, spec, err := (ActionReq{Action: "net_dns_set", Args: map[string]string{"servers": "192.0.2.53", "search": "lan"}}).Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if u := spec.undo(c.Args); u.Args["servers"] != "192.0.2.1" || u.Args["search"] != "-" {
		t.Errorf("undo should restore the old servers: %+v", u)
	}
	c, spec, err = (ActionReq{Action: "net_tailscale_set", Args: map[string]string{"dns": "no", "routes": "yes"}}).Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if u := spec.undo(c.Args); u.Args["dns"] != "yes" || u.Args["routes"] != "no" {
		t.Errorf("tailscale undo: %+v", u)
	}
	for _, a := range []ActionReq{
		{Action: "net_dns_set", Args: map[string]string{"servers": "127.0.0.1", "search": "-"}},
		{Action: "net_tailscale_set", Args: map[string]string{"dns": "maybe", "routes": "no"}},
		{Action: "net_confirm"}, // nothing pending
	} {
		if _, _, err := a.Validate(&snap); err == nil {
			t.Errorf("%+v should be refused", a)
		}
	}
	snap.NetEdit.Pending = &helperapi.NetPending{Kind: "dns", Deadline: time.Now().Add(time.Minute)}
	for _, a := range []string{"net_confirm", "net_revert"} {
		if _, _, err := (ActionReq{Action: a}).Validate(&snap); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
}
