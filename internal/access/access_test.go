package access

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

// edKey makes an ed25519 public key line from a seed byte.
func edKey(seed byte, comment string) string {
	pub := ed25519.NewKeyFromSeed(make([]byte, 32, 32)).Public().(ed25519.PublicKey)
	pub = append([]byte(nil), pub...)
	pub[0] ^= seed
	blob := sshString("ssh-ed25519")
	blob = append(blob, sshString(string(pub))...)
	line := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
	if comment != "" {
		line += " " + comment
	}
	return line
}

func sshString(s string) []byte {
	b := make([]byte, 4, 4+len(s))
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	return append(b, s...)
}

func TestParseKey(t *testing.T) {
	line := edKey(1, "alice@laptop")
	k, err := ParseKey(line)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.StdEncoding.DecodeString(strings.Fields(line)[1])
	sum := sha256.Sum256(blob)
	want := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if k.Type != "ssh-ed25519" || k.Comment != "alice@laptop" || k.Fingerprint != want || k.SecurityKey || k.Options {
		t.Errorf("got %+v", k)
	}
	if !FingerprintRe.MatchString(k.Fingerprint) {
		t.Errorf("fingerprint %q doesn't match FingerprintRe", k.Fingerprint)
	}
	// Options before the type are read, and flagged.
	k, err = ParseKey(`from="192.0.2.0/24",no-pty ` + line)
	if err != nil || !k.Options || k.Type != "ssh-ed25519" {
		t.Errorf("with options: %+v %v", k, err)
	}
	// A blob whose inner type differs from the stated one is refused.
	f := strings.Fields(line)
	if _, err := ParseKey("ecdsa-sha2-nistp256 " + f[1]); err == nil {
		t.Error("a mismatched type was accepted")
	}
	for _, bad := range []string{"", "# comment", "ssh-ed25519", "ssh-ed25519 !!!", "nonsense AAAA"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) accepted", bad)
		}
	}
}

func TestNewKeyLine(t *testing.T) {
	line, k, err := NewKeyLine("  " + edKey(2, "bob laptop") + "\n")
	if err != nil || line != edKey(2, "bob laptop") || k.Comment != "bob laptop" {
		t.Fatalf("%q %+v %v", line, k, err)
	}
	for _, bad := range []string{
		`command="sh" ` + edKey(2, ""),     // options are never written
		edKey(2, `x" ; rm -rf /`),          // comment characters
		edKey(2, strings.Repeat("a", 101)), // comment length
		"ssh-dss AAAAB3NzaC1kc3MAAACBAP",   // DSA
	} {
		if _, _, err := NewKeyLine(bad); err == nil {
			t.Errorf("NewKeyLine(%q) accepted", bad)
		}
	}
}

func TestRemoveKey(t *testing.T) {
	a, b := edKey(3, "a"), edKey(4, "b")
	text := "# keys\n" + a + "\n" + b + "\n" + a + " again\n"
	ka, _ := ParseKey(a)
	got, n := RemoveKey(text, ka.Fingerprint)
	if n != 2 || got != "# keys\n"+b+"\n" {
		t.Errorf("n=%d got %q", n, got)
	}
	if len(ParseKeys(text)) != 3 {
		t.Error("ParseKeys miscounted")
	}
}

func TestParseAccounts(t *testing.T) {
	passwd := `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
alice:x:1000:1000:Alice:/home/alice:/bin/bash
media:x:1001:1001::/home/media:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
`
	group := "sudo:x:27:alice\nusers:x:100:media\n"
	us := ParseAccounts(passwd, group)
	if len(us) != 3 {
		t.Fatalf("got %d accounts: %+v", len(us), us)
	}
	if !us[0].Admin || !us[1].Admin || us[2].Admin {
		t.Errorf("admins wrong: %+v", us)
	}
	if !us[1].CanLogin || us[2].CanLogin {
		t.Errorf("logins wrong: %+v", us)
	}
	shadow := "root:*:20000:0:99999:7:::\nalice:$y$j9T$abc:20000:0:99999:7:::\nmedia:!:20000:0:99999:7::1:\n"
	ApplyShadow(us, shadow, 20400)
	if us[0].Password || !us[1].Password || us[1].Locked {
		t.Errorf("passwords wrong: %+v", us)
	}
	if !us[2].Locked || !us[2].LockedByPanel {
		t.Errorf("media should be locked by the panel: %+v", us[2])
	}
}

func TestApplySSHDConfig(t *testing.T) {
	var s SSH
	ApplySSHDConfig(&s, `port 22
port 2222
passwordauthentication no
pubkeyauthentication yes
kbdinteractiveauthentication yes
permitrootlogin prohibit-password
authenticationmethods publickey,keyboard-interactive
subsystem sftp internal-sftp
allowusers alice@192.0.2.0/24 bob
authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2
`)
	if len(s.Ports) != 2 || s.PasswordAuth || !s.PubkeyAuth || !s.KbdInteractive || s.RootLogin != "prohibit-password" ||
		!s.SFTP || !s.SFTPInternal || len(s.AllowUsers) != 2 || s.AuthMethods == "" || !DefaultKeysFile(s.KeysFile) {
		t.Errorf("got %+v", s)
	}
	if DefaultKeysFile("/etc/ssh/keys/%u") {
		t.Error("a custom keys file counted as the default")
	}
}

func TestPAMAsksCode(t *testing.T) {
	if !PAMAsksCode("@include common-auth\nauth required pam_google_authenticator.so nullok\n") {
		t.Error("missed pam_google_authenticator")
	}
	if PAMAsksCode("# auth required pam_google_authenticator.so\n@include common-auth\n") {
		t.Error("counted a commented-out line")
	}
}
