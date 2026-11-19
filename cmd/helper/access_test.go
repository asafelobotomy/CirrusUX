package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/access"
	"cirrusux/internal/helperapi"
)

func testKey(seed byte, comment string) string {
	pub := make([]byte, 32)
	pub[0] = seed
	str := func(s string) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	blob := append(str("ssh-ed25519"), str(string(pub))...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

func enc(line string) string { return base64.RawURLEncoding.EncodeToString([]byte(line)) }

func TestKeysFileOp(t *testing.T) {
	home := t.TempDir()
	a, b := testKey(1, "alice@laptop"), testKey(2, "alice@phone")
	if _, err := keysFileOp(home, "add", enc(a)); err != nil {
		t.Fatal(err)
	}
	if _, err := keysFileOp(home, "add", enc(b)); err != nil {
		t.Fatal(err)
	}
	if out, _ := keysFileOp(home, "add", enc(a)); string(out) != "already there\n" {
		t.Errorf("adding twice: %q", out)
	}
	file := filepath.Join(home, ".ssh", "authorized_keys")
	fi, err := os.Stat(file)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", fi.Mode(), err)
	}
	if di, _ := os.Stat(filepath.Join(home, ".ssh")); di.Mode().Perm() != 0o700 {
		t.Errorf(".ssh mode %v", di.Mode())
	}
	out, _ := keysFileOp(home, "read", "-")
	if got := access.ParseKeys(string(out)); len(got) != 2 || strings.Contains(string(out), "\x00") {
		t.Fatalf("read: %q", out)
	}
	ka, _ := access.ParseKey(a)
	if _, err := keysFileOp(home, "remove", ka.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := keysFileOp(home, "remove", ka.Fingerprint); err == nil {
		t.Error("removing a missing key worked")
	}
	os.WriteFile(filepath.Join(home, ".google_authenticator"), []byte("x"), 0o600)
	out, _ = keysFileOp(home, "read", "-")
	text, totp, _ := strings.Cut(string(out), "\x00")
	if totp != "totp" || len(access.ParseKeys(text)) != 1 {
		t.Errorf("after remove: %q", out)
	}
	// A key with options is refused, so nothing but plain keys get written.
	if _, err := keysFileOp(home, "add", enc(`command="id" `+a)); err == nil {
		t.Error("a key with options was added")
	}
}

func TestKeysIOArgs(t *testing.T) {
	fp := "SHA256:" + strings.Repeat("A", 43)
	good := [][]string{
		{helperapi.CmdKeysIO, "read", "alice", "-"},
		{helperapi.CmdKeysIO, "add", "alice", enc(testKey(1, "x"))},
		{helperapi.CmdKeysIO, "remove", "alice", fp},
		{helperapi.CmdSSHKeyAdd, "alice", enc(testKey(1, "x"))},
		{helperapi.CmdAccountLock, "alice"},
	}
	bad := [][]string{
		{helperapi.CmdKeysIO, "read", "alice", "/etc/shadow"},
		{helperapi.CmdKeysIO, "write", "alice", "-"},
		{helperapi.CmdKeysIO, "read", "../root", "-"},
		{helperapi.CmdSSHKeyAdd, "alice", "ssh-ed25519 AAAA"},
		{helperapi.CmdSSHKeyAdd, "-oProxyCommand", enc(testKey(1, "x"))},
		{helperapi.CmdSSHKeyRemove, "alice", "MD5:aa:bb"},
		{helperapi.CmdAccountLock, "Alice"},
		{helperapi.CmdAccountUnlock},
	}
	for _, a := range good {
		if !validArgs(a) {
			t.Errorf("refused %v", a)
		}
	}
	for _, a := range bad {
		if validArgs(a) {
			t.Errorf("accepted %v", a)
		}
	}
}

func TestPersonAndStatus(t *testing.T) {
	root := t.TempDir()
	old := accessRoot
	accessRoot = root
	t.Cleanup(func() { accessRoot = old })
	write := func(p, s string) {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/passwd", "root:x:0:0::/root:/bin/bash\nalice:x:1000:1000::/home/alice:/bin/bash\nmedia:x:1001:1001::/srv:/usr/sbin/nologin\n")
	write("etc/group", "sudo:x:27:alice\n")
	write("etc/shadow", "alice:$y$x:1:0:99999:7:::\n")
	if _, err := person("alice"); err != nil {
		t.Error(err)
	}
	for _, n := range []string{"root", "media", "bob"} {
		if _, err := person(n); err == nil {
			t.Errorf("person(%s) accepted", n)
		}
	}
	// Locking the only admin is refused before anything runs.
	if _, err := accountLock(context.Background(), "alice", true); err == nil || !strings.Contains(err.Error(), "only admin") {
		t.Errorf("locking the last admin: %v", err)
	}
	// Unlocking only undoes the panel's own lock.
	write("etc/shadow", "alice:$y$x:1:0:99999:7::5:\n")
	if _, err := accountLock(context.Background(), "alice", false); err == nil || !strings.Contains(err.Error(), "chage") {
		t.Errorf("unlocking a hand-set expiry: %v", err)
	}

	write("etc/pure-ftpd/conf/TLS", "3\n")
	write("etc/pure-ftpd/conf/PassivePortRange", "39000 40000\n")
	write("etc/pure-ftpd/conf/NoAnonymous", "yes\n")
	write("etc/pure-ftpd/pureftpd.passwd", "media:$hash:1001:1001::/mnt/media/./::::::::::::\n")
	write("etc/ssl/private/pure-ftpd.pem", string(selfSigned(t)))
	f := ftpsStatus(context.Background())
	if f == nil || f.TLS != 3 || f.User != "media" || f.Home != "/mnt/media" || f.Anonymous || f.PassivePorts != "39000 40000" {
		t.Fatalf("ftps: %+v", f)
	}
	if len(f.CertSHA256) != 95 {
		t.Errorf("cert fingerprint %q", f.CertSHA256)
	}
}

func selfSigned(t *testing.T) []byte {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "homeserver"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	sum := sha256.Sum256(der)
	if want := strings.ToUpper(hex.EncodeToString(sum[:2])); !strings.HasPrefix(strings.ReplaceAll(certFingerprint(out), ":", ""), want) {
		t.Errorf("fingerprint doesn't start with %s", want)
	}
	return out
}

func TestCanSignIn(t *testing.T) {
	ctx := context.Background()
	pw := access.Account{Name: "alice", Password: true}
	keyed := access.SSH{Installed: true, PubkeyAuth: true}
	if canSignIn(ctx, pw, keyed) {
		t.Error("a password counted while sshd has password logins off")
	}
	if !canSignIn(ctx, pw, access.SSH{Installed: true, PasswordAuth: true}) {
		t.Error("a password didn't count while sshd takes passwords")
	}
	// No password and no way to read keys: not a way in.
	if canSignIn(ctx, access.Account{Name: "bob"}, keyed) {
		t.Error("an account with nothing counted")
	}
}
