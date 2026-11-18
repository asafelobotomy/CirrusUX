package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cirrusux/internal/access"
)

// maxKeysFile is how much of an authorized_keys file is read.
const maxKeysFile = 256 << 10

// keysIO is the child asUser starts, running as the account itself: it
// reads or changes that account's authorized_keys and nothing else. It
// refuses to run as anyone but the account it is told about.
func keysIO(op, name, arg string) ([]byte, error) {
	u, err := lookupAccount(name)
	if err != nil {
		return nil, err
	}
	if os.Getuid() != u.UID {
		return nil, fmt.Errorf("keys-io runs as %s only", name)
	}
	return keysFileOp(u.Home, op, arg)
}

// keysFileOp does the work in a home folder; tests call it directly.
func keysFileOp(home, op, arg string) ([]byte, error) {
	dir := filepath.Join(home, ".ssh")
	file := filepath.Join(dir, "authorized_keys")
	current, err := readCapped(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading authorized_keys: %v", err)
	}
	switch op {
	case "read":
		out := current
		if _, err := os.Stat(filepath.Join(home, ".google_authenticator")); err == nil {
			out += "\x00totp"
		}
		return []byte(out), nil
	case "add":
		raw, err := base64.RawURLEncoding.DecodeString(arg)
		if err != nil {
			return nil, fmt.Errorf("bad key encoding")
		}
		line, k, err := access.NewKeyLine(string(raw))
		if err != nil {
			return nil, err
		}
		for _, have := range access.ParseKeys(current) {
			if have.Fingerprint == k.Fingerprint {
				return []byte("already there\n"), nil
			}
		}
		if current != "" && !strings.HasSuffix(current, "\n") {
			current += "\n"
		}
		return nil, writeKeys(dir, file, current+line+"\n")
	case "remove":
		if !access.FingerprintRe.MatchString(arg) {
			return nil, fmt.Errorf("bad fingerprint")
		}
		next, n := access.RemoveKey(current, arg)
		if n == 0 {
			return nil, fmt.Errorf("no key %s", arg)
		}
		return nil, writeKeys(dir, file, next)
	}
	return nil, fmt.Errorf("unknown keys-io operation %q", op)
}

func readCapped(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxKeysFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxKeysFile {
		return "", fmt.Errorf("authorized_keys is over %d KB", maxKeysFile>>10)
	}
	return string(b), nil
}

// writeKeys replaces authorized_keys in one step, mode 600 in a 700 folder,
// as sshd's StrictModes wants.
func writeKeys(dir, file, text string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".authorized_keys.cirrusux-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
