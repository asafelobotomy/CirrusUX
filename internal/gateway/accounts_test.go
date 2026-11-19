package gateway

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	"cirrusux/internal/access"
	"cirrusux/internal/snapshot"
)

func pubKey(seed byte, comment string) string {
	str := func(s string) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	pub := make([]byte, 32)
	pub[0] = seed
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(str("ssh-ed25519"), str(string(pub))...)) + " " + comment
}

func accessSnap() snapshot.Snapshot {
	s := awakeSnap()
	k, _ := access.ParseKey(pubKey(1, "alice@laptop"))
	s.Access = &access.Status{Users: []access.Account{
		{Name: "root", UID: 0, CanLogin: true, Admin: true},
		{Name: "alice", UID: 1000, CanLogin: true, Admin: true, Keys: []access.Key{k}},
		{Name: "bob", UID: 1001, CanLogin: true, Locked: true, LockedByPanel: true},
		{Name: "carol", UID: 1002, CanLogin: true, Locked: true},
	}}
	return s
}

func TestAccountActions(t *testing.T) {
	snap := accessSnap()
	k, _ := access.ParseKey(pubKey(1, "alice@laptop"))

	add := ActionReq{Action: "ssh_key_add", Args: map[string]string{"user": "alice", "key": pubKey(2, "alice@phone"), "fingerprint": "spoofed"}}
	c, spec, err := add.Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := access.ParseKey(pubKey(2, ""))
	if c.Args["fingerprint"] != k2.Fingerprint {
		t.Errorf("the fingerprint should come from the key: %q", c.Args["fingerprint"])
	}
	if u := spec.undo(c.Args); u.Action != "ssh_key_remove" || u.Args["fingerprint"] != k2.Fingerprint {
		t.Errorf("undo %+v", u)
	}

	rm := ActionReq{Action: "ssh_key_remove", Args: map[string]string{"user": "alice", "fingerprint": k.Fingerprint}}
	c, spec, err = rm.Validate(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if u := spec.undo(c.Args); u.Action != "ssh_key_add" || u.Args["key"] != pubKey(1, "alice@laptop") {
		t.Errorf("undo should put the same key back: %+v", u)
	}

	bad := []ActionReq{
		{Action: "ssh_key_add", Args: map[string]string{"user": "alice", "key": pubKey(1, "again")}},              // already there
		{Action: "ssh_key_add", Args: map[string]string{"user": "root", "key": pubKey(3, "")}},                    // not a person
		{Action: "ssh_key_add", Args: map[string]string{"user": "alice", "key": `command="sh" ` + pubKey(3, "")}}, // options
		{Action: "ssh_key_remove", Args: map[string]string{"user": "alice", "fingerprint": "SHA256:nope"}},
		{Action: "account_lock", Args: map[string]string{"user": "bob"}},     // already locked
		{Action: "account_unlock", Args: map[string]string{"user": "carol"}}, // not the panel's lock
		{Action: "account_lock", Args: map[string]string{"user": "mallory"}},
	}
	for _, a := range bad {
		if _, _, err := a.Validate(&snap); err == nil {
			t.Errorf("%+v should be refused", a)
		}
	}
	for _, a := range []ActionReq{
		{Action: "account_lock", Args: map[string]string{"user": "alice"}},
		{Action: "account_unlock", Args: map[string]string{"user": "bob"}},
	} {
		if _, _, err := a.Validate(&snap); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	// A key with options can't be put back as it was, so removing it has no undo.
	restricted := accessSnap()
	rk, _ := access.ParseKey(pubKey(5, "ci"))
	rk.Options = true
	restricted.Access.Users[1].Keys = append(restricted.Access.Users[1].Keys, rk)
	c, spec, err = (ActionReq{Action: "ssh_key_remove", Args: map[string]string{"user": "alice", "fingerprint": rk.Fingerprint, "key": "ssh-ed25519 AAAA forged"}}).Validate(&restricted)
	if err != nil {
		t.Fatal(err)
	}
	if spec.undo(c.Args) != nil || c.Args["key"] != "" {
		t.Errorf("a key with options got an undo: %+v", c.Args)
	}
	noAccess := awakeSnap()
	if _, _, err := (ActionReq{Action: "account_lock", Args: map[string]string{"user": "alice"}}).Validate(&noAccess); err == nil {
		t.Error("accepted without the access module")
	}
}
