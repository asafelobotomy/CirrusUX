package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.env")
	os.WriteFile(p, []byte(`# a comment
; another
CIRRUS_TEST_PLAIN=A home media server, running Jellyfin
CIRRUS_TEST_DOLLAR=pa$word
  CIRRUS_TEST_DQ="say \"hi\" \\ $HOME"
CIRRUS_TEST_SQ='it''s $x'
CIRRUS_TEST_EMPTY=
`), 0o600)
	if err := LoadEnvFile(p); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"CIRRUS_TEST_PLAIN":  "A home media server, running Jellyfin",
		"CIRRUS_TEST_DOLLAR": "pa$word",
		"CIRRUS_TEST_DQ":     `say "hi" \ $HOME`,
		"CIRRUS_TEST_SQ":     "it''s $x",
		"CIRRUS_TEST_EMPTY":  "",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
		os.Unsetenv(k)
	}
	for _, bad := range []string{"no equals sign", "1BAD=x", `CIRRUS_TEST_Q="open`} {
		os.WriteFile(p, []byte(bad+"\n"), 0o600)
		if err := LoadEnvFile(p); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
