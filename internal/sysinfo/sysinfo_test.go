package sysinfo

import "testing"

func TestParseBootTime(t *testing.T) {
	if got := ParseBootTime("cpu 1 2\nbtime 1700000000\nprocs 3\n"); got.Unix() != 1700000000 {
		t.Errorf("got %v", got)
	}
	if !ParseBootTime("cpu 1\n").IsZero() || !ParseBootTime("btime x\n").IsZero() {
		t.Error("missing or bad btime should be zero")
	}
}

func TestParseTimers(t *testing.T) {
	out := `[{"next":2000000,"unit":"b.timer"},{"next":0,"unit":"never.timer"},{"next":1000000,"unit":"a.timer"}]`
	got, err := ParseTimers(out)
	if err != nil || len(got) != 2 || got[0].Unit != "a.timer" || got[1].Unit != "b.timer" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ParseTimers("nope"); err == nil {
		t.Error("bad JSON should fail")
	}
}
