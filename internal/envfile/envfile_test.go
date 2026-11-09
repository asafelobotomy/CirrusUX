package envfile

import "testing"

func TestParse(t *testing.T) {
	in := "# top\n\nA=1\n B = two words \nC=\"quoted # not a comment\" # real comment\nD='x'\nE=plain # note\nF=\nnoequals\nG=\"unclosed\n"
	want := []Line{
		{3, "A", "1", true},
		{4, "B", "two words", true},
		{5, "C", "quoted # not a comment", true},
		{6, "D", "x", true},
		{7, "E", "plain", true},
		{8, "F", "", true},
		{9, "noequals", "", false},
		{10, "G", "unclosed", true},
	}
	got := Parse(in)
	if len(got) != len(want) {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMapSkipsLinesWithoutEquals(t *testing.T) {
	m := Map("A=1\njunk\nA=2\n")
	if len(m) != 1 || m["A"] != "2" {
		t.Errorf("got %v", m)
	}
}
