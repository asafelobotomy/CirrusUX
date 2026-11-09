// Package envfile reads the KEY=VALUE files CirrusUX keeps in /etc
// (helper.env, sleep.env, ...) and the agent's copy of sleep.env. One
// reader for all of them, so they agree on quotes and comments:
//
//	# a comment line
//	KEY=value
//	KEY="quoted value"   # a comment after the value
//
// A value that starts with a quote runs to the matching quote; anything
// after it is ignored. An unquoted value ends at the first " #".
package envfile

import "strings"

// Line is one setting. HasEquals is false for a line with no "=", which
// the caller may skip or refuse.
type Line struct {
	N         int // line number, from 1
	Key       string
	Value     string
	HasEquals bool
}

// Parse returns every setting line in text, in order. Blank lines and
// comment lines are left out.
func Parse(text string) []Line {
	var out []Line
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		out = append(out, Line{N: i + 1, Key: strings.TrimSpace(k), Value: value(v), HasEquals: ok})
	}
	return out
}

// Map is the settings as a map; a later line wins. Lines without "=" are skipped.
func Map(text string) map[string]string {
	m := map[string]string{}
	for _, l := range Parse(text) {
		if l.HasEquals {
			m[l.Key] = l.Value
		}
	}
	return m
}

func value(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
		return strings.Trim(v, `"'`) // no closing quote: as before, drop the quotes
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, "\t#"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
