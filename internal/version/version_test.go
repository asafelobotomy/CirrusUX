package version

import "testing"

func TestAtLeast(t *testing.T) {
	cases := []struct {
		have, need string
		want       bool
	}{
		{"0.7.0", "0.7.0", true},
		{"0.7.1", "0.7.0", true},
		{"0.7.0", "0.8.0", false},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"v0.7.0-rc1", "0.7.0", true},
		{"dev", "9.9.9", true},
		{"", "9.9.9", true},
		{"0.7.0", "bogus", false},
		{"0.7.0", "0.8", false},
	}
	for _, c := range cases {
		if got := AtLeast(c.have, c.need); got != c.want {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", c.have, c.need, got, c.want)
		}
	}
}

func TestNumber(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	for in, want := range map[string]string{"0.7.0": "0.7.0", "v1.2.3-dev": "1.2.3", "dev": "", "1.2": ""} {
		Version = in
		if got := Number(); got != want {
			t.Errorf("Number() for %q = %q, want %q", in, got, want)
		}
	}
}
