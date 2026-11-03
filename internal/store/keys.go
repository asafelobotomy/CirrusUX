// Package store is CirrusStore's signed catalogue: the keys a source signs
// with, the sources a server trusts, and the files a source publishes (a
// signed index and one signed envelope per app).
//
// Every file is signed byte for byte with Ed25519. Before an app is shown
// or installed its envelope must carry a signature from a key trusted for
// the source it came from, name that source, not have expired, and not be
// older than a version already seen, so an old file can't be replayed.
package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// nameRe is a source's name, and a key's.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

// ValidName reports whether s can name a source or a key.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// Key is one public key: "cirrusstore-key v1 <name> <base64>".
type Key struct {
	Name string
	Pub  ed25519.PublicKey
}

// ParseKey reads a public key line.
func ParseKey(s string) (Key, error) {
	f := strings.Fields(s)
	if len(f) != 4 || f[0] != "cirrusstore-key" || f[1] != "v1" {
		return Key{}, errors.New(`a key is "cirrusstore-key v1 <name> <base64>"`)
	}
	if !ValidName(f[2]) {
		return Key{}, fmt.Errorf("key name %q: lowercase letters, digits and -", f[2])
	}
	b, err := base64.StdEncoding.DecodeString(f[3])
	if err != nil || len(b) != ed25519.PublicKeySize {
		return Key{}, errors.New("key: not a base64 Ed25519 public key")
	}
	return Key{Name: f[2], Pub: ed25519.PublicKey(b)}, nil
}

func (k Key) String() string {
	return "cirrusstore-key v1 " + k.Name + " " + base64.StdEncoding.EncodeToString(k.Pub)
}

// Fingerprint is what the panel shows to compare a key by eye.
func (k Key) Fingerprint() string {
	sum := sha256.Sum256(k.Pub)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Secret is a signing key: "cirrusstore-secret v1 <name> <base64 seed>".
// It belongs offline with the publisher, never on a server.
type Secret struct {
	Name string
	priv ed25519.PrivateKey
}

// GenerateKey makes a new key pair named name; rnd nil uses crypto/rand.
func GenerateKey(name string, rnd io.Reader) (Key, Secret, error) {
	if !ValidName(name) {
		return Key{}, Secret{}, fmt.Errorf("key name %q: lowercase letters, digits and -", name)
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	pub, priv, err := ed25519.GenerateKey(rnd)
	if err != nil {
		return Key{}, Secret{}, err
	}
	return Key{Name: name, Pub: pub}, Secret{Name: name, priv: priv}, nil
}

// ParseSecret reads a signing key line.
func ParseSecret(s string) (Secret, error) {
	f := strings.Fields(s)
	if len(f) != 4 || f[0] != "cirrusstore-secret" || f[1] != "v1" || !ValidName(f[2]) {
		return Secret{}, errors.New(`a signing key is "cirrusstore-secret v1 <name> <base64>"`)
	}
	seed, err := base64.StdEncoding.DecodeString(f[3])
	if err != nil || len(seed) != ed25519.SeedSize {
		return Secret{}, errors.New("signing key: not a base64 Ed25519 seed")
	}
	return Secret{Name: f[2], priv: ed25519.NewKeyFromSeed(seed)}, nil
}

func (s Secret) String() string {
	return "cirrusstore-secret v1 " + s.Name + " " + base64.StdEncoding.EncodeToString(s.priv.Seed())
}

// Public is the key that checks this secret's signatures.
func (s Secret) Public() Key {
	return Key{Name: s.Name, Pub: s.priv.Public().(ed25519.PublicKey)}
}

// Sign signs data, returning the contents of its .sig file.
func (s Secret) Sign(data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, data)) + "\n")
}

// ParseSig reads a .sig file: one base64 Ed25519 signature.
func ParseSig(b []byte) ([]byte, error) {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: not a base64 Ed25519 signature", ErrSignature)
	}
	return sig, nil
}
