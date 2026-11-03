package gateway

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadEnvFile sets the environment from a NAME=value file, read the way
// systemd's EnvironmentFile= reads it, for init systems that have no such
// thing (runit's chpst only reads a folder of files). Blank lines and lines
// starting with # or ; are skipped; a value may be wrapped in single or
// double quotes, and inside double quotes \" and \\ are escapes. Nothing
// is expanded: a $ is just a $.
func LoadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !envNameRe.MatchString(k) {
			return fmt.Errorf("%s line %d: want NAME=value", path, n)
		}
		v, err := unquoteEnv(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("%s line %d: %s: %v", path, n, k, err)
		}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return sc.Err()
}

func unquoteEnv(v string) (string, error) {
	if len(v) < 2 || (v[0] != '"' && v[0] != '\'') {
		return v, nil
	}
	q := v[0]
	if v[len(v)-1] != q {
		return "", fmt.Errorf("unclosed quote")
	}
	inner := v[1 : len(v)-1]
	if q == '\'' {
		return inner, nil
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) && (inner[i+1] == '"' || inner[i+1] == '\\') {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String(), nil
}
