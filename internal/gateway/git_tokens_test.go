package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git-tokens.json")
	ts := openGitTokens(path)
	secret, tok, err := ts.Create("owner@example.com", "laptop", "read")
	if err != nil || !strings.HasPrefix(secret, "cxg_") || len(secret) < 40 || tok.Scope != "read" || tok.Owner != "owner@example.com" {
		t.Fatalf("Create: %q %+v %v", secret, tok, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), secret) || strings.Contains(string(b), secret[4:]) || !strings.Contains(string(b), tok.Hash) {
		t.Errorf("file: %s %v", b, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if got, ok := ts.Check(secret); !ok || got.ID != tok.ID || got.LastUsed == nil {
		t.Errorf("Check good: %+v %v", got, ok)
	}
	for _, bad := range []string{secret + "x", "", "cxg_", strings.ToUpper(secret)} {
		if _, ok := ts.Check(bad); ok {
			t.Errorf("Check(%q) passed", bad)
		}
	}
	list := ts.List("owner@example.com")
	if len(list) != 1 || list[0].Hash != "" {
		t.Errorf("List: %+v", list)
	}
	if len(ts.List("second@example.com")) != 0 {
		t.Error("another user sees the token")
	}
	if _, err := ts.Revoke("second@example.com", tok.ID); err == nil {
		t.Error("revoked by someone else")
	}
	// The store survives a restart.
	if _, ok := openGitTokens(path).Check(secret); !ok {
		t.Error("lost after reopening")
	}
	if _, err := ts.Revoke("owner@example.com", tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := ts.Check(secret); ok {
		t.Error("works after revoking")
	}
	for _, c := range [][2]string{{"x", "admin"}, {"", "read"}, {"a\nb", "read"}, {strings.Repeat("a", 61), "read"}} {
		if _, _, err := ts.Create("owner@example.com", c[0], c[1]); err == nil {
			t.Errorf("Create(%q, %q) accepted", c[0], c[1])
		}
	}
}
