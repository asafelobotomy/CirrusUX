package snapshot

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"
)

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keys(m map[string]any) []string {
	var k []string
	for name := range m {
		k = append(k, name)
	}
	slices.Sort(k)
	return k
}

func TestMinimalSnapshotKeepsUIFields(t *testing.T) {
	// Fields the web UI (web/src/lib/sync.svelte.ts) reads without a guard
	// are always present; the optional ones are omitted when empty.
	m := asMap(t, Snapshot{})
	want := []string{"backups", "bootAt", "containers", "features", "host", "power", "takenAt", "temps", "timeline"}
	if got := keys(m); !slices.Equal(got, want) {
		t.Errorf("keys of an empty snapshot = %v, want %v", got, want)
	}
	p := asMap(t, Power{})
	if got, want := keys(p), []string{"graceMin", "idleMin", "nextWakes", "reasons", "since"}; !slices.Equal(got, want) {
		t.Errorf("power keys = %v, want %v", got, want)
	}
	b := asMap(t, Backups{})
	if got, want := keys(b), []string{"files"}; !slices.Equal(got, want) {
		t.Errorf("backups keys = %v, want %v", got, want)
	}
}

func TestFullSnapshotNames(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	n := 3
	s := Snapshot{
		Host: "homeserver", Protocol: Protocol, Actions: []string{"stay-awake"}, AgentVersion: "0.7.0",
		WakeIface: "eth0", WakeMAC: "aa:bb:cc:dd:ee:ff", TakenAt: now,
		Power:      Power{StayAwakeUntil: &now, StayAwakeForever: true, LastActivity: &now},
		Containers: []Container{{Name: "x", UpSince: &now}},
		Backups:    Backups{LastRun: &BackupRun{}, NextRun: &now},
		Disk:       &Disk{}, Security: &Security{Banned: &n, ClamAV: &ClamAV{}},
		Apps:     []App{{ID: "a", HTTPS: true, Source: "s", Serial: 1, PreviousTag: "1", PreviousDigest: "d", Digest: "d"}},
		Jobs:     []Job{{ID: "j", Finished: &now, Detail: "d", Error: "e"}},
		Timeline: []Segment{{From: now, To: &now, Reason: "r"}},
		Errors:   []string{"boom"},
	}
	m := asMap(t, s)
	for _, k := range []string{"protocol", "actions", "agentVersion", "wakeIface", "wakeMAC", "disk", "security", "apps", "jobs", "errors"} {
		if _, ok := m[k]; !ok {
			t.Errorf("snapshot lacks %q", k)
		}
	}
	pw := m["power"].(map[string]any)
	for _, k := range []string{"stayAwakeUntil", "stayAwakeForever", "lastActivity"} {
		if _, ok := pw[k]; !ok {
			t.Errorf("power lacks %q", k)
		}
	}
	c := m["containers"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "image", "tag", "state", "health", "status", "ports", "upSince"} {
		if _, ok := c[k]; !ok {
			t.Errorf("container lacks %q", k)
		}
	}
	bk := m["backups"].(map[string]any)
	for _, k := range []string{"files", "lastRun", "nextRun"} {
		if _, ok := bk[k]; !ok {
			t.Errorf("backups lacks %q", k)
		}
	}
	if got := keys(bk["lastRun"].(map[string]any)); !slices.Equal(got, []string{"end", "ok", "start", "summary"}) {
		t.Errorf("lastRun keys: %v", got)
	}
	if got := keys(m["disk"].(map[string]any)); !slices.Equal(got, []string{"freeBytes", "linkMbps", "mount", "totalBytes"}) {
		t.Errorf("disk keys: %v", got)
	}
	sec := m["security"].(map[string]any)
	for _, k := range []string{"banned", "clamav", "firewall"} {
		if _, ok := sec[k]; !ok {
			t.Errorf("security lacks %q", k)
		}
	}
	if got := keys(sec["clamav"].(map[string]any)); !slices.Equal(got, []string{"at", "files", "infected"}) {
		t.Errorf("clamav keys: %v", got)
	}
	app := m["apps"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "name", "tag", "digest", "previousTag", "previousDigest", "runtime", "source", "serial", "mainPort", "https", "installed"} {
		if _, ok := app[k]; !ok {
			t.Errorf("app lacks %q", k)
		}
	}
	job := m["jobs"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "action", "app", "name", "state", "step", "detail", "error", "started", "finished"} {
		if _, ok := job[k]; !ok {
			t.Errorf("job lacks %q", k)
		}
	}
	seg := m["timeline"].([]any)[0].(map[string]any)
	for _, k := range []string{"from", "to", "state", "reason"} {
		if _, ok := seg[k]; !ok {
			t.Errorf("segment lacks %q", k)
		}
	}
	bf := asMap(t, BackupFile{})
	if got := keys(bf); !slices.Equal(got, []string{"at", "bytes", "encrypted", "name"}) {
		t.Errorf("backup file keys: %v", got)
	}
}

func TestOptionalFieldsOmitted(t *testing.T) {
	m := asMap(t, Snapshot{Apps: []App{{ID: "a"}}, Jobs: []Job{{ID: "j"}}, Timeline: []Segment{{}}})
	app := m["apps"].([]any)[0].(map[string]any)
	for _, k := range []string{"digest", "previousTag", "previousDigest", "source", "serial", "https"} {
		if _, ok := app[k]; ok {
			t.Errorf("app should omit empty %q", k)
		}
	}
	job := m["jobs"].([]any)[0].(map[string]any)
	for _, k := range []string{"detail", "error", "finished"} {
		if _, ok := job[k]; ok {
			t.Errorf("job should omit empty %q", k)
		}
	}
	if _, ok := m["timeline"].([]any)[0].(map[string]any)["to"]; ok {
		t.Error("an ongoing segment should omit \"to\"")
	}
	// A banned count of 0 is a real answer (a jail with nobody banned) and
	// must survive; nil (no jail) is omitted.
	zero := 0
	if sec := asMap(t, Security{Banned: &zero}); sec["banned"] != 0.0 {
		t.Errorf("banned:0 lost: %v", sec)
	}
	if sec := asMap(t, Security{}); sec["banned"] != nil {
		t.Errorf("nil banned should be omitted: %v", sec)
	}
}

func TestRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	n := 2
	in := Snapshot{
		Host: "homeserver", Protocol: Protocol, Actions: []string{"a", "b"}, TakenAt: now, BootAt: now,
		Features:   []string{"sleep"},
		Power:      Power{Since: now, IdleMin: 20, GraceMin: 5, Reasons: []string{"ssh"}, NextWakes: []Wake{{At: now, Unit: "u"}}},
		Containers: []Container{{Name: "x", Ports: []int{80}}},
		Backups:    Backups{Files: []BackupFile{{Name: "f", At: now, Bytes: 9, Encrypted: true}}},
		Temps:      []float64{41.5},
		Security:   &Security{Banned: &n, Firewall: []FirewallCheck{{Chain: "c", OK: true}}},
		Timeline:   []Segment{{From: now, State: "awake"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip changed the snapshot:\n in: %+v\nout: %+v", in, out)
	}
	// An agent that predates Protocol/Actions sends neither: they decode as
	// zero, which the gateway reads as "allow everything".
	var old Snapshot
	if err := json.Unmarshal([]byte(`{"host":"h"}`), &old); err != nil || old.Protocol != 0 || old.Actions != nil {
		t.Errorf("old agent snapshot: %+v, %v", old, err)
	}
}

func TestFirewallFailing(t *testing.T) {
	s := &Security{}
	if got := s.FirewallFailing(); got != nil {
		t.Errorf("no checks: %v", got)
	}
	s.Firewall = []FirewallCheck{{"a", true}, {"b", false}, {"c", true}, {"d", false}}
	if got := s.FirewallFailing(); !slices.Equal(got, []string{"b", "d"}) {
		t.Errorf("got %v", got)
	}
	s.Firewall = []FirewallCheck{{"a", true}}
	if got := s.FirewallFailing(); len(got) != 0 {
		t.Errorf("all ok: %v", got)
	}
}
