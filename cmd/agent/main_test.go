package main

import (
	"maps"
	"testing"
)

func TestPortNames(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"8080=Proxy,22457=Admin panel", map[string]string{"8080": "Proxy", "22457": "Admin panel"}},
		{" 8080 = Proxy , 22 = SSH ", map[string]string{"8080": "Proxy", "22": "SSH"}},
		{"nonsense,80=Web,,", map[string]string{"80": "Web"}},
		{"80=a=b", map[string]string{"80": "a=b"}},
		{"80=,81=x", map[string]string{"80": "", "81": "x"}},
		{"80=a,80=b", map[string]string{"80": "b"}},
	}
	for _, c := range cases {
		got := portNames(c.in)
		if got == nil || !maps.Equal(got, c.want) {
			t.Errorf("portNames(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("CIRRUS_TEST_SET", "value")
	t.Setenv("CIRRUS_TEST_EMPTY", "")
	if got := env("CIRRUS_TEST_SET", "def"); got != "value" {
		t.Errorf("set variable: got %q", got)
	}
	if got := env("CIRRUS_TEST_EMPTY", "def"); got != "def" {
		t.Errorf("empty variable should fall back to the default, got %q", got)
	}
	if got := env("CIRRUS_TEST_UNSET_XYZ", "def"); got != "def" {
		t.Errorf("unset variable: got %q", got)
	}
}
