package agent

import (
	"testing"
	"time"
)

func TestFirstScheduleRetriesSoonWhenTheFirstReportFails(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 40, 0, 0, time.UTC)

	next, retry := firstSchedule(true, now, 5*time.Minute)
	if !next.Equal(now.Add(5*time.Minute)) || !retry.IsZero() {
		t.Errorf("a first report that worked: next %v, retry until %v; want a full interval, no retrying", next.Sub(now), retry)
	}

	next, retry = firstSchedule(false, now, 5*time.Minute)
	if !next.Equal(now.Add(15 * time.Second)) {
		t.Errorf("a first report that failed: next in %v, want 15s", next.Sub(now))
	}
	if !retry.Equal(now.Add(3 * time.Minute)) {
		t.Errorf("a first report that failed: retrying for %v, want 3m", retry.Sub(now))
	}
}
