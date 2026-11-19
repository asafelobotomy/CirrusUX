package store

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Signer makes the .sig file for some bytes. A Secret signs with a key
// held in a file; an AgentSigner asks an SSH agent, so the key never has
// to leave the password manager that holds it.
type Signer interface {
	Public() Key
	SignBytes(data []byte) ([]byte, error)
}

// SignBytes is Sign with the Signer interface's shape.
func (s Secret) SignBytes(data []byte) ([]byte, error) { return s.Sign(data), nil }

// agentTimeout is how long one request to the agent may take. An agent may
// wait for its owner to approve each signature.
const agentTimeout = 2 * time.Minute

// SSH agent protocol (draft-miller-ssh-agent): the messages used here.
const (
	agentFailure         = 5
	agentRequestIDs      = 11
	agentIdentitiesReply = 12
	agentSignRequest     = 13
	agentSignResponse    = 14
	sshEd25519           = "ssh-ed25519"
)

// AgentSigner signs with an Ed25519 key held by an SSH agent.
type AgentSigner struct {
	sock string
	key  Key
	blob []byte
}

// NewAgentSigner connects to the agent at the unix socket sock and checks
// that it offers key. Nothing is signed yet.
func NewAgentSigner(sock string, key Key) (*AgentSigner, error) {
	if sock == "" {
		return nil, errors.New("no SSH agent: SSH_AUTH_SOCK is empty")
	}
	a := &AgentSigner{sock: sock, key: key, blob: sshPublicBlob(key.Pub)}
	body, err := a.call(agentRequestIDs, nil, agentIdentitiesReply)
	if err != nil {
		return nil, err
	}
	n, body, ok := readUint32(body)
	if !ok || n > 1024 {
		return nil, errors.New("SSH agent: unreadable list of keys")
	}
	var offered []string
	for i := uint32(0); i < n; i++ {
		var blob []byte
		if blob, body, ok = readString(body); !ok {
			return nil, errors.New("SSH agent: unreadable list of keys")
		}
		if _, body, ok = readString(body); !ok { // the comment
			return nil, errors.New("SSH agent: unreadable list of keys")
		}
		if string(blob) == string(a.blob) {
			return a, nil
		}
		if pub, err := parseSSHBlob(blob); err == nil {
			offered = append(offered, Key{Pub: pub}.Fingerprint())
		}
	}
	return nil, fmt.Errorf("the SSH agent doesn't offer the key %s (it offers %d Ed25519 key(s): %s)",
		key.Fingerprint(), len(offered), strings.Join(offered, ", "))
}

// Public is the key the agent signs with.
func (a *AgentSigner) Public() Key { return a.key }

// SignBytes asks the agent for an Ed25519 signature and checks it against
// the public key before returning it, so a wrong key or a bad agent can't
// produce a file a server would refuse.
func (a *AgentSigner) SignBytes(data []byte) ([]byte, error) {
	req := appendString(nil, a.blob)
	req = appendString(req, data)
	req = binary.BigEndian.AppendUint32(req, 0) // no flags: plain Ed25519
	body, err := a.call(agentSignRequest, req, agentSignResponse)
	if err != nil {
		return nil, err
	}
	sig, _, ok := readString(body)
	if !ok {
		return nil, errors.New("SSH agent: unreadable signature")
	}
	format, rest, ok := readString(sig)
	if !ok || string(format) != sshEd25519 {
		return nil, errors.New("SSH agent: not an Ed25519 signature")
	}
	raw, _, ok := readString(rest)
	if !ok || len(raw) != ed25519.SignatureSize || !ed25519.Verify(a.key.Pub, data, raw) {
		return nil, errors.New("SSH agent: the signature doesn't verify against the key")
	}
	return []byte(base64.StdEncoding.EncodeToString(raw) + "\n"), nil
}

// call sends one request and returns the reply's body, which must be of
// type want.
func (a *AgentSigner) call(typ byte, payload []byte, want byte) ([]byte, error) {
	c, err := net.DialTimeout("unix", a.sock, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("SSH agent: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(agentTimeout))
	msg := binary.BigEndian.AppendUint32(nil, uint32(len(payload)+1))
	msg = append(msg, typ)
	msg = append(msg, payload...)
	if _, err := c.Write(msg); err != nil {
		return nil, fmt.Errorf("SSH agent: %v", err)
	}
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return nil, fmt.Errorf("SSH agent: %v", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n < 1 || n > 256*1024 {
		return nil, errors.New("SSH agent: reply of an odd size")
	}
	reply := make([]byte, n)
	if _, err := io.ReadFull(c, reply); err != nil {
		return nil, fmt.Errorf("SSH agent: %v", err)
	}
	switch reply[0] {
	case want:
		return reply[1:], nil
	case agentFailure:
		return nil, errors.New("SSH agent: refused (locked, or the request was declined)")
	}
	return nil, fmt.Errorf("SSH agent: unexpected reply %d", reply[0])
}

// KeyFromSSH turns an OpenSSH public key line ("ssh-ed25519 AAAA… comment")
// into a CirrusStore key with the given name.
func KeyFromSSH(name, line string) (Key, error) {
	if !ValidName(name) {
		return Key{}, fmt.Errorf("key name %q: lowercase letters, digits and -", name)
	}
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != sshEd25519 {
		return Key{}, errors.New(`not an Ed25519 public key: expected "ssh-ed25519 <base64> [comment]"`)
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return Key{}, errors.New("public key: not base64")
	}
	pub, err := parseSSHBlob(blob)
	if err != nil {
		return Key{}, err
	}
	return Key{Name: name, Pub: pub}, nil
}

func sshPublicBlob(pub ed25519.PublicKey) []byte {
	return appendString(appendString(nil, []byte(sshEd25519)), pub)
}

func parseSSHBlob(blob []byte) (ed25519.PublicKey, error) {
	typ, rest, ok := readString(blob)
	if !ok || string(typ) != sshEd25519 {
		return nil, errors.New("not an Ed25519 key")
	}
	pub, rest, ok := readString(rest)
	if !ok || len(pub) != ed25519.PublicKeySize || len(rest) != 0 {
		return nil, errors.New("not an Ed25519 key")
	}
	return ed25519.PublicKey(pub), nil
}

func appendString(b, s []byte) []byte {
	return append(binary.BigEndian.AppendUint32(b, uint32(len(s))), s...)
}

func readUint32(b []byte) (uint32, []byte, bool) {
	if len(b) < 4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(b), b[4:], true
}

func readString(b []byte) ([]byte, []byte, bool) {
	n, b, ok := readUint32(b)
	if !ok || uint64(n) > uint64(len(b)) {
		return nil, nil, false
	}
	return b[:n], b[n:], true
}
