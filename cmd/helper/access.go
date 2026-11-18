package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cirrusux/internal/access"
	"cirrusux/internal/execx"
	"cirrusux/internal/helperapi"
)

// Accounts and SSH (access-status, ssh-key-add, ssh-key-remove,
// account-lock, account-unlock). Root reads the account files and sshd's
// settings; anything inside a home folder (authorized_keys, the one-time
// code file) is read and written by a child running as that user
// (keys-io), so a link planted there can't make root touch another file.

func init() {
	for k, v := range map[string]func([]string) bool{
		helperapi.CmdSSHKeyAdd: func(a []string) bool {
			return len(a) == 2 && access.UserRe.MatchString(a[0]) && helperapi.ValidKeyArg(a[1])
		},
		helperapi.CmdSSHKeyRemove: func(a []string) bool {
			return len(a) == 2 && access.UserRe.MatchString(a[0]) && access.FingerprintRe.MatchString(a[1])
		},
		helperapi.CmdAccountLock:   func(a []string) bool { return len(a) == 1 && access.UserRe.MatchString(a[0]) },
		helperapi.CmdAccountUnlock: func(a []string) bool { return len(a) == 1 && access.UserRe.MatchString(a[0]) },
		// Run by the helper as the account itself; not in sudoers.
		helperapi.CmdKeysIO: func(a []string) bool {
			if len(a) != 3 || !access.UserRe.MatchString(a[1]) {
				return false
			}
			switch a[0] {
			case "read":
				return a[2] == "-"
			case "add":
				return helperapi.ValidKeyArg(a[2])
			case "remove":
				return access.FingerprintRe.MatchString(a[2])
			}
			return false
		},
	} {
		argChecks[k] = v
	}
}

// accessCommand runs one of the access subcommands, its arguments checked.
func accessCommand(ctx context.Context, sub string, args []string) (any, error) {
	switch sub {
	case helperapi.CmdAccessStatus:
		return accessStatus(ctx), nil
	case helperapi.CmdSSHKeyAdd:
		return sshKeyAdd(ctx, args[0], args[1])
	case helperapi.CmdSSHKeyRemove:
		return sshKeyRemove(ctx, args[0], args[1])
	}
	return accountLock(ctx, args[0], sub == helperapi.CmdAccountLock)
}

// accessRoot is "/" in use; tests point it at a fake tree.
var accessRoot = "/"

func rootFile(p string) string { return filepath.Join(accessRoot, p) }

func accessStatus(ctx context.Context) access.Status {
	st := access.Status{Users: []access.Account{}}
	errf := func(f string, a ...any) { st.Errors = append(st.Errors, fmt.Sprintf(f, a...)) }
	passwd, err1 := os.ReadFile(rootFile("etc/passwd"))
	group, _ := os.ReadFile(rootFile("etc/group"))
	if err1 != nil {
		errf("accounts: %v", err1)
	}
	st.Users = access.ParseAccounts(string(passwd), string(group))
	if shadow, err := os.ReadFile(rootFile("etc/shadow")); err == nil {
		access.ApplyShadow(st.Users, string(shadow), time.Now().Unix()/86400)
	} else {
		errf("shadow: %v", err)
	}

	if _, err := exec.LookPath("sshd"); err == nil {
		st.SSH.Installed = true
		if out, err := execx.Run(ctx, execx.Options{}, "sshd", "-T"); err == nil {
			access.ApplySSHDConfig(&st.SSH, string(out))
		} else {
			errf("sshd -T: %v", err)
		}
		st.SSH.Running = unitActive(ctx, "ssh.service") || unitActive(ctx, "sshd.service")
		if pam, err := os.ReadFile(rootFile("etc/pam.d/sshd")); err == nil {
			st.SSH.TOTP = access.PAMAsksCode(string(pam)) && st.SSH.KbdInteractive
		}
	}
	for i := range st.Users {
		u := &st.Users[i]
		if !u.CanLogin {
			continue
		}
		if !access.DefaultKeysFile(st.SSH.KeysFile) {
			u.KeysError = "sshd reads keys from " + st.SSH.KeysFile + ", which the panel doesn't read"
			continue
		}
		out, err := asUser(ctx, u.Name, "read", "-")
		if err != nil {
			u.KeysError = err.Error()
			continue
		}
		text, totp, _ := strings.Cut(string(out), "\x00")
		u.Keys, u.TOTP = access.ParseKeys(text), totp == "totp"
	}
	st.FTPS = ftpsStatus(ctx)
	return st
}

func unitActive(ctx context.Context, unit string) bool {
	_, err := execx.Run(ctx, execx.Options{}, "systemctl", "is-active", "--quiet", unit)
	return err == nil
}

// ftpsStatus reads Debian's pure-ftpd settings, as the ftps host module
// writes them; nil when pure-ftpd isn't installed.
func ftpsStatus(ctx context.Context) *access.FTPS {
	conf := rootFile("etc/pure-ftpd/conf")
	if _, err := os.Stat(conf); err != nil {
		return nil
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(conf, name))
		return strings.TrimSpace(string(b))
	}
	f := &access.FTPS{Installed: true, Running: unitActive(ctx, "pure-ftpd.service")}
	f.TLS, _ = strconv.Atoi(read("TLS"))
	f.PassivePorts = read("PassivePortRange")
	f.Anonymous = read("NoAnonymous") != "yes"
	if b, err := os.ReadFile(rootFile("etc/pure-ftpd/pureftpd.passwd")); err == nil {
		// user:hash:uid:gid::home/./:…; only the name and home are kept.
		for _, line := range strings.Split(string(b), "\n") {
			p := strings.Split(line, ":")
			if len(p) > 5 && access.UserRe.MatchString(p[0]) {
				f.User, f.UserSet, f.Home = p[0], true, strings.TrimSuffix(strings.TrimSuffix(p[5], "/./"), "/")
				break
			}
		}
	}
	if b, err := os.ReadFile(rootFile("etc/ssl/private/pure-ftpd.pem")); err == nil {
		f.CertSHA256 = certFingerprint(b)
	}
	return f
}

// certFingerprint is the SHA-256 of the first certificate in a PEM file,
// as browsers and FTP clients show it (AA:BB:…). The key beside it is
// skipped.
func certFingerprint(b []byte) string {
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return ""
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(blk.Bytes); err != nil {
			return ""
		}
		sum := sha256.Sum256(blk.Bytes)
		h := strings.ToUpper(hex.EncodeToString(sum[:]))
		var parts []string
		for i := 0; i < len(h); i += 2 {
			parts = append(parts, h[i:i+2])
		}
		return strings.Join(parts, ":")
	}
}

// person finds a login account the panel may change: UID 1000 and up,
// with a login shell.
func person(name string) (access.Account, error) {
	passwd, err := os.ReadFile(rootFile("etc/passwd"))
	if err != nil {
		return access.Account{}, err
	}
	group, _ := os.ReadFile(rootFile("etc/group"))
	for _, u := range access.ParseAccounts(string(passwd), string(group)) {
		if u.Name == name {
			if u.UID < 1000 || !u.CanLogin {
				return u, fmt.Errorf("%s isn't a person's login account", name)
			}
			return u, nil
		}
	}
	return access.Account{}, fmt.Errorf("no account named %s", name)
}

func sshKeyAdd(ctx context.Context, name, encoded string) (helperapi.Result, error) {
	if _, err := person(name); err != nil {
		return helperapi.Result{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return helperapi.Result{}, fmt.Errorf("the key isn't encoded as the panel sends it")
	}
	_, k, err := access.NewKeyLine(string(raw))
	if err != nil {
		return helperapi.Result{}, err
	}
	out, err := asUser(ctx, name, "add", encoded)
	if err != nil {
		return helperapi.Result{}, err
	}
	if strings.TrimSpace(string(out)) == "already there" {
		return helperapi.Result{Output: []string{name + " already has key " + k.Fingerprint}}, nil
	}
	return helperapi.Result{Output: []string{fmt.Sprintf("added %s key %s for %s", k.Type, k.Fingerprint, name)}}, nil
}

func sshKeyRemove(ctx context.Context, name, fp string) (helperapi.Result, error) {
	if _, err := person(name); err != nil {
		return helperapi.Result{}, err
	}
	st := access.SSH{}
	if out, err := execx.Run(ctx, execx.Options{}, "sshd", "-T"); err == nil {
		access.ApplySSHDConfig(&st, string(out))
	}
	out, err := asUser(ctx, name, "read", "-")
	if err != nil {
		return helperapi.Result{}, err
	}
	text, _, _ := strings.Cut(string(out), "\x00")
	keys := access.ParseKeys(text)
	if !slices.ContainsFunc(keys, func(k access.Key) bool { return k.Fingerprint == fp }) {
		return helperapi.Result{}, fmt.Errorf("%s has no key %s", name, fp)
	}
	// Without password logins, the last key is the only way in: lock the
	// account instead, which can be undone.
	if !st.PasswordAuth && len(slices.DeleteFunc(keys, func(k access.Key) bool { return k.Fingerprint == fp })) == 0 {
		return helperapi.Result{}, fmt.Errorf("that's %s's last key and password logins are off: lock the account instead", name)
	}
	if _, err := asUser(ctx, name, "remove", fp); err != nil {
		return helperapi.Result{}, err
	}
	return helperapi.Result{Output: []string{"removed key " + fp + " from " + name}}, nil
}

// accountLock expires the account, which stops every login, keys
// included; sessions already open stay open. The last unlocked admin is
// never locked.
func accountLock(ctx context.Context, name string, lock bool) (helperapi.Result, error) {
	u, err := person(name)
	if err != nil {
		return helperapi.Result{}, err
	}
	shadow, err := os.ReadFile(rootFile("etc/shadow"))
	if err != nil {
		return helperapi.Result{}, err
	}
	passwd, _ := os.ReadFile(rootFile("etc/passwd"))
	group, _ := os.ReadFile(rootFile("etc/group"))
	all := access.ParseAccounts(string(passwd), string(group))
	access.ApplyShadow(all, string(shadow), time.Now().Unix()/86400)
	i := slices.IndexFunc(all, func(a access.Account) bool { return a.Name == name })
	if lock {
		if all[i].Locked {
			return helperapi.Result{Output: []string{name + " is already locked"}}, nil
		}
		others := slices.ContainsFunc(all, func(a access.Account) bool {
			return a.Name != name && a.UID >= 1000 && a.Admin && a.CanLogin && !a.Locked
		})
		if u.Admin && !others {
			return helperapi.Result{}, fmt.Errorf("%s is the only admin who can log in; locking it would shut everyone out", name)
		}
		return run(ctx, "usermod", "--expiredate", "1970-01-02", name)
	}
	if !all[i].Locked {
		return helperapi.Result{Output: []string{name + " isn't locked"}}, nil
	}
	if !all[i].LockedByPanel {
		return helperapi.Result{}, fmt.Errorf("%s expired on a date someone set; change it with `chage -E`, not the panel", name)
	}
	return run(ctx, "usermod", "--expiredate", "", name)
}

// asUser runs keys-io as the account, with its own IDs and no others.
func asUser(ctx context.Context, name, op, arg string) ([]byte, error) {
	u, err := lookupAccount(name)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, self, helperapi.CmdKeysIO, op, name, arg)
	cmd.Env = []string{"PATH=" + rootPath, "HOME=" + u.Home, "LANG=C"}
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(u.UID), Gid: uint32(u.GID), Groups: []uint32{}}}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(strings.TrimPrefix(stderr.String(), "cirrusux-helper: "))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}

type passwdEntry struct {
	UID, GID int
	Home     string
}

func lookupAccount(name string) (passwdEntry, error) {
	b, err := os.ReadFile(rootFile("etc/passwd"))
	if err != nil {
		return passwdEntry{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 7 && f[0] == name {
			uid, e1 := strconv.Atoi(f[2])
			gid, e2 := strconv.Atoi(f[3])
			if e1 != nil || e2 != nil || !filepath.IsAbs(f[5]) {
				break
			}
			return passwdEntry{UID: uid, GID: gid, Home: f[5]}, nil
		}
	}
	return passwdEntry{}, fmt.Errorf("no account named %s", name)
}
