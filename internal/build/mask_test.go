package build

import (
	"bytes"
	"strings"
	"testing"
)

func masked(t *testing.T, secrets []string, writes ...string) string {
	t.Helper()
	var buf bytes.Buffer
	m := NewMasker(&buf, secrets)
	for _, w := range writes {
		if _, err := m.Write([]byte(w)); err != nil {
			t.Fatal(err)
		}
	}
	m.Flush()
	return buf.String()
}

func TestMaskHidesSecrets(t *testing.T) {
	tok := "ghp_supersecrettoken"
	if got := masked(t, []string{tok}, "using "+tok+" now\n"); got != "using *** now\n" {
		t.Fatalf("%q", got)
	}
	// Split over two writes, and with no newline at the end.
	if got := masked(t, []string{tok}, "token=ghp_super", "secrettoken"); got != "token=***" {
		t.Fatalf("%q", got)
	}
	// Each line of a multi-line secret, and the longer secret first.
	key := "-----BEGIN KEY-----\nabcdefghijklmnop\n-----END KEY-----"
	if got := masked(t, []string{key}, "-----BEGIN KEY-----\nabcdefghijklmnop\nafter\n"); got != "***\n***\nafter\n" {
		t.Fatalf("%q", got)
	}
	if got := masked(t, []string{"abcdefgh", "abcdefghij"}, "x abcdefghij y\n"); got != "x *** y\n" {
		t.Fatalf("%q", got)
	}
}

func TestMaskLeavesShortValuesAndOtherText(t *testing.T) {
	if got := masked(t, []string{"short"}, "a short line\n"); got != "a short line\n" {
		t.Fatalf("%q", got)
	}
	if got := masked(t, nil, "plain\n"); got != "plain\n" {
		t.Fatalf("%q", got)
	}
}

func TestMaskVeryLongLine(t *testing.T) {
	tok := "0123456789abcdef"
	line := strings.Repeat("x", 100<<10) + tok + strings.Repeat("y", 100<<10)
	var parts []string
	for i := 0; i < len(line); i += 7000 {
		parts = append(parts, line[i:min(i+7000, len(line))])
	}
	got := masked(t, []string{tok}, parts...)
	if strings.Contains(got, tok) || len(got) != len(line)-len(tok)+3 {
		t.Fatalf("secret survived, or the text changed: %d vs %d", len(got), len(line))
	}
}
