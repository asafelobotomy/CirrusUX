package main

import (
	"crypto/ed25519"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/store"
)

// miniAgent answers the two requests the tool makes, for one key.
func miniAgent(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	str := func(b, s []byte) []byte { return append(binary.BigEndian.AppendUint32(b, uint32(len(s))), s...) }
	blob := str(str(nil, []byte("ssh-ed25519")), priv.Public().(ed25519.PublicKey))
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var n [4]byte
				if _, err := io.ReadFull(c, n[:]); err != nil {
					return
				}
				msg := make([]byte, binary.BigEndian.Uint32(n[:]))
				if _, err := io.ReadFull(c, msg); err != nil {
					return
				}
				var typ byte
				var body []byte
				switch msg[0] {
				case 11:
					typ, body = 12, str(binary.BigEndian.AppendUint32(nil, 1), blob)
					body = str(body, []byte("k"))
				case 13:
					bl := binary.BigEndian.Uint32(msg[1:])
					dl := binary.BigEndian.Uint32(msg[5+bl:])
					data := msg[9+bl : 9+bl+dl]
					typ, body = 14, str(nil, str(str(nil, []byte("ssh-ed25519")), ed25519.Sign(priv, data)))
				default:
					typ = 5
				}
				out := binary.BigEndian.AppendUint32(nil, uint32(len(body)+1))
				c.Write(append(append(out, typ), body...))
			}()
		}
	}()
	return sock
}

func TestSignThroughAgent(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	key := store.Key{Name: "example", Pub: priv.Public().(ed25519.PublicKey)}
	t.Setenv("SSH_AUTH_SOCK", miniAgent(t, priv))
	pubFile := filepath.Join(t.TempDir(), "example.pub")
	writeFile(t, pubFile, key.String()+"\n")

	dir := t.TempDir()
	defs, _ := filepath.Glob("../../internal/catalogue/apps/*.json")
	args := append([]string{"-agent", "-pub", pubFile, "-dir", dir}, defs...)
	if err := sign(args); err != nil {
		t.Fatal(err)
	}
	if err := index([]string{"-agent", "-pub", pubFile, "-dir", dir}); err != nil {
		t.Fatal(err)
	}
	src := store.Source{Name: "example", Keys: []store.Key{key}}
	ix, err := verifyDir(dir, src, time.Now())
	if err != nil || len(ix.Apps) != len(defs) {
		t.Fatalf("verifyDir = %+v, %v", ix, err)
	}

	// Another key than the agent's is refused before anything is signed.
	other, _, _ := store.GenerateKey("example", nil)
	writeFile(t, pubFile, other.String()+"\n")
	if err := index([]string{"-agent", "-pub", pubFile, "-dir", dir}); err == nil || !strings.Contains(err.Error(), "doesn't offer") {
		t.Fatalf("the wrong key was signed with: %v", err)
	}
}

func TestAgentFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-agent", "-dir", t.TempDir()},              // no -pub
		{"-agent", "-key", "x.key", "-pub", "x.pub"}, // both
		{"-dir", t.TempDir()},                        // nothing
	} {
		if err := index(args); err == nil {
			t.Errorf("index %v succeeded", args)
		}
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
