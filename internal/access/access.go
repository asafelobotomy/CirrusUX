// Package access describes who can reach the server and how: its login
// accounts, their SSH keys (security keys included), two-step sign-in,
// sshd's settings, SFTP and FTPS. The helper reads these as root and the
// panel shows them. Nothing here is secret: public keys, names and
// settings, never password hashes or TOTP seeds.
package access

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Status is what the helper's access-status prints.
type Status struct {
	SSH    SSH       `json:"ssh"`
	Users  []Account `json:"users"`
	FTPS   *FTPS     `json:"ftps,omitempty"`
	Errors []string  `json:"errors,omitempty"`
}

// SSH is sshd's effective configuration, from `sshd -T`.
type SSH struct {
	Installed bool  `json:"installed"`
	Running   bool  `json:"running"`
	Ports     []int `json:"ports"`
	// PasswordAuth and KbdInteractive are the password and the
	// prompt-based (one-time code) logins.
	PasswordAuth   bool   `json:"passwordAuth"`
	PubkeyAuth     bool   `json:"pubkeyAuth"`
	KbdInteractive bool   `json:"kbdInteractive"`
	RootLogin      string `json:"rootLogin"`             // yes, no, prohibit-password, …
	AuthMethods    string `json:"authMethods,omitempty"` // e.g. "publickey,keyboard-interactive"
	// SFTP is on when sshd has an sftp subsystem; Internal when it is
	// sshd's own (internal-sftp).
	SFTP         bool     `json:"sftp"`
	SFTPInternal bool     `json:"sftpInternal,omitempty"`
	AllowUsers   []string `json:"allowUsers,omitempty"`
	AllowGroups  []string `json:"allowGroups,omitempty"`
	// TOTP is on when sshd's PAM stack asks for a one-time code.
	TOTP bool `json:"totp"`
	// KeysFile is sshd's AuthorizedKeysFile; keys are only read from the
	// usual place.
	KeysFile string `json:"keysFile,omitempty"`
}

// Account is one login account: root and the people (UID 1000 and up).
type Account struct {
	Name  string `json:"name"`
	UID   int    `json:"uid"`
	Home  string `json:"home"`
	Shell string `json:"shell"`
	// CanLogin is false for nologin and false shells.
	CanLogin bool `json:"canLogin"`
	// Admin: a member of sudo, wheel or admin (or root itself).
	Admin bool `json:"admin"`
	// Groups are the account's groups by name, primary one included, so
	// the panel can tell whether sshd's AllowGroups lets it in.
	Groups []string `json:"groups,omitempty"`
	// Password is set (not "!" or "*"): logins with it may work.
	Password bool `json:"password"`
	// Locked: the account expired, so no login works, keys included.
	// LockedByPanel when CirrusUX set it (expiry day 1), which the panel
	// can undo.
	Locked        bool  `json:"locked"`
	LockedByPanel bool  `json:"lockedByPanel,omitempty"`
	Keys          []Key `json:"keys"`
	// TOTP: the user has a one-time code set up (~/.google_authenticator).
	TOTP bool `json:"totp"`
	// KeysError says why the keys couldn't be read.
	KeysError string `json:"keysError,omitempty"`
}

// Key is one line of authorized_keys.
type Key struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"` // SHA256:…, as ssh-keygen -l prints
	Comment     string `json:"comment,omitempty"`
	// SecurityKey: a FIDO2 key (sk-…), the SSH form of a passkey.
	SecurityKey bool `json:"securityKey,omitempty"`
	// Options are present (from=, command=, …); the panel shows them as such.
	Options bool `json:"options,omitempty"`
	// Public is "type base64", so a removed key can be put back.
	Public string `json:"public"`
}

// FTPS is pure-ftpd's state, from its Debian conf/ folder.
type FTPS struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	// TLS is pure-ftpd's TLS level: 3 refuses plain FTP, 1 and 2 allow it.
	TLS          int    `json:"tls"`
	User         string `json:"user,omitempty"`
	UserSet      bool   `json:"userSet"`
	Home         string `json:"home,omitempty"`
	PassivePorts string `json:"passivePorts,omitempty"`
	Anonymous    bool   `json:"anonymous"`
	// CertSHA256 is the certificate's fingerprint, to check on first connect.
	CertSHA256 string `json:"certSha256,omitempty"`
}

// KeyTypes are the key types the panel adds. DSA is gone from OpenSSH.
var KeyTypes = []string{
	"ssh-ed25519", "sk-ssh-ed25519@openssh.com",
	"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ecdsa-sha2-nistp256@openssh.com",
	"ssh-rsa",
}

// UserRe is a login name the panel acts on.
var UserRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// FingerprintRe is a SHA256 key fingerprint.
var FingerprintRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

var commentRe = regexp.MustCompile(`^[A-Za-z0-9@._+:-][A-Za-z0-9@._+: -]{0,99}$`)

// ParseKey reads one authorized_keys line. Lines with options are read
// too, but the panel never writes options.
func ParseKey(line string) (Key, error) {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return Key{}, fmt.Errorf("empty line")
	}
	opts := false
	if !slices.Contains(KeyTypes, f[0]) && !isKnownType(f[0]) {
		// Options come first; they may hold quoted spaces, so find the type.
		i := slices.IndexFunc(f, func(s string) bool { return slices.Contains(KeyTypes, s) || isKnownType(s) })
		if i < 1 {
			return Key{}, fmt.Errorf("no key type")
		}
		f, opts = f[i:], true
	}
	if len(f) < 2 {
		return Key{}, fmt.Errorf("no key")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return Key{}, fmt.Errorf("key isn't base64")
	}
	if t, ok := blobType(blob); !ok || t != f[0] {
		return Key{}, fmt.Errorf("key doesn't match its type %s", f[0])
	}
	sum := sha256.Sum256(blob)
	k := Key{
		Type: f[0], Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
		Comment: strings.Join(f[2:], " "), SecurityKey: strings.HasPrefix(f[0], "sk-"), Options: opts,
		Public: f[0] + " " + f[1],
	}
	if len(k.Comment) > 100 {
		k.Comment = k.Comment[:100]
	}
	return k, nil
}

func isKnownType(s string) bool {
	return s == "ssh-dss" || strings.HasSuffix(s, "-cert-v01@openssh.com")
}

// blobType reads the type name a public key blob starts with.
func blobType(b []byte) (string, bool) {
	if len(b) < 4 {
		return "", false
	}
	n := binary.BigEndian.Uint32(b)
	if n == 0 || n > 64 || int(n)+4 > len(b) {
		return "", false
	}
	return string(b[4 : 4+n]), true
}

// NewKeyLine checks a public key someone pasted and returns the line the
// panel writes: type, key and a plain comment, no options.
func NewKeyLine(pasted string) (string, Key, error) {
	f := strings.Fields(strings.TrimSpace(pasted))
	if len(f) < 2 || !slices.Contains(KeyTypes, f[0]) {
		return "", Key{}, fmt.Errorf("paste a public key that starts with one of: %s", strings.Join(KeyTypes, ", "))
	}
	comment := strings.Join(f[2:], " ")
	if comment != "" && !commentRe.MatchString(comment) {
		return "", Key{}, fmt.Errorf("the key's comment may use letters, digits, spaces and @._+:- (up to 100)")
	}
	line := f[0] + " " + f[1]
	if comment != "" {
		line += " " + comment
	}
	k, err := ParseKey(line)
	if err != nil {
		return "", Key{}, err
	}
	if blob, _ := base64.StdEncoding.DecodeString(f[1]); k.Type == "ssh-rsa" && len(blob) < 270 { // under 2048 bits
		return "", Key{}, fmt.Errorf("RSA keys under 2048 bits are refused")
	}
	return line, k, nil
}

// ParseKeys reads an authorized_keys file, skipping lines it can't read.
func ParseKeys(text string) []Key {
	keys := []Key{}
	for _, line := range strings.Split(text, "\n") {
		if k, err := ParseKey(line); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

// RemoveKey returns the file without the lines whose key has the
// fingerprint, and how many it removed.
func RemoveKey(text, fingerprint string) (string, int) {
	var keep []string
	n := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if k, err := ParseKey(line); err == nil && k.Fingerprint == fingerprint {
			n++
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, ""), n
}

// AdminGroups are the groups whose members can become root.
var AdminGroups = []string{"sudo", "wheel", "admin"}

// ParseAccounts reads /etc/passwd and /etc/group: root and UIDs from 1000,
// leaving out nobody (65534).
func ParseAccounts(passwd, group string) []Account {
	admins := map[string]bool{}
	member := map[string][]string{} // user -> supplementary groups
	byGID := map[int]string{}
	for _, line := range strings.Split(group, "\n") {
		f := strings.Split(line, ":")
		if len(f) != 4 {
			continue
		}
		if gid, err := strconv.Atoi(f[2]); err == nil {
			byGID[gid] = f[0]
		}
		for _, u := range strings.Split(f[3], ",") {
			if u == "" {
				continue
			}
			member[u] = append(member[u], f[0])
			if slices.Contains(AdminGroups, f[0]) {
				admins[u] = true
			}
		}
	}
	out := []Account{}
	for _, line := range strings.Split(passwd, "\n") {
		f := strings.Split(line, ":")
		if len(f) != 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || (uid != 0 && uid < 1000) || uid == 65534 || !UserRe.MatchString(f[0]) {
			continue
		}
		groups := slices.Clone(member[f[0]])
		if gid, err := strconv.Atoi(f[3]); err == nil && byGID[gid] != "" && !slices.Contains(groups, byGID[gid]) {
			groups = append(groups, byGID[gid])
		}
		out = append(out, Account{
			Name: f[0], UID: uid, Home: f[5], Shell: f[6], Keys: []Key{}, Groups: groups,
			CanLogin: !strings.HasSuffix(f[6], "/nologin") && !strings.HasSuffix(f[6], "/false") && f[6] != "",
			Admin:    uid == 0 || admins[f[0]],
		})
	}
	return out
}

// ApplyShadow fills Password and Locked from /etc/shadow, as of today
// (days since 1970). Hashes are read only to say whether one is set.
func ApplyShadow(users []Account, shadow string, today int64) {
	for _, line := range strings.Split(shadow, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 8 {
			continue
		}
		i := slices.IndexFunc(users, func(a Account) bool { return a.Name == f[0] })
		if i < 0 {
			continue
		}
		users[i].Password = f[1] != "" && !strings.HasPrefix(f[1], "!") && !strings.HasPrefix(f[1], "*")
		if exp, err := strconv.ParseInt(f[7], 10, 64); err == nil && exp <= today {
			users[i].Locked, users[i].LockedByPanel = true, exp == PanelLockDay
		}
	}
}

// PanelLockDay is the expiry day CirrusUX sets to lock an account: 2 Jan
// 1970, a date nobody sets by hand, so unlocking only undoes the panel's
// own lock.
const PanelLockDay = 1

// ApplySSHDConfig reads `sshd -T` (lower-case keys, one per line).
func ApplySSHDConfig(s *SSH, out string) {
	s.Ports = []int{}
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch k {
		case "port":
			if n, err := strconv.Atoi(v); err == nil {
				s.Ports = append(s.Ports, n)
			}
		case "passwordauthentication":
			s.PasswordAuth = v == "yes"
		case "pubkeyauthentication":
			s.PubkeyAuth = v == "yes"
		case "kbdinteractiveauthentication":
			s.KbdInteractive = v == "yes"
		case "permitrootlogin":
			s.RootLogin = v
		case "authenticationmethods":
			if v != "any" {
				s.AuthMethods = v
			}
		case "subsystem":
			name, cmd, _ := strings.Cut(v, " ")
			if name == "sftp" {
				s.SFTP, s.SFTPInternal = true, strings.TrimSpace(cmd) == "internal-sftp"
			}
		case "allowusers":
			s.AllowUsers = append(s.AllowUsers, strings.Fields(v)...)
		case "allowgroups":
			s.AllowGroups = append(s.AllowGroups, strings.Fields(v)...)
		case "authorizedkeysfile":
			s.KeysFile = v
		}
	}
}

// DefaultKeysFile is the AuthorizedKeysFile the panel reads and writes.
func DefaultKeysFile(v string) bool {
	return v == "" || v == ".ssh/authorized_keys" || v == ".ssh/authorized_keys .ssh/authorized_keys2"
}

// PAMAsksCode reports whether a PAM file (/etc/pam.d/sshd) asks for a
// one-time code.
func PAMAsksCode(pam string) bool {
	for _, line := range strings.Split(pam, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "pam_google_authenticator.so") || strings.Contains(line, "pam_oath.so") {
			return true
		}
	}
	return false
}
