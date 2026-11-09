package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A job whose unit is gone but whose status still says running was killed
// on the way: it shows as failed, not running for ever.
func TestRecentJobsKilled(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	write := func(id string, started time.Time) {
		os.WriteFile(filepath.Join(dir, id+".json"), []byte(`{"job":"`+id+`","action":"install","app":"kavita","state":"running","started":"`+started.Format(time.RFC3339)+`"}`), 0o644)
	}
	write("aaaa1111", now.Add(-10*time.Minute)) // its unit is gone
	write("bbbb2222", now.Add(-10*time.Minute)) // still running
	write("cccc3333", now.Add(-10*time.Second)) // just started: the unit may not show yet
	was := jobUnitActive
	jobUnitActive = func(job string) bool { return job == "bbbb2222" }
	t.Cleanup(func() { jobUnitActive = was })
	got := map[string]string{}
	for _, j := range recentJobs(dir, now) {
		got[j.ID] = j.State
	}
	if got["aaaa1111"] != "failed" || got["bbbb2222"] != "running" || got["cccc3333"] != "running" {
		t.Errorf("states = %v", got)
	}
}
