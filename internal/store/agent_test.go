package store

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAgent is an SSH agent holding keys. refuse makes it answer every
// signing request with a failure, as a locked agent or a declined prompt
// does; corrupt makes it sign with the wrong key.
type fakeAgent struct {
	keys    []ed25519.PrivateKey
	refuse  bool
	corrupt bool
}

func (f *fakeAgent) serve(t *testing.T) string {
	t.Helper()
	// A short path: unix socket paths are limited to about 100 bytes.
	sock := filepath.Join(t.TempDir(), "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	return sock
}

func (f *fakeAgent) handle(c net.Conn) {
	defer c.Close()
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	msg := make([]byte, binary.BigEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(c, msg); err != nil {
		return
	}
	reply := func(typ byte, body []byte) {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(body)+1))
		c.Write(append(append(out, typ), body...))
	}
	switch msg[0] {
	case agentRequestIDs:
		body := binary.BigEndian.AppendUint32(nil, uint32(len(f.keys)))
		for _, k := range f.keys {
			body = appendString(body, sshPublicBlob(k.Public().(ed25519.PublicKey)))
			body = appendString(body, []byte("test key"))
		}
		reply(agentIdentitiesReply, body)
	case agentSignRequest:
		blob, rest, _ := readString(msg[1:])
		data, _, _ := readString(rest)
		if f.refuse {
			reply(agentFailure, nil)
			return
		}
		for _, k := range f.keys {
			if string(sshPublicBlob(k.Public().(ed25519.PublicKey))) != string(blob) {
				continue
			}
			if f.corrupt {
				_, k, _ = ed25519.GenerateKey(nil)
			}
			sig := appendString(appendString(nil, []byte(sshEd25519)), ed25519.Sign(k, data))
			reply(agentSignResponse, appendString(nil, sig))
			return
		}
		reply(agentFailure, nil)
	default:
		reply(agentFailure, nil)
	}
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func keyOf(k ed25519.PrivateKey) Key {
	return Key{Name: "cirrusstore", Pub: k.Public().(ed25519.PublicKey)}
}

func TestAgentSigns(t *testing.T) {
	mine, other := newKey(t), newKey(t)
	sock := (&fakeAgent{keys: []ed25519.PrivateKey{other, mine}}).serve(t)

	a, err := NewAgentSigner(sock, keyOf(mine))
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"source":"cirrusstore"}`)
	sig, err := a.SignBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	// A server checks it exactly as it checks a file-key signature.
	raw, err := ParseSig(sig)
	if err != nil || !ed25519.Verify(keyOf(mine).Pub, data, raw) {
		t.Fatalf("the signature doesn't verify: %v", err)
	}
	if want := base64.StdEncoding.EncodeToString(ed25519.Sign(mine, data)) + "\n"; string(sig) != want {
		t.Fatal("an agent's Ed25519 signature should equal the file key's")
	}
}

func TestAgentRefusals(t *testing.T) {
	mine, other := newKey(t), newKey(t)

	sock := (&fakeAgent{keys: []ed25519.PrivateKey{other}}).serve(t)
	if _, err := NewAgentSigner(sock, keyOf(mine)); err == nil || !strings.Contains(err.Error(), "doesn't offer the key") {
		t.Fatalf("a key the agent lacks was accepted: %v", err)
	}

	sock = (&fakeAgent{keys: []ed25519.PrivateKey{mine}, refuse: true}).serve(t)
	a, err := NewAgentSigner(sock, keyOf(mine))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignBytes([]byte("x")); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a refusal was ignored: %v", err)
	}

	sock = (&fakeAgent{keys: []ed25519.PrivateKey{mine}, corrupt: true}).serve(t)
	a, err = NewAgentSigner(sock, keyOf(mine))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignBytes([]byte("x")); err == nil || !strings.Contains(err.Error(), "doesn't verify") {
		t.Fatalf("a signature by the wrong key was accepted: %v", err)
	}

	if _, err := NewAgentSigner("", keyOf(mine)); err == nil {
		t.Fatal("no socket was accepted")
	}
	if _, err := NewAgentSigner(filepath.Join(t.TempDir(), "none"), keyOf(mine)); err == nil {
		t.Fatal("a missing socket was accepted")
	}
}

func TestKeyFromSSH(t *testing.T) {
	k := newKey(t)
	pub := k.Public().(ed25519.PublicKey)
	line := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(sshPublicBlob(pub)) + " me@host\n"
	got, err := KeyFromSSH("cirrusstore", line)
	if err != nil || string(got.Pub) != string(pub) || got.Name != "cirrusstore" {
		t.Fatalf("KeyFromSSH = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "ssh-rsa AAAA x", "ssh-ed25519 !!!", "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("junk"))} {
		if _, err := KeyFromSSH("cirrusstore", bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := KeyFromSSH("Bad Name", line); err == nil {
		t.Error("a bad name was accepted")
	}
}
