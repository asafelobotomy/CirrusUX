package idle

import (
	"strings"
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	cfg := DefaultConfig() // 30 min idle, 15 min grace
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	ago := func(m int) time.Time { return now.Add(-time.Duration(m) * time.Minute) }
	boot := ago(600)

	cases := []struct {
		name    string
		st      State
		reasons []string
		hold    Hold
		want    Verdict
		msg     string
	}{
		{"activity keeps it awake", State{LastActive: ago(90), LastWake: ago(90)}, []string{"1 login session(s)"}, Hold{}, Active, "ACTIVE: 1 login session(s)"},
		{"quiet long enough", State{LastActive: ago(31), LastWake: ago(120)}, nil, Hold{}, Suspend, "IDLE: nothing active for 31 min"},
		{"quiet, not yet", State{LastActive: ago(10), LastWake: ago(120)}, nil, Hold{}, Idle, "sleeps at 12:20"},
		{"grace after a wake", State{LastActive: ago(200), LastWake: ago(5)}, nil, Hold{}, Idle, "sleeps at 12:25"},
		{"a wake is quiet time's start, not activity", State{LastActive: ago(300), LastWake: ago(40)}, nil, Hold{}, Suspend, ""},
		{"never active since boot", State{}, nil, Hold{}, Suspend, ""},
		{"fresh boot gets grace", State{}, nil, Hold{}, Idle, ""},
		{"hold until a time", State{LastActive: ago(300)}, nil, Hold{Until: now.Add(time.Hour)}, Active, "stay-awake hold until 13:00"},
		{"hold forever", State{LastActive: ago(300)}, nil, Hold{Forever: true}, Active, "until you allow sleep"},
		{"expired hold doesn't count", State{LastActive: ago(300), LastWake: ago(300)}, nil, Hold{Until: ago(1)}, Suspend, ""},
		{"hold and activity both listed", State{}, []string{"host conns: 22<-192.0.2.5"}, Hold{Forever: true}, Active, "until you allow sleep; host conns: 22<-192.0.2.5"},
	}
	for _, c := range cases {
		b := boot
		if c.name == "fresh boot gets grace" {
			b = ago(3)
		}
		d := Decide(now, cfg, c.st, b, c.reasons, c.hold)
		if d.Verdict != c.want || !strings.Contains(d.Msg, c.msg) {
			t.Errorf("%s: got %v %q, want %v containing %q", c.name, d.Verdict, d.Msg, c.want, c.msg)
		}
	}
}
